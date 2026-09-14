package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/rveen/fitcalc/derating"
	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/rel"
	"github.com/rveen/fitcalc/report"
	"github.com/rveen/fitcalc/spice"
)

// maxRuns bounds the Monte Carlo runs.
const maxRuns = 10000

// parseVary reads element tolerances written as NAME=TOLERANCE, in percent,
// separated by commas or spaces: "V1=5%, R1=1".
func parseVary(s string) (map[string]float64, error) {
	out := map[string]float64{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		name, tol, ok := strings.Cut(f, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("%q: want NAME=TOLERANCE%%", f)
		}
		x, err := strconv.ParseFloat(strings.TrimSuffix(tol, "%"), 64)
		if err != nil || !(x > 0) || x >= 100 {
			return nil, fmt.Errorf("%q: the tolerance is a percentage above 0 and below 100", f)
		}
		out[strings.ToUpper(name)] = x
	}
	if len(out) == 0 {
		return nil, errors.New("no elements")
	}
	return out, nil
}

// variations returns the elements whose values vary, by name: the
// resistors, capacitors and inductors with a tolerance in the parts files,
// and the elements of vary (from where), which win. Elements of included
// files do not vary: warnings say so.
func variations(c *spice.Circuit, comps []*parts.Component, vary map[string]float64, where string) ([]report.Variation, []string, error) {

	refs := map[*spice.Element]string{}
	byName := map[string]report.Variation{}
	for _, x := range comps {
		e := x.Element
		if e == nil {
			continue
		}
		refs[e] = x.Ref
		tol, ok := x.Number("tolerance")
		if !strings.Contains("RCL", e.Kind) || math.IsNaN(e.Value) || !ok || tol <= 0 {
			continue
		}
		byName[e.Name] = report.Variation{Element: e.Name, Kind: e.Kind, Ref: x.Ref, Nominal: e.Value, Tolerance: tol, Source: x.Source("tolerance")}
	}
	for name, tol := range vary {
		e := c.Element(name)
		switch {
		case e == nil:
			return nil, nil, fmt.Errorf("%s: %s is not an element of the netlist", where, name)
		case math.IsNaN(e.Value):
			return nil, nil, fmt.Errorf("%s: %s has no numeric value (%s)", where, e.Name, e.ValueText)
		}
		byName[e.Name] = report.Variation{Element: e.Name, Kind: e.Kind, Ref: refs[e], Nominal: e.Value, Tolerance: tol, Source: where}
	}

	var out []report.Variation
	var warnings []string
	for _, v := range byName {
		if e := c.Element(v.Element); e.File != c.File {
			warnings = append(warnings, fmt.Sprintf("Monte Carlo: %s is in an included file: its value does not vary", e.Name))
			continue
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b report.Variation) int { return cmp.Compare(a.Element, b.Element) })
	slices.Sort(warnings)
	return out, warnings, nil
}

// draw returns the element values of run k (from 0): each nominal value
// times 1 + tolerance·x/3, with x from a standard normal distribution
// truncated at ±3. The values depend only on the seed and k.
func draw(vs []report.Variation, seed uint64, k int) map[string]float64 {
	r := rand.New(rand.NewPCG(seed, uint64(k)))
	values := map[string]float64{}
	for _, v := range vs {
		x := r.NormFloat64()
		for math.Abs(x) > 3 {
			x = r.NormFloat64()
		}
		values[v.Element] = v.Nominal * (1 + v.Tolerance/100*x/3)
	}
	return values
}

// phaseComponents returns for each phase of m the components with the
// stresses at its temperature: those of the corner at the ambient of an
// operating phase, nil for the others; and false if no phase has any. at
// holds the components of each corner.
func phaseComponents(m *rel.Mission, corners []report.Corner, at []*stressed) ([][]*parts.Component, bool) {
	phases := make([][]*parts.Component, len(m.Phases))
	perPhase := false
	for i, ph := range m.Phases {
		for k, corner := range corners {
			if ph.On && corner.Temp == ph.Tamb {
				phases[i], perPhase = at[k].comps, true
			}
		}
	}
	return phases, perPhase
}

// monteCarlo runs the analysis cfg.mc more times, each with the values of
// the varied elements drawn within their tolerances: at the nominal
// temperature and at every corner of rep, with the thermal network. The
// simulations run in parallel; the FIT and the derating of each run follow
// one after the other, as the fides library logs through the log package.
// comps are the components of rep's rows.
func monteCarlo(ctx context.Context, cfg *config, c *spice.Circuit, p *parts.Parts, an spice.Analysis,
	comps []*parts.Component, rep *report.Report, warn func(string)) error {

	var vary map[string]float64
	if cfg.vary != "" {
		vary, _ = parseVary(cfg.vary) // checked by parseArgs
	}
	vs, warnings, err := variations(c, comps, vary, cfg.origin("vary"))
	if err != nil {
		return err
	}
	for _, w := range warnings {
		warn(w)
	}
	mc := &report.MonteCarlo{Runs: cfg.mc, Seed: cfg.seed, Varied: vs}
	rep.MonteCarlo = mc
	if len(vs) == 0 {
		warn("Monte Carlo: no element varies: give tolerances in the parts files, or elements with --vary")
		return nil
	}

	// The temperatures of a run: the nominal one, and those of the corners
	// that differ from it
	temps := []float64{math.NaN()}
	for _, k := range rep.Corners {
		if k.Temp != rep.Temp {
			temps = append(temps, k.Temp)
		}
	}

	type run struct {
		sims    []*simulation
		thermal []*report.ThermalRun
		err     error
	}
	runs := make([]run, cfg.mc)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range jobs {
				values := draw(vs, cfg.seed, k)
				r := &runs[k]
				for _, temp := range temps {
					s, th, err := simulateThermal(ctx, cfg, c, an, temp, "", rep.Thermal, p, values)
					if err != nil {
						r.err = err
						if !math.IsNaN(temp) {
							r.err = fmt.Errorf("at %g °C: %w", temp, err)
						}
						break
					}
					r.sims, r.thermal = append(r.sims, s), append(r.thermal, th)
				}
			}
		}()
	}
	for k := range cfg.mc {
		if ctx.Err() != nil {
			break
		}
		jobs <- k
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}

	for i := range rep.Rows {
		rep.Rows[i].MonteCarlo = &report.MonteCarloRow{Ratios: map[string][]float64{}}
	}
	for k, r := range runs {
		if r.err != nil {
			mc.Failed = append(mc.Failed, fmt.Sprintf("run %d: %v", k+1, firstLine(r.err)))
			continue
		}
		st := make([]*stressed, len(r.sims))
		for j, s := range r.sims {
			st[j] = s.components(c, p, comps)
			if r.thermal[j] != nil {
				st[j].heat(rep.Thermal, r.thermal[j].Solution)
			}
		}
		nominal := st[0]
		atCorner := make([]*stressed, len(rep.Corners))
		next := 1
		for i, corner := range rep.Corners {
			if corner.Temp == rep.Temp {
				atCorner[i] = nominal
			} else {
				atCorner[i], next = st[next], next+1
			}
		}

		var fits []*rel.Result
		if m := rep.Mission; m != nil {
			if phases, ok := phaseComponents(m, rep.Corners, atCorner); ok {
				fits = rel.FITPhases(nominal.comps, phases, m)
			} else {
				fits = rel.FIT(nominal.comps, m)
			}
			total, _ := rel.Total(fits)
			mc.Total = append(mc.Total, total)
		}

		for i := range rep.Rows {
			row := rep.Rows[i].MonteCarlo
			f := math.NaN()
			if fits != nil {
				f = fits[i].FIT
			}
			row.FIT = append(row.FIT, f)

			checks := []*derating.Result{nominal.check(rep.Rules, i, math.NaN())}
			for j, corner := range rep.Corners {
				checks = append(checks, atCorner[j].check(rep.Rules, i, corner.Temp))
			}
			level, worst := derating.OK, map[string]float64{}
			for _, d := range checks {
				level = max(level, d.Level())
				for _, x := range d.Ratios {
					worst[x.Name] = max(worst[x.Name], x.Value)
				}
			}
			row.Levels = append(row.Levels, level)
			for name, v := range worst {
				row.Ratios[name] = append(row.Ratios[name], v)
			}
		}
	}
	if n := len(mc.Failed); n > 0 {
		warn(fmt.Sprintf("Monte Carlo: %d of %d runs failed; %s", n, mc.Runs, mc.Failed[0]))
	}
	return nil
}

// firstLine is the first line of the message of err.
func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return s
}
