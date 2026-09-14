package parts

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rveen/golib/csv"
)

func TestExpandRefs(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
		err  bool
	}{
		{"R1", []string{"R1"}, false},
		{"R1,R2,R5", []string{"R1", "R2", "R5"}, false},
		{"R1-R3, R5", []string{"R1", "R2", "R3", "R5"}, false},
		{"C1-3;C7 C9", []string{"C1", "C2", "C3", "C7", "C9"}, false},
		{"U1-A", []string{"U1-A"}, false}, // a unit, not a range
		{"R3-R1", []string{"R3-R1"}, true},
		{"R1-C3", []string{"R1-C3"}, true},
	} {
		got, err := ExpandRefs(tc.in)
		if !reflect.DeepEqual(got, tc.want) || (err != nil) != tc.err {
			t.Errorf("ExpandRefs(%q) = %q, %v; want %q, error %v", tc.in, got, err, tc.want, tc.err)
		}
	}
}

func TestFieldName(t *testing.T) {
	for in, want := range map[string]string{
		"Reference": "reference", "Refs": "reference", "Designator": "reference", "Qty": "qty", "DNP": "dnp",
		"MPN": "mpn", "Manufacturer_Part_Number": "mpn", "Mfr. Part #": "mpn", "Manufacturer": "manufacturer",
		"name": "name", "Tmax": "tmax", "Operating Temp": "operating_temp",
	} {
		if got := fieldName(in); got != want {
			t.Errorf("fieldName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLoadKiCad: a KiCad BOM export with grouped references, DNP, and types
// found by part number and footprint.
func TestLoadKiCad(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"bom.csv": `"Reference","Value","Footprint","Qty","DNP","MPN"
"R1-R3,R7","10k","Resistor_SMD:R_0603_1608Metric","4","",""
"R4","1k","Resistor_SMD:R_0805_2012Metric","2","",""
"D1","1N4148W","Diode_SMD:D_SOD-123","1","","1N4148W"
"Q1","BC847","Package_TO_SOT_SMD:SOT-23","1","","bc847"
"U1","LM358","Package_SO:SOIC-8_3.9x4.9mm_P1.27mm","1","","LM358"
"C5,C6","100n","Capacitor_SMD:C_0603_1608Metric","2","DNP",""
"J1","","TerminalBlock_Phoenix:TerminalBlock_Phoenix_MKDS-1,5-2_1x02_P5.00mm_Horizontal","1","","1715721"
`,
		"db.csv": "name, class, tmax, mpns, footprints\n" +
			"R0603, R, 155, , Resistor_SMD:R_0603*\n" +
			"D1N4148, D, 150, 1N4148W 1N4148WS,\n" +
			"BC847, Q, 150, ,\n" +
			"TB2, J, 105, 1715721,\n",
	})
	bom, db := filepath.Join(dir, "bom.csv"), filepath.Join(dir, "db.csv")
	p, err := Load([]string{bom, db})
	if err != nil {
		t.Fatal(err)
	}

	if p.Format != FormatKiCad {
		t.Errorf("format %q", p.Format)
	}
	if got := p.Refs(); !reflect.DeepEqual(got, []string{"D1", "J1", "Q1", "R1", "R2", "R3", "R4", "R7", "U1"}) {
		t.Errorf("refs %v", got)
	}
	if !reflect.DeepEqual(p.DNP, []string{"C5", "C6"}) {
		t.Errorf("DNP %v", p.DNP)
	}

	for ref, want := range map[string]string{"R2": "R0603", "D1": "D1N4148", "Q1": "BC847", "J1": "TB2", "R4": "", "U1": ""} {
		if got := p.Items[ref]["type"].Value; got != want {
			t.Errorf("%s: type %q, want %q", ref, got, want)
		}
	}
	for ref, want := range map[string][]string{
		"R2": {"one of R1-R3,R7 in the BOM", "type R0603 from the footprint Resistor_SMD:R_0603_1608Metric"},
		"D1": {"type D1N4148 from the part number 1N4148W"},
		"Q1": {"type BC847 from the part number bc847"},
	} {
		if got := p.Notes[ref]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: notes %q, want %q", ref, got, want)
		}
	}

	// Values keep their commas, and point to the KiCad file
	if got := p.Items["J1"]["footprint"].Value; got != "TerminalBlock_Phoenix:TerminalBlock_Phoenix_MKDS-1,5-2_1x02_P5.00mm_Horizontal" {
		t.Errorf("J1 footprint %q", got)
	}
	if got := p.Items["R2"]["value"].Sources; !reflect.DeepEqual(got, []csv.Source{{File: bom, Name: "R2"}}) {
		t.Errorf("R2 value sources %v", got)
	}
	if f := p.Items["D1"]["tmax"]; f.Value != "150" || f.Sources[0].File != db {
		t.Errorf("D1 tmax %+v", f)
	}

	w := strings.Join(p.Warnings, "\n")
	for _, want := range []string{"R4: quantity 2 for 1 references", "part numbers without a type in the parts files: LM358 (U1)"} {
		if !strings.Contains(w, want) {
			t.Errorf("warnings without %q:\n%s", want, w)
		}
	}
	if len(p.Warnings) != 2 {
		t.Errorf("warnings:\n%s", w)
	}
}

// TestLoadFidesGroups: a fides BOM can group references too, and the first
// row of a reference wins.
func TestLoadFidesGroups(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"bom.csv": "name, type, dnp\n\"R1,R2\", R0603,\nR3-R4, R0603, x\nr1, R1206,\n",
		"db.csv":  "name, class\nR0603, R\nR1206, R\n",
	})
	p, err := Load([]string{filepath.Join(dir, "bom.csv"), filepath.Join(dir, "db.csv")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Format != FormatFides || !reflect.DeepEqual(p.Refs(), []string{"R1", "R2"}) || !reflect.DeepEqual(p.DNP, []string{"R3", "R4"}) {
		t.Errorf("format %s, refs %v, DNP %v", p.Format, p.Refs(), p.DNP)
	}
	if p.Items["R1"]["type"].Value != "R0603" ||
		!slices.ContainsFunc(p.Warnings, func(w string) bool { return strings.Contains(w, "R1 appears more than once") }) {
		t.Errorf("R1 type %q, warnings %q", p.Items["R1"]["type"].Value, p.Warnings)
	}
}
