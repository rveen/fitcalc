package report

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/rveen/fitcalc/derating"
)

func TestStatistics(t *testing.T) {
	x := []float64{1, math.NaN(), 2, 3, 4}
	for p, want := range map[float64]float64{0: 1, 0.5: 2.5, 1: 4, 0.05: 1.15} {
		if got := percentile(x, p); math.Abs(got-want) > 1e-12 {
			t.Errorf("percentile %g = %g, want %g", p, got, want)
		}
	}
	if m := mean(x); m != 2.5 {
		t.Errorf("mean %g", m)
	}
	if s := stddev([]float64{2, 4, 4, 4, 5, 5, 7, 9}); math.Abs(s-2.138089935) > 1e-9 {
		t.Errorf("stddev %g", s)
	}
	if !math.IsNaN(percentile([]float64{math.NaN()}, 0.5)) || !math.IsNaN(stddev([]float64{1})) {
		t.Error("no values, or a single one")
	}
}

// sampleMonteCarlo is sample with 4 Monte Carlo runs, one of which failed:
// the FIT of the others at 98, 100 and 102 % of the nominal one, and the
// stress ratios at 90, 100 and 110 %.
func sampleMonteCarlo(t *testing.T) *Report {
	t.Helper()
	r := sample(t)
	r.MonteCarlo = &MonteCarlo{Runs: 4, Seed: 7, Total: []float64{60, 62, 64},
		Varied: []Variation{
			{Element: "R1", Kind: "R", Ref: "R1", Nominal: 10e3, Tolerance: 1, Source: "db.csv (R_0603)"},
			{Element: "V1", Kind: "V", Nominal: 12, Tolerance: 5, Source: "--vary"},
		},
		Failed: []string{"run 4: at 85 °C: no convergence"},
	}
	for i := range r.Rows {
		row := &r.Rows[i]
		x := &MonteCarloRow{Ratios: map[string][]float64{}}
		for _, f := range []float64{0.9, 1, 1.1} {
			x.FIT = append(x.FIT, fit(*row)*(0.8+0.2*f))
			level := derating.OK
			if row.Derating != nil {
				for _, ratio := range row.Derating.Ratios {
					v := ratio.Value * f
					x.Ratios[ratio.Name] = append(x.Ratios[ratio.Name], v)
					switch {
					case v > 1:
						level = max(level, derating.Overstress)
					case v > 0.8:
						level = max(level, derating.Warning)
					}
				}
			}
			x.Levels = append(x.Levels, level)
		}
		row.MonteCarlo = x
	}
	return r
}

func TestMarkdownMonteCarlo(t *testing.T) {
	var b bytes.Buffer
	if err := Markdown(&b, sampleMonteCarlo(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report_montecarlo.md", b.Bytes())

	for _, want := range []string{
		"- Monte Carlo (section Monte Carlo): 4 more runs of the whole analysis",
		"- **Monte Carlo:** 4 runs, total FIT from 60.2 to 63.8 (5 % to 95 %); above a derating limit in some runs: R3 (100 %)\n",
		"4 runs with seed 7 (1 failed, left out). In each run",
		"| R1 | R1 | 10 kΩ | 1 % | db.csv (R_0603) |\n| V1 |  | 12 V | 5 % | --vary |\n",
		"over the runs 62 on average (σ 2), from 60.2 to 63.8 (5 % to 95 %).",
		"| **100 %** | **100 %** |\n",
		"- **R3**: Monte Carlo: above a derating limit in 100 % of the runs, overstressed in 100 % (largest P/Pmax = 140 %)\n",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("report without %q", want)
		}
	}
}

func TestCSVMonteCarlo(t *testing.T) {
	var b bytes.Buffer
	if err := CSV(&b, sampleMonteCarlo(t)); err != nil {
		t.Fatal(err)
	}
	golden(t, "report_montecarlo.csv", b.Bytes())
	if !strings.Contains(b.String(), ",fit_mc_mean,fit_mc_p5,fit_mc_p95,mc_warning_share,mc_overstress_share\n") {
		t.Error("no Monte Carlo columns")
	}
}
