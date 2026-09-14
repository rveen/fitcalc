package parts

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/rveen/fides"
	"github.com/rveen/fitcalc/spice"
)

// footprints are footprints of KiCad's libraries, with what they tell.
var footprints = map[string]Footprint{
	"Resistor_SMD:R_0603_1608Metric":                                                 {Package: "0603", Tags: []string{"thick"}},
	"Resistor_SMD:R_0603_1608Metric_Pad0.98x0.95mm_HandSolder":                       {Package: "0603", Tags: []string{"thick"}},
	"Resistor_SMD:R_MiniMELF_MMA-0204":                                               {Tags: []string{"melf"}},
	"Resistor_SMD:R_Array_Convex_4x0603":                                             {Tags: []string{"network"}},
	"Resistor_THT:R_Axial_DIN0207_L6.3mm_D2.5mm_P10.16mm_Horizontal":                 {Tags: []string{"tht"}},
	"Capacitor_SMD:C_0805_2012Metric":                                                {Package: "0805", Tags: []string{"cer"}},
	"Capacitor_SMD:CP_Elec_6.3x5.8":                                                  {Tags: []string{"alu"}},
	"Capacitor_THT:CP_Radial_D5.0mm_P2.00mm":                                         {Tags: []string{"alu", "tht"}},
	"Capacitor_Tantalum_SMD:CP_EIA-3528-21_Kemet-B":                                  {Tags: []string{"tant"}},
	"LED_SMD:LED_0603_1608Metric":                                                    {Package: "0603", Tags: []string{"led"}},
	"Diode_SMD:D_SOD-123":                                                            {Package: "SOD123"},
	"Diode_SMD:D_SOD-323":                                                            {Package: "SOD323"},
	"Diode_SMD:D_SMA":                                                                {Package: "SMA"},
	"Diode_SMD:D_SMB_Handsoldering":                                                  {Package: "SMB"},
	"Diode_SMD:D_MiniMELF":                                                           {Package: "SOD80"},
	"Diode_THT:D_DO-41_SOD81_P10.16mm_Horizontal":                                    {Package: "DO41", Tags: []string{"tht"}},
	"Package_TO_SOT_SMD:SOT-23":                                                      {Package: "SOT23"},
	"Package_TO_SOT_SMD:SOT-23-3":                                                    {Package: "SOT23"},
	"Package_TO_SOT_SMD:SOT-23-5":                                                    {Package: "SOT23-5"},
	"Package_TO_SOT_SMD:SOT-23-6":                                                    {Package: "SOT23-6"},
	"Package_TO_SOT_SMD:SOT-223-3_TabPin2":                                           {Package: "SOT223"},
	"Package_TO_SOT_SMD:SOT-89-3":                                                    {Package: "SOT89"},
	"Package_TO_SOT_SMD:SOT-363_SC-70-6":                                             {Package: "SOT363"},
	"Package_TO_SOT_SMD:TO-252-2":                                                    {Package: "DPAK"},
	"Package_TO_SOT_SMD:TO-263-2":                                                    {Package: "D2PAK"},
	"Package_TO_SOT_THT:TO-220-3_Vertical":                                           {Package: "TO220", Tags: []string{"tht"}},
	"Package_TO_SOT_THT:TO-92_Inline":                                                {Package: "TO92", Tags: []string{"tht"}},
	"Package_TO_SOT_THT:TO-247-3_Vertical":                                           {Package: "TO247", Tags: []string{"tht"}},
	"Package_SO:SOIC-8_3.9x4.9mm_P1.27mm":                                            {Package: "SOIC8"},
	"Package_SO:TSSOP-16_4.4x5mm_P0.65mm":                                            {Package: "TSSOP16"},
	"Package_SO:MSOP-10_3x3mm_P0.5mm":                                                {Package: "MSOP10"},
	"Package_SO:SSOP-20_5.3x7.2mm_P0.65mm":                                           {Package: "SSOP20"},
	"Package_SO:PowerPAK_SO-8_Single":                                                {Package: "SO8P"},
	"Package_DFN_QFN:QFN-32-1EP_5x5mm_P0.5mm_EP3.45x3.45mm":                          {Package: "QFN32"},
	"Package_DFN_QFN:WQFN-16-1EP_3x3mm_P0.5mm_EP1.75x1.75mm":                         {Package: "QFN16"},
	"Package_DFN_QFN:DFN-8-1EP_3x3mm_P0.5mm_EP1.66x2.38mm":                           {Package: "DFN8"},
	"Package_QFP:LQFP-64_10x10mm_P0.5mm":                                             {Package: "LQFP64"},
	"Package_QFP:TQFP-44_10x10mm_P0.8mm":                                             {Package: "TQFP44"},
	"Package_DIP:DIP-8_W7.62mm":                                                      {Package: "PDIP8", Tags: []string{"tht"}},
	"Package_BGA:BGA-256_17.0x17.0mm_Layout16x16_P1.0mm_Ball0.5mm":                   {Package: "PBGA256"},
	"Connector_PinHeader_2.54mm:PinHeader_1x02_P2.54mm_Vertical":                     {Pins: 2, Tags: []string{"tht"}},
	"Connector_PinHeader_2.54mm:PinHeader_2x05_P2.54mm_Vertical_SMD":                 {Pins: 10},
	"TerminalBlock_Phoenix:TerminalBlock_Phoenix_MKDS-1,5-2_1x02_P5.00mm_Horizontal": {Pins: 2, Tags: []string{"tht"}},
	"Inductor_SMD:L_Bourns_SRR1260":                                                  {},
}

func TestParseFootprint(t *testing.T) {
	for fp, want := range footprints {
		if got := ParseFootprint(fp); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseFootprint(%q) = %+v, want %+v", fp, got, want)
		}
	}
}

// TestFootprintPackages: every package name the footprints give, other than
// the sizes of chip parts (whose FIDES models have no package), is one that
// the fides library knows.
func TestFootprintPackages(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	for fp, f := range footprints {
		if f.Package == "" || strings.IndexFunc(f.Package, unicode.IsLetter) < 0 {
			continue
		}
		buf.Reset()
		p := fides.NewPackage(f.Package)
		if lrh, _, _, _ := p.FitBase(); lrh < 0 || buf.Len() > 0 {
			t.Errorf("%s: package %s unknown to fides: %s", fp, f.Package, buf.String())
		}
	}
}

func TestFootprintMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, fp string
		want        bool
	}{
		{"Resistor_SMD:R_0603*", "Resistor_SMD:R_0603_1608Metric", true},
		{"R_0603*", "Resistor_SMD:R_0603_1608Metric", true},
		{"Resistor_SMD:R_0603*", "Resistor_SMD:R_0805_2012Metric", false},
		{"Resistor_SMD:R_0603_1608Metric", "Resistor_SMD:R_0603_1608Metric_Pad0.98x0.95mm_HandSolder", false},
	} {
		if got := footprintMatch(tc.pattern, tc.fp); got != tc.want {
			t.Errorf("footprintMatch(%q, %q) = %v", tc.pattern, tc.fp, got)
		}
	}
}

// TestMergeFootprint: the footprint and the reference prefix fill in only
// what the parts files and the netlist leave out.
func TestMergeFootprint(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"bom.csv": "name, tags, package, npins, footprint\n" +
			"C1, , , , Capacitor_SMD:C_0603_1608Metric\n" +
			"C2, alu, , , Capacitor_SMD:C_0603_1608Metric\n" +
			"Q1, , SOT223, , Package_TO_SOT_SMD:SOT-23\n" +
			"J3, , , , Connector_PinHeader_2.54mm:PinHeader_2x05_P2.54mm_Vertical\n" +
			"J4, , , 3, Connector_PinHeader_2.54mm:PinHeader_2x05_P2.54mm_Vertical\n" +
			"LED1, , , , LED_SMD:LED_0603_1608Metric\n",
	})
	p, err := Load([]string{filepath.Join(dir, "bom.csv")})
	if err != nil {
		t.Fatal(err)
	}
	c := &spice.Circuit{Elements: []*spice.Element{{Name: "C1", Kind: "C"}, {Name: "C2", Kind: "C"}, {Name: "Q1", Kind: "Q"}}}
	comps, _, _ := Merge(c, nil, p)
	by := map[string]*Component{}
	for _, x := range comps {
		by[x.Ref] = x
	}

	for _, tc := range []struct {
		ref, class, pkg, npins string
		tags                   []string
	}{
		{"C1", "C", "0603", "", []string{"cer"}},
		{"C2", "C", "0603", "", []string{"alu"}},
		{"Q1", "Q", "SOT223", "", nil},
		{"J3", "J", "", "10", []string{"tht"}},
		{"J4", "J", "", "3", []string{"tht"}},
		{"LED1", "D", "0603", "", []string{"led"}},
	} {
		x := by[tc.ref]
		if x.Class != tc.class || x.Text("package") != tc.pkg || x.Text("npins") != tc.npins || !slices.Equal(x.Tags, tc.tags) {
			t.Errorf("%s: class %q, package %q, npins %q, tags %q; want %q, %q, %q, %q",
				tc.ref, x.Class, x.Text("package"), x.Text("npins"), x.Tags, tc.class, tc.pkg, tc.npins, tc.tags)
		}
	}
	if n := strings.Join(by["C1"].Notes, "; "); !strings.Contains(n, "package 0603 from the footprint Capacitor_SMD:C_0603_1608Metric") {
		t.Errorf("C1 notes %q", n)
	}
	if n := strings.Join(by["J3"].Notes, "; "); !strings.Contains(n, "class J from the reference prefix J") {
		t.Errorf("J3 notes %q", n)
	}
	// The parts are not changed: a second merge sees the same metadata
	if _, ok := p.Items["C1"]["package"]; ok {
		t.Error("Merge changed the parts metadata")
	}
}

func TestMergeDNP(t *testing.T) {
	dir := writeFiles(t, map[string]string{"bom.csv": "name, class, dnp\nR1, R,\nR2, R, DNP\n"})
	p, err := Load([]string{filepath.Join(dir, "bom.csv")})
	if err != nil {
		t.Fatal(err)
	}
	c := &spice.Circuit{Elements: []*spice.Element{{Name: "R1", Kind: "R"}, {Name: "R2", Kind: "R"}}}
	comps, simOnly, warnings := Merge(c, nil, p)
	if len(comps) != 1 || comps[0].Ref != "R1" || len(simOnly) != 0 {
		t.Errorf("components %v, simulation-only %v", comps, simOnly)
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "R2: DNP in the BOM, but netlist element R2 is simulated") {
		t.Errorf("warnings %q", warnings)
	}
}
