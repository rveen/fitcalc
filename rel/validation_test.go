package rel

import (
	"context"
	"encoding/csv"
	"math"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/spice"
	"github.com/rveen/fitcalc/stress"
)

// validationTolerance is the relative difference allowed between fitcalc and
// the reference. The reference takes the stresses with 6 significant digits.
const validationTolerance = 1e-4

// TestFIDESReference compares the FIT values of the probe circuit with the
// independent FIDES 2022 reference calculation in testdata/validation
// (fides_ref.py, written from the guide without the fides library; its output
// is reference.csv).
func TestFIDESReference(t *testing.T) {
	name := os.Getenv("NGSPICE")
	if name == "" {
		name = "ngspice"
	}
	if _, err := exec.LookPath(name); err != nil {
		t.Skip("ngspice not found")
	}

	f, err := os.Open("../testdata/validation/reference.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{}
	for _, r := range records[1:] {
		v, err := strconv.ParseFloat(r[1], 64)
		if err != nil {
			t.Fatal(err)
		}
		want[r[0]] = v
	}

	c, err := spice.Parse("../testdata/probe.net")
	if err != nil {
		t.Fatal(err)
	}
	op, err := (&spice.Runner{}).OP(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	values, err := spice.ReadOP(op.Raw)
	if err != nil {
		t.Fatal(err)
	}
	stresses, _ := stress.DeriveAll(c, values)
	p, err := parts.Load([]string{"../testdata/probe.bom.csv", "../testdata/parts.db.csv"})
	if err != nil {
		t.Fatal(err)
	}
	comps, _, _ := parts.Merge(c, stresses, p)

	got := map[string]float64{}
	for _, r := range FIT(comps, mission(t)) {
		got[r.Component.Ref] = r.FIT
	}

	for ref, w := range want {
		g, ok := got[ref]
		if !ok {
			t.Errorf("%s: not in the fitcalc results", ref)
			continue
		}
		if math.Abs(g-w) > validationTolerance*w {
			t.Errorf("%s: fitcalc %.6g FIT, FIDES 2022 reference %.6g FIT (%+.4f %%)", ref, g, w, 100*(g/w-1))
		}
	}
	if len(want) < 11 {
		t.Errorf("%d reference values, want the 11 of the probe circuit's models", len(want))
	}
}
