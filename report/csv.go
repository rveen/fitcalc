package report

import (
	"encoding/csv"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/rveen/fitcalc/derating"
)

// extraRatios are the ratios whose columns follow the others, with the
// prefix of their column names.
var extraRatios = []struct{ name, prefix string }{
	{"V/Vpmax", "vp"}, {"Vgs/Vgsmax", "vgs"}, {"Vcb/Vcbmax", "vcb"}, {"Veb/Vebmax", "veb"},
}

// CSV writes the report as CSV: one row per component, with the source of
// each stress next to it. The columns of the simulations at other
// temperatures are empty without them.
func CSV(w io.Writer, r *Report) error {

	cw := csv.NewWriter(w)
	head := []string{"ref", "element", "class", "tags", "package", "value",
		"v", "v_source", "i", "i_source", "p", "p_source", "t", "t_source",
		"fit", "fit_error", "v_ratio", "p_ratio", "i_ratio", "derating", "warnings", "fit_nominal"}
	for _, n := range []string{"v_ratio", "p_ratio", "i_ratio"} {
		head = append(head, n+"_max", n+"_max_temp")
	}
	head = append(head, "derating_max", "manufacturer", "mpn")
	for _, x := range extraRatios {
		head = append(head, x.prefix+"_ratio", x.prefix+"_ratio_max", x.prefix+"_ratio_max_temp")
	}
	head = append(head, "v_min", "v_max", "i_min", "i_max", "p_min", "p_max",
		"v_peak", "v_peak_time", "v_rms", "i_peak", "i_peak_time", "i_rms", "p_peak", "p_peak_time",
		"thermal_rise", "thermal_coupled",
		"fit_mc_mean", "fit_mc_p5", "fit_mc_p95", "mc_warning_share", "mc_overstress_share")
	if err := cw.Write(head); err != nil {
		return err
	}

	for _, row := range r.Rows {
		c := row.Component
		element := ""
		if c.Element != nil {
			element = c.Element.Name
		}
		rec := []string{c.Ref, element, c.Class, strings.Join(c.Tags, " "), c.Text("package"), c.Text("value")}
		if c.Element != nil && !math.IsNaN(c.Element.Value) {
			rec[5] = number(c.Element.Value)
		}

		for _, col := range []string{"V", "I", "P", "T"} {
			if q, ok := quantity(row, col); ok {
				rec = append(rec, number(q.Value), q.Source.String())
			} else {
				rec = append(rec, "", "")
			}
		}

		fitErr := ""
		if row.FIT != nil && row.FIT.Err != nil {
			fitErr = row.FIT.Err.Error()
		}
		rec = append(rec, number(fit(row)), fitErr)

		for _, name := range baseRatios {
			rec = append(rec, nominalRatio(row, name))
		}
		level := ""
		if row.Derating != nil {
			level = row.Derating.Level().String()
		}
		rec = append(rec, level, strings.Join(Warnings(row), "; "), number(nominalFit(row)))

		worst := worstRatios(row)
		for _, name := range baseRatios {
			if x, ok := worst[name]; ok {
				rec = append(rec, ratio(x.Value), number(x.Corner.Temp))
			} else {
				rec = append(rec, "", "")
			}
		}
		level = ""
		if l, ok := cornerLevel(row); ok {
			level = l.String()
		}
		rec = append(rec, level, c.Text("manufacturer"), c.Text("mpn"))

		for _, x := range extraRatios {
			rec = append(rec, nominalRatio(row, x.name))
			if w, ok := worst[x.name]; ok {
				rec = append(rec, ratio(w.Value), number(w.Corner.Temp))
			} else {
				rec = append(rec, "", "")
			}
		}

		// The range over the sweep or the transient
		for _, col := range []string{"V", "I", "P"} {
			if lo, hi, _, _, ok := sweepRange(row, col); ok {
				rec = append(rec, number(lo), number(hi))
			} else if s, ok := stats(row, col); ok {
				rec = append(rec, number(s.Min), number(s.Max))
			} else {
				rec = append(rec, "", "")
			}
		}

		// Over the transient: the peaks, and the RMS values of V and I
		for _, col := range []string{"V", "I", "P"} {
			s, ok := stats(row, col)
			n := 3
			if col == "P" {
				n = 2 // its mean is p
			}
			if !ok {
				rec = append(rec, make([]string, n)...)
				continue
			}
			peak, at := s.Peak()
			rec = append(rec, number(peak), number(at))
			if n == 3 {
				rec = append(rec, number(s.RMS))
			}
		}

		// In the thermal network at the nominal temperature: the rise, and
		// the part of it from the other components
		if node := strings.ToUpper(c.Ref); r.Thermal != nil && len(r.ThermalRuns) > 0 && r.Thermal.Has(node) {
			sol := r.ThermalRuns[0].Solution
			rec = append(rec, number(sol.Rise[node]), number(sol.Coupled(node)))
		} else {
			rec = append(rec, "", "")
		}

		// Over the Monte Carlo runs
		if x := row.MonteCarlo; x != nil {
			rec = append(rec, number(mean(x.FIT)), number(percentile(x.FIT, 0.05)), number(percentile(x.FIT, 0.95)),
				ratio(x.Share(derating.Warning)), ratio(x.Share(derating.Overstress)))
		} else {
			rec = append(rec, "", "", "", "", "")
		}

		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// nominalRatio is a ratio of the nominal derating check of a row, or "".
func nominalRatio(row Row, name string) string {
	if row.Derating != nil {
		for _, x := range row.Derating.Ratios {
			if x.Name == name {
				return ratio(x.Value)
			}
		}
	}
	return ""
}

func ratio(v float64) string {
	return strconv.FormatFloat(v, 'g', 4, 64)
}

func number(v float64) string {
	if math.IsNaN(v) {
		return ""
	}
	return strconv.FormatFloat(v, 'g', 6, 64)
}
