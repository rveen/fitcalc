package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestTutorial: the example project of doc/tutorial.md gives what the
// tutorial shows.
func TestTutorial(t *testing.T) {
	if !haveNgspice() {
		t.Skip("ngspice not found")
	}
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"-p", "../../doc/tutorial/bom-v1.csv", "-p", "../../doc/tutorial/parts.csv", "-m", "../../doc/tutorial/mission.csv", "../../doc/tutorial/sensor.net"},
			[]string{`R7: P/Pmax = 109% (0.1094 W of 0.1 W): overstress`, "fitcalc: 15 components, 105 FIT in total (1 without FIT), 1 overstressed"}},
		{[]string{"../../doc/tutorial"},
			[]string{`R7: at 85 °C (full-op): P/Pmax = 52% (0.107 W of 0.2059 W): warning (limit 50 %)`, "fitcalc: 15 components, 106 FIT in total;"}},
		{[]string{"--sweep", "V1 9 16 0.5", "../../doc/tutorial"},
			[]string{`D2: I/Imax = 65% (0.01294 A of 0.02 A) with V1 = 16 V: warning (limit 60 %)`, "105 FIT in total"}},
		{[]string{"--tran", "10u 5m 1m", "../../doc/tutorial"},
			[]string{"106 FIT in total; at 32, 60 and 85 °C: 0 overstressed, 0 derating warnings"}},
		{[]string{"--thermal", "../../doc/tutorial/thermal.csv", "../../doc/tutorial"},
			[]string{"D2: no FIT: phase full-op: LED D2: junction temperature 102°C exceeds Tmax 100°C", "96.2 FIT in total (1 without FIT)"}},
		{[]string{"--mc", "100", "--vary", "V1=15%", "--seed", "1", "../../doc/tutorial"},
			[]string{"R7: Monte Carlo: above a derating limit in 64 % of the runs (largest P/Pmax = 69.9 %)", "total FIT 105 to 108 (5 % to 95 %)"}},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(append([]string{"-o", "-"}, tc.args...), &stdout, &stderr); code != exitOK {
			t.Fatalf("%q: exit status %d:\n%s", tc.args, code, stderr.String())
		}
		for _, want := range tc.want {
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("%q: stderr without %q:\n%s", tc.args, want, stderr.String())
			}
		}
	}
}
