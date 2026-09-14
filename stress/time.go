package stress

import (
	"math"
	"sort"
)

// Stats summarise a quantity over the time points of a transient analysis.
// Mean and RMS are over time, with the quantity linear between the points.
type Stats struct {
	Unit         string
	Source       Source
	Note         string // that of the quantity at the first point
	Min, Max     float64
	MinAt, MaxAt float64 // the times of Min and Max
	Mean, RMS    float64
}

// Peak returns the value of the largest magnitude, Min or Max, and its time.
func (s Stats) Peak() (float64, float64) {
	if math.Abs(s.Min) > math.Abs(s.Max) {
		return s.Min, s.MinAt
	}
	return s.Max, s.MaxAt
}

// Effective returns the value that stands for the quantity over the
// transient: the mean of a power, and of other quantities the RMS value with
// the sign of the mean. Both are the value of a constant quantity.
func (s Stats) Effective() Quantity {
	if s.Unit == "W" {
		return Quantity{s.Mean, s.Unit, s.Source, "mean over the transient of " + s.Note}
	}
	return Quantity{math.Copysign(s.RMS, s.Mean), s.Unit, s.Source, "RMS over the transient of " + s.Note}
}

// TimeStats returns the statistics of each quantity of an element over the
// time points of a transient analysis, from its stresses at each point. A
// quantity missing at one of the points is left out.
func TimeStats(times []float64, points []Stress) map[string]Stats {
	out := map[string]Stats{}
	if len(points) == 0 || len(points) != len(times) {
		return out
	}
	names := make([]string, 0, len(points[0]))
	for name := range points[0] {
		names = append(names, name)
	}
	sort.Strings(names)

next:
	for _, name := range names {
		q := points[0][name]
		s := Stats{Unit: q.Unit, Source: q.Source, Note: q.Note,
			Min: q.Value, Max: q.Value, MinAt: times[0], MaxAt: times[0]}
		var sum, sum2 float64
		for k, p := range points {
			x, ok := p[name]
			if !ok {
				continue next
			}
			if x.Value < s.Min {
				s.Min, s.MinAt = x.Value, times[k]
			}
			if x.Value > s.Max {
				s.Max, s.MaxAt = x.Value, times[k]
			}
			if k > 0 {
				a, b, dt := points[k-1][name].Value, x.Value, times[k]-times[k-1]
				sum += (a + b) / 2 * dt
				sum2 += (a*a + a*b + b*b) / 3 * dt
			}
		}
		if d := times[len(times)-1] - times[0]; d > 0 {
			s.Mean, s.RMS = sum/d, math.Sqrt(math.Max(sum2/d, 0))
		} else {
			s.Mean, s.RMS = q.Value, math.Abs(q.Value)
		}
		out[name] = s
	}
	return out
}

// TimeStatsAll is TimeStats for all elements (DeriveAll of each point), by
// element. An element without stress at one of the points is left out.
func TimeStatsAll(times []float64, points []map[string]Stress) map[string]map[string]Stats {
	out := map[string]map[string]Stats{}
	if len(points) == 0 {
		return out
	}
	for name := range points[0] {
		var ss []Stress
		for _, p := range points {
			if s, ok := p[name]; ok {
				ss = append(ss, s)
			}
		}
		if len(ss) == len(points) {
			out[name] = TimeStats(times, ss)
		}
	}
	return out
}

// Effective returns the effective value (Stats.Effective) of each quantity.
func Effective(stats map[string]Stats) Stress {
	s := Stress{}
	for name, x := range stats {
		s[name] = x.Effective()
	}
	return s
}

// Extremes returns the stresses for the derating checks over a transient:
// the voltages at their maximum (high) and at their minimum (low), and the
// other quantities at their effective value in both. Currents are compared
// with their ratings as RMS values, and power as its mean: the ratings of a
// continuous current or power are thermal.
func Extremes(stats map[string]Stats) (high, low Stress) {
	high, low = Stress{}, Stress{}
	for name, x := range stats {
		if x.Unit != "V" {
			high[name], low[name] = x.Effective(), x.Effective()
			continue
		}
		high[name] = Quantity{x.Max, x.Unit, x.Source, "maximum over the transient of " + x.Note}
		low[name] = Quantity{x.Min, x.Unit, x.Source, "minimum over the transient of " + x.Note}
	}
	return high, low
}
