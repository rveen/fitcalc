package derating

import (
	"fmt"
	"math"
	"slices"

	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/stress"
)

// Thresholds of the stress ratio.
const (
	WarnRatio = 0.8 // the derating limit where no rule gives another: above it, a warning
	MaxRatio  = 1.0 // above: overstress, whatever the rules
)

// Level is the outcome of a check.
type Level int

const (
	OK Level = iota
	Warning
	Overstress
)

func (l Level) String() string {
	switch l {
	case Warning:
		return "warning"
	case Overstress:
		return "overstress"
	}
	return "ok"
}

// Ratio is one stress ratio of a component.
type Ratio struct {
	Name   string          // "V/Vmax", "P/Pmax", …
	Key    string          // the stress quantity: v, vr, p, i, vgs, vcb, veb
	Stress stress.Quantity // the stress, as a magnitude
	Rating float64
	Source string // where the rating comes from
	Value  float64
	Limit  float64 // the derating limit of the rule, 1 = 100 %
	Rule   string  // where the rule comes from: "default", or file:line
	At     string  // the point of a sweep where it is largest, "V1 = 13.2 V", or what of a transient it takes, "the peak at t = 1.25 ms"; "" for an operating point
	Level  Level
}

func (r Ratio) String() string {
	s := fmt.Sprintf("%s = %.0f%% (%.4g %s of %.4g %s)", r.Name, 100*r.Value, r.Stress.Value, r.Stress.Unit, r.Rating, r.Stress.Unit)
	if r.At != "" {
		s += " with " + r.At
	}
	return s
}

// Worst combines the checks of component c at the points of a sweep, named
// by labels: of each ratio the largest, with the point where it is, and the
// issues of all points. With labels nil, the ratios keep their At.
func Worst(c *parts.Component, results []*Result, labels []string) *Result {
	w := &Result{Component: c, Temp: math.NaN()}
	at := map[string]int{}
	for k, r := range results {
		w.Temp = r.Temp
		for _, x := range r.Ratios {
			if labels != nil {
				x.At = labels[k]
			}
			i, ok := at[x.Name]
			switch {
			case !ok:
				at[x.Name] = len(w.Ratios)
				w.Ratios = append(w.Ratios, x)
			case x.Value > w.Ratios[i].Value:
				w.Ratios[i] = x
			}
		}
		for _, is := range r.Issues {
			if !slices.Contains(w.Issues, is) {
				w.Issues = append(w.Issues, is)
			}
		}
	}
	return w
}

// Issue is a finding that is not a ratio.
type Issue struct {
	Level Level
	Text  string
}

// Result is the derating check of one component.
type Result struct {
	Component *parts.Component
	Temp      float64 // ambient temperature of the check, °C; NaN: the ratings as given
	Ratios    []Ratio
	Issues    []Issue
}

// Level is the worst level of the ratios and issues.
func (r *Result) Level() Level {
	l := OK
	for _, x := range r.Ratios {
		l = max(l, x.Level)
	}
	for _, x := range r.Issues {
		l = max(l, x.Level)
	}
	return l
}

// checks are the ratios, with the stress quantity (VoltageKey for v) and the
// rating field.
var checks = []struct{ name, key, rating string }{
	{"V/Vmax", "v", "vmax"},
	{"V/Vpmax", "v", "vpmax"},
	{"P/Pmax", "p", "pmax"},
	{"I/Imax", "i", "imax"},
	{"Vgs/Vgsmax", "vgs", "vgsmax"},
	{"Vcb/Vcbmax", "vcb", "vcbmax"},
	{"Veb/Vebmax", "veb", "vebmax"},
}

// Names returns the names of the ratios, in report order.
func Names() []string {
	var n []string
	for _, ch := range checks {
		n = append(n, ch.name)
	}
	return n
}

// Ratings returns the rating fields the ratios compare with.
func Ratings() []string {
	var r []string
	for _, ch := range checks {
		r = append(r, ch.rating)
	}
	return r
}

// polarized are the tags of polarized capacitors.
var polarized = []string{"alu", "elco", "tant", "tantalium"}

// Check compares the stresses of a component with its ratings, with the
// default rule. A ratio needs both; a stress without a rating gives none.
func Check(c *parts.Component) *Result {
	return Default.CheckAt(c, math.NaN())
}

// CheckAt is Check at an ambient temperature (Rules.CheckAt).
func CheckAt(c *parts.Component, temp float64) *Result {
	return Default.CheckAt(c, temp)
}

// CheckAll checks every component with the default rule.
func CheckAll(comps []*parts.Component) []*Result {
	return Default.CheckAll(comps)
}

// Check compares the stresses of a component with its ratings: a ratio above
// the limit of its rule is a warning, and above 100 % an overstress.
func (rs Rules) Check(c *parts.Component) *Result {
	return rs.CheckAt(c, math.NaN())
}

// CheckAt is Check at ambient temperature temp, in °C. Above the rated
// temperature (RatedTemp), the power rating falls linearly to 0 at tmax, and
// an ambient temperature at or above tmax is an overstress. Other ratings are
// not derated. A component without tmax keeps its power rating. temp NaN is
// Check.
//
// A component heated by the others (the stress ta, from a thermal network)
// is checked at its local ambient, temp + ta; one with a temperature rise t
// is also an overstress when temp + ta + t reaches tmax.
func (rs Rules) CheckAt(c *parts.Component, temp float64) *Result {

	r := &Result{Component: c, Temp: temp}

	// The local ambient: temp and the heating by the other components
	amb, what := temp, "ambient temperature"
	if ta, ok := c.Stress["ta"]; ok && ta.Value != 0 && !math.IsNaN(temp) {
		amb = temp + ta.Value
		what = fmt.Sprintf("local ambient temperature (%g °C and %.3g K from the other components)", temp, ta.Value)
	}

	tmax, hasTmax := c.Number("tmax")
	hot := !math.IsNaN(temp) && hasTmax
	switch t, hasT := c.Stress["t"]; {
	case hot && amb >= tmax:
		r.Issues = append(r.Issues, Issue{Overstress,
			fmt.Sprintf("%s %.4g °C at or above tmax %g °C", what, amb, tmax)})
	case hot && hasT && amb+t.Value >= tmax:
		r.Issues = append(r.Issues, Issue{Overstress,
			fmt.Sprintf("temperature %.4g °C (%s and a rise of %.3g K) at or above tmax %g °C", amb+t.Value, what, t.Value, tmax)})
	}
	temp = amb

	for _, ch := range checks {
		key := ch.key
		if key == "v" {
			key = parts.VoltageKey(c.Class)
		}
		q, ok := c.Stress[key]
		rating, hasRating := c.Number(ch.rating)
		if !ok || !hasRating || rating <= 0 {
			continue
		}
		source := c.Source(ch.rating)
		if ch.rating == "pmax" && hot {
			if temp >= tmax {
				continue // the tmax issue
			}
			if tr, from := RatedTemp(c); temp > tr {
				f := (tmax - temp) / (tmax - tr)
				rating *= f
				source += fmt.Sprintf(", derated to %.3g%% at %g °C (linear from %g °C, %s, to tmax %g °C)",
					100*f, temp, tr, from, tmax)
			}
		}
		rule := rs.For(c, ch.rating)
		q.Value = math.Abs(q.Value)
		x := Ratio{Name: ch.name, Key: key, Stress: q, Rating: rating, Source: source, Value: q.Value / rating,
			Limit: rule.Limit, Rule: rule.Source}
		switch {
		case x.Value > MaxRatio:
			x.Level = Overstress
		case x.Value > x.Limit:
			x.Level = Warning
		}
		r.Ratios = append(r.Ratios, x)
	}

	if c.Class == "C" && slices.ContainsFunc(c.Tags, func(t string) bool { return slices.Contains(polarized, t) }) {
		if v, ok := c.Stress["v"]; ok && v.Value < 0 {
			r.Issues = append(r.Issues, Issue{Overstress,
				fmt.Sprintf("reverse voltage %.4g V on a polarized capacitor", v.Value)})
		}
	}
	return r
}

// CheckAll checks every component.
func (rs Rules) CheckAll(comps []*parts.Component) []*Result {
	results := make([]*Result, 0, len(comps))
	for _, c := range comps {
		results = append(results, rs.Check(c))
	}
	return results
}

// RatedTemp returns the ambient temperature up to which the power rating
// applies, and where it comes from: the parts field trated, else 70 °C for
// resistors, as in most resistor datasheets, and 25 °C for other classes, as
// for the total power of semiconductors.
func RatedTemp(c *parts.Component) (float64, string) {
	if v, ok := c.Number("trated"); ok {
		return v, "trated from " + c.Source("trated")
	}
	if c.Class == "R" {
		return 70, "default for resistors"
	}
	return 25, "default"
}
