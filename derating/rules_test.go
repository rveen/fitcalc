package derating

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rveen/fitcalc/stress"
)

func TestLoadRules(t *testing.T) {
	rs, err := LoadRules("../testdata/derating.csv")
	if err != nil {
		t.Fatal(err)
	}
	if !rs.Custom() || rs[len(rs)-1].Source != "default" {
		t.Errorf("rules %v: want the file's rules, then the default one", rs)
	}
	if r := rs[2]; r.Class != "R" || len(r.Tags) != 1 || r.Tags[0] != "thick" || r.Rating != "pmax" || r.Limit != 0.5 ||
		!strings.HasSuffix(r.Source, "derating.csv:11") {
		t.Errorf("rule 3: %+v", r)
	}
	if Default.Custom() {
		t.Error("the default rules are custom")
	}

	dir := t.TempDir()
	for name, content := range map[string]string{
		"rating.csv":  "class, rating, limit\nR, pmaks, 50\n",
		"limit.csv":   "class, rating, limit\nR, pmax, 150\n",
		"columns.csv": "class, limit\nR, 50\n",
		"empty.csv":   "class, rating, limit\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRules(path); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestRuleFor(t *testing.T) {
	rs := Rules{
		{Class: "*", Rating: "*", Limit: 0.8, Source: "any"},
		{Class: "*", Rating: "pmax", Limit: 0.7, Source: "any pmax"},
		{Class: "R", Rating: "*", Limit: 0.65, Source: "R"},
		{Class: "R", Rating: "pmax", Limit: 0.6, Source: "R pmax"},
		{Class: "R", Tags: []string{"thick"}, Rating: "pmax", Limit: 0.5, Source: "R thick pmax"},
		{Class: "R", Tags: []string{"thick"}, Rating: "pmax", Limit: 0.4, Source: "R thick pmax, again"},
	}
	for _, tc := range []struct {
		class  string
		tags   []string
		rating string
		want   string
	}{
		{"R", []string{"thick", "smd"}, "pmax", "R thick pmax"},
		{"R", nil, "pmax", "R pmax"},
		{"R", nil, "vmax", "R"},
		{"C", nil, "pmax", "any pmax"},
		{"C", []string{"thick"}, "vmax", "any"},
	} {
		c := comp(tc.class, tc.tags, nil, nil)
		if got := rs.For(c, tc.rating).Source; got != tc.want {
			t.Errorf("%s %v %s: rule %q, want %q", tc.class, tc.tags, tc.rating, got, tc.want)
		}
	}
	if r := Rules(nil).For(comp("R", nil, nil, nil), "pmax"); r.Limit != WarnRatio || r.Source != "default" {
		t.Errorf("no rules: %+v", r)
	}
}

// TestCheckRules: a ratio above the limit of its rule is a warning; above
// 100 % an overstress, whatever the rule.
func TestCheckRules(t *testing.T) {
	rs := Rules{{Class: "R", Tags: []string{"thick"}, Rating: "pmax", Limit: 0.5, Source: "r.csv:2"}}
	rs = append(rs, Default...)
	for _, tc := range []struct {
		p    float64
		want Level
	}{
		{0.05, OK}, {0.06, Warning}, {0.09, Warning}, {0.11, Overstress},
	} {
		r := rs.Check(comp("R", []string{"thick"}, stress.Stress{"p": q(tc.p, "W"), "v": q(10, "V")}, map[string]string{"pmax": "0.1", "vmax": "50"}))
		x := ratio(t, r, "P/Pmax")
		if x.Level != tc.want || x.Limit != 0.5 || x.Rule != "r.csv:2" {
			t.Errorf("P = %g W: %+v, want %v with the rule r.csv:2", tc.p, x, tc.want)
		}
		if v := ratio(t, r, "V/Vmax"); v.Limit != WarnRatio || v.Rule != "default" {
			t.Errorf("V/Vmax %+v: want the default rule", v)
		}
	}
}

// TestWorst: over the points of a sweep, each ratio at its largest.
func TestWorst(t *testing.T) {
	ratings := map[string]string{"pmax": "0.1", "vmax": "50"}
	var rs []*Result
	for _, p := range []float64{0.05, 0.09, 0.07} {
		rs = append(rs, Check(comp("R", nil, stress.Stress{"p": q(p, "W"), "v": q(40-100*p, "V")}, ratings)))
	}
	w := Worst(rs[0].Component, rs, []string{"V1 = 10 V", "V1 = 12 V", "V1 = 14 V"})
	p := ratio(t, w, "P/Pmax")
	if math.Abs(p.Value-0.9) > 1e-12 || p.At != "V1 = 12 V" || p.Level != Warning || w.Level() != Warning {
		t.Errorf("P/Pmax %+v, result %v", p, w.Level())
	}
	if s := p.String(); s != "P/Pmax = 90% (0.09 W of 0.1 W) with V1 = 12 V" {
		t.Errorf("String() = %q", s)
	}
	if v := ratio(t, w, "V/Vmax"); v.At != "V1 = 10 V" || math.Abs(v.Value-0.7) > 1e-12 {
		t.Errorf("V/Vmax %+v", v)
	}
}

// TestDeviceRatios: the gate, collector-base and emitter-base voltages, and
// the transient voltage rating.
func TestDeviceRatios(t *testing.T) {
	m := CheckAt(comp("Q", []string{"mos"}, stress.Stress{"v": q(24, "V"), "vgs": q(-18, "V")},
		map[string]string{"vmax": "60", "vpmax": "30", "vgsmax": "20"}), 25)
	if x := ratio(t, m, "Vgs/Vgsmax"); x.Value != 0.9 || x.Level != Warning || x.Key != "vgs" {
		t.Errorf("Vgs/Vgsmax %+v", x)
	}
	if x := ratio(t, m, "V/Vpmax"); x.Value != 0.8 || x.Level != OK {
		t.Errorf("V/Vpmax %+v", x)
	}
	b := Check(comp("Q", nil, stress.Stress{"vcb": q(30, "V"), "veb": q(7, "V")},
		map[string]string{"vcbmax": "50", "vebmax": "6"}))
	if x := ratio(t, b, "Vcb/Vcbmax"); x.Value != 0.6 || x.Level != OK {
		t.Errorf("Vcb/Vcbmax %+v", x)
	}
	if x := ratio(t, b, "Veb/Vebmax"); x.Level != Overstress {
		t.Errorf("Veb/Vebmax %+v", x)
	}
}
