package main

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/rveen/fitcalc/derating"
	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/rel"
	"github.com/rveen/fitcalc/report"
	"github.com/rveen/fitcalc/spice"
	"github.com/rveen/fitcalc/stress"
	"github.com/rveen/fitcalc/thermal"
)

// maxWarningsPerElement bounds the warnings of the stresses of an element
// over the points of a sweep or a transient, which can differ at every point.
const maxWarningsPerElement = 3

// simulation is one ngspice run at one temperature: of the operating point,
// the DC sweep or the transient analysis.
type simulation struct {
	op       *spice.OP
	stress   map[string]stress.Stress           // by element: of the operating point, the means over the sweep, or the effective values over the transient
	points   []map[string]stress.Stress         // with a sweep: the stresses at each of its points
	labels   []string                           // with a sweep: its points, "V1 = 13.2 V"
	tran     *spice.Tran                        // the transient analysis; nil without
	times    int                                // with a transient: the number of time points
	stats    map[string]map[string]stress.Stats // with a transient: the statistics of the stresses, by element
	warnings []string                           // ngspice's and those of the stresses, without repetitions
}

// Electro-thermal iteration
const (
	maxThermalRuns   = 20   // simulations at one temperature
	thermalTolerance = 0.01 // K: the iteration stops when no element's temperature changes by more
)

// simulateThermal is simulate with the thermal network net, if not nil: it
// simulates with every element of the network at its temperature (dtemp),
// solves the network for the power of that simulation, and simulates again
// until the temperatures change by no more than thermalTolerance, at most
// maxThermalRuns times. values are the element values of a Monte Carlo run,
// by name; nil for those of the netlist. The run has the solution of the
// last simulation; its
// Temp and Name are the caller's.
func simulateThermal(ctx context.Context, cfg *config, c *spice.Circuit, an spice.Analysis, temp float64, keep string,
	net *thermal.Network, p *parts.Parts, values map[string]float64) (*simulation, *report.ThermalRun, error) {

	if net == nil {
		s, err := simulate(ctx, cfg, c, an, temp, keep, nil, values)
		return s, nil, err
	}
	dtemp := map[string]float64{}
	for run := 1; ; run++ {
		s, err := simulate(ctx, cfg, c, an, temp, keep, dtemp, values)
		if err != nil {
			return nil, nil, err
		}
		comps, _, _ := parts.Merge(c, s.stress, p)
		sol := net.Solve(powers(comps))
		next := map[string]float64{}
		for _, x := range comps {
			if x.Element != nil && net.Has(x.Ref) && spice.Heatable(x.Element) {
				next[x.Element.Name] = sol.Rise[strings.ToUpper(x.Ref)]
			}
		}
		var change float64
		for _, m := range []map[string]float64{dtemp, next} {
			for name := range m {
				change = max(change, math.Abs(next[name]-dtemp[name]))
			}
		}
		if change <= thermalTolerance || run == maxThermalRuns {
			return s, &report.ThermalRun{Solution: sol, Simulations: run, Converged: change <= thermalTolerance, Change: change}, nil
		}
		dtemp = next
	}
}

// powers returns the power of the components, by reference: the magnitude
// of their stress p.
func powers(comps []*parts.Component) map[string]float64 {
	pw := map[string]float64{}
	for _, x := range comps {
		if q, ok := x.Stress["p"]; ok {
			pw[x.Ref] = math.Abs(q.Value)
		}
	}
	return pw
}

// simulate runs ngspice on c at temp, NaN for the temperature of the
// netlist: the operating point, or the analysis an if it is not nil, with
// the elements of dtemp (by name, in K) above that temperature. keep is the
// directory for its files (spice.Runner).
func simulate(ctx context.Context, cfg *config, c *spice.Circuit, an spice.Analysis, temp float64, keep string, dtemp, values map[string]float64) (*simulation, error) {

	r := &spice.Runner{Exe: cfg.ngspice, Keep: keep, Analysis: an, Dtemp: dtemp, Values: values}
	var op *spice.OP
	var err error
	if math.IsNaN(temp) {
		op, err = r.OP(ctx, c)
	} else {
		op, err = r.OPAt(ctx, c, temp)
	}
	if err != nil {
		return nil, err
	}
	if !math.IsNaN(temp) && !math.IsNaN(op.Temp) && math.Abs(op.Temp-temp) > 1e-6 {
		return nil, fmt.Errorf("ngspice simulated at %g °C", op.Temp)
	}

	s := &simulation{op: op}
	s.warn(op.Warnings)

	switch an := an.(type) {

	case *spice.Sweep:
		axis, points, err := spice.ReadSweep(op.Raw)
		if err != nil {
			return nil, err
		}
		s.points = s.derive(c, points)
		for _, x := range axis {
			s.labels = append(s.labels, an.Label(x))
		}
		s.stress = stress.MeanAll(s.points)

	case *spice.Tran:
		times, points, err := spice.ReadTran(op.Raw)
		if err != nil {
			return nil, err
		}
		s.tran, s.times = an, len(times)
		s.stats = stress.TimeStatsAll(times, s.derive(c, points))
		s.stress = map[string]stress.Stress{}
		for name, st := range s.stats {
			s.stress[name] = stress.Effective(st)
		}

	default:
		values, err := spice.ReadOP(op.Raw)
		if err != nil {
			return nil, err
		}
		st, w := stress.DeriveAll(c, values)
		s.stress = st
		s.warn(w)
	}
	return s, nil
}

// derive returns the stresses at each point of a sweep or a transient, and
// keeps their warnings: at most maxWarningsPerElement for each element.
func (s *simulation) derive(c *spice.Circuit, points []spice.Values) []map[string]stress.Stress {
	var out []map[string]stress.Stress
	per := map[*spice.Element]int{}
	for _, values := range points {
		all := map[string]stress.Stress{}
		for _, e := range c.Elements {
			st, w := stress.Derive(c, values.Element(e))
			for _, x := range w {
				if !slices.Contains(s.warnings, x) && per[e] < maxWarningsPerElement {
					per[e]++
					s.warnings = append(s.warnings, x)
				}
			}
			if st != nil {
				all[e.Name] = st
			}
		}
		out = append(out, all)
	}
	return out
}

func (s *simulation) warn(warnings []string) {
	for _, w := range warnings {
		if !slices.Contains(s.warnings, w) {
			s.warnings = append(s.warnings, w)
		}
	}
}

// stressed are the components with the stresses of one simulation, in the
// order of the components of the report.
type stressed struct {
	comps []*parts.Component // with the stresses for the FIT and the tables

	// With a sweep: the components at each of its points, named by labels
	points [][]*parts.Component
	labels []string

	// With a transient: the components with the stresses for the derating
	// (stress.Extremes), and the statistics of their stresses
	high, low []*parts.Component
	stats     []map[string]stress.Stats
	tran      *spice.Tran
}

// components returns the components with the stresses of s, in the order of
// comps.
func (s *simulation) components(c *spice.Circuit, p *parts.Parts, comps []*parts.Component) *stressed {
	merge := func(st map[string]stress.Stress) []*parts.Component {
		at, _, _ := parts.Merge(c, st, p)
		return align(comps, at)
	}
	out := &stressed{comps: merge(s.stress), labels: s.labels, tran: s.tran}
	for _, st := range s.points {
		out.points = append(out.points, merge(st))
	}
	if s.stats != nil {
		high, low := map[string]stress.Stress{}, map[string]stress.Stress{}
		for name, st := range s.stats {
			high[name], low[name] = stress.Extremes(st)
		}
		out.high, out.low = merge(high), merge(low)
		for _, x := range comps {
			var st map[string]stress.Stats
			if x.Element != nil {
				st = s.stats[x.Element.Name]
			}
			out.stats = append(out.stats, st)
		}
	}
	return out
}

// heat gives the components of the thermal network their temperatures in
// solution sol: ta, the heating by the other components, and for diodes,
// transistors and ICs t, the rise by their own power, unless the parts files
// give t.
func (st *stressed) heat(net *thermal.Network, sol *thermal.Solution) {
	lists := append([][]*parts.Component{st.comps, st.high, st.low}, st.points...)
	for _, list := range lists {
		for _, x := range list {
			if !net.Has(x.Ref) {
				continue
			}
			ref := strings.ToUpper(x.Ref)
			if x.Stress == nil {
				x.Stress = stress.Stress{}
			}
			x.Stress["ta"] = stress.Quantity{Value: sol.Coupled(ref), Unit: "°C", Source: stress.Derived,
				Note: "thermal network: heating by the other components"}
			if t, ok := x.Stress["t"]; rel.SelfHeated(x.Class) && (!ok || t.Source != stress.Metadata) {
				x.Stress["t"] = stress.Quantity{Value: sol.Own[ref], Unit: "°C", Source: stress.Derived,
					Note: fmt.Sprintf("thermal network: p·Z, Z = %.4g K/W", net.Z(ref, ref))}
			}
		}
	}
}

// align returns the components of at in the order of comps, matched by
// reference; the one of comps where at has none.
func align(comps, at []*parts.Component) []*parts.Component {
	byRef := map[string]*parts.Component{}
	for _, x := range at {
		byRef[x.Ref] = x
	}
	out := make([]*parts.Component, len(comps))
	for i, x := range comps {
		out[i] = x
		if y := byRef[x.Ref]; y != nil {
			out[i] = y
		}
	}
	return out
}

// check is the derating check of component i at ambient temperature temp
// (NaN: the ratings as given). With a sweep, it takes the largest ratios over
// its points. With a transient, it compares the peak voltages, the RMS
// currents and the mean power with the ratings, and each ratio says which it
// takes.
func (st *stressed) check(rules derating.Rules, i int, temp float64) *derating.Result {
	switch {
	case st.points != nil:
		var rs []*derating.Result
		for k := range st.points {
			rs = append(rs, rules.CheckAt(st.points[k][i], temp))
		}
		return derating.Worst(st.comps[i], rs, st.labels)

	case st.high != nil:
		high, low := rules.CheckAt(st.high[i], temp), rules.CheckAt(st.low[i], temp)
		st.label(high, i, true)
		st.label(low, i, false)
		return derating.Worst(st.comps[i], []*derating.Result{high, low}, nil)
	}
	return rules.CheckAt(st.comps[i], temp)
}

// label sets the At of the ratios of r, the check of the high or low
// stresses of component i: "the peak at t = 1.25 ms", "the RMS current",
// "the mean power". Ratings compared with values from the parts files get
// none.
func (st *stressed) label(r *derating.Result, i int, high bool) {
	for k := range r.Ratios {
		x := &r.Ratios[k]
		s, ok := st.stats[i][x.Key]
		if !ok || x.Stress.Source == stress.Metadata {
			continue
		}
		switch s.Unit {
		case "V":
			at := s.MaxAt
			if !high {
				at = s.MinAt
			}
			x.At = "the peak at " + st.tran.Label(at)
		case "A":
			x.At = "the RMS current"
		case "W":
			x.At = "the mean power"
		}
	}
}

// sweepOf parses the --sweep of netlist c: its source must be a voltage or
// current source of the netlist.
func sweepOf(c *spice.Circuit, s string) (*spice.Sweep, error) {
	w, err := spice.ParseSweep(s)
	if err != nil {
		return nil, err
	}
	if e := c.Element(w.Source); e == nil || e.Kind != "V" && e.Kind != "I" {
		return nil, fmt.Errorf("--sweep: %s is not a voltage or current source of the netlist", w.Source)
	}
	return w, nil
}
