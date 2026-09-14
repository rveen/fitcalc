package rel

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/rveen/fides"
	"github.com/rveen/fitcalc/parts"
	"github.com/rveen/fitcalc/stress"
)

// Result is the FIDES result of one component.
type Result struct {
	Component *parts.Component
	FIT       float64          // failures in 10⁹ hours; NaN if it could not be calculated
	Err       error            // why there is no FIT
	Inputs    stress.Stress    // the working conditions given to FIDES (V, I, P, T), with their origin
	Fides     *fides.Component // the component as the fides library used it
	Warnings  []string         // assumptions, and the messages the fides library logged
	Phases    []float64        // FITPhases: the contribution of each mission phase to FIT
}

// substrateConductivity is the thermal conductivity of the board, in
// W/(m·K), assumed for the default thermal resistance of a package. FIDES
// distinguishes boards below and above 15 W/(m·K); 0 selects the values for
// an FR4-like board, the higher (conservative) ones.
const substrateConductivity = 0

// selfHeated are the classes whose FIDES model uses the temperature rise
// T = P·Rth.
var selfHeated = []string{"D", "Q", "U"}

// SelfHeated reports whether the FIDES model of class uses the temperature
// rise T (the stress t): diodes, transistors and ICs.
func SelfHeated(class string) bool {
	return slices.Contains(selfHeated, class)
}

// Mission is a FIDES mission profile.
type Mission = fides.Mission

// LoadMission reads a FIDES mission profile CSV file. Lines starting with #
// are comments, as in the other input files.
func LoadMission(file string) (*Mission, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	// The fides library reads a file without comments
	src := file
	lines := strings.Split(string(data), "\n")
	kept := slices.DeleteFunc(slices.Clone(lines), func(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), "#") })
	if len(kept) < len(lines) {
		tmp, err := os.CreateTemp("", "fitcalc-mission-*.csv")
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmp.Name())
		_, err = tmp.WriteString(strings.Join(kept, "\n"))
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
		}
		src = tmp.Name()
	}

	m := fides.NewMission()
	if err := m.FromCsv(src); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if len(m.Phases) == 0 || m.Ttotal <= 0 {
		return nil, fmt.Errorf("%s: no mission phases", file)
	}
	return m, nil
}

// FIT calculates the FIDES 2022 failure rate of every component for mission
// m.
func FIT(comps []*parts.Component, m *fides.Mission) []*Result {
	results := make([]*Result, 0, len(comps))
	for _, c := range comps {
		results = append(results, fit(c, m))
	}
	return results
}

// FITPhases is FIT with the stresses of each mission phase: phases[i] holds
// the components with the stresses of phase i of m, in the order of comps,
// or is nil for a phase with the stresses of comps.
//
// FIDES sums over the phases, each weighted by its share of the mission, so
// the FIT of a component is the sum of its contributions per phase, each
// calculated with the stresses of that phase. Inputs and Fides are those of
// comps.
func FITPhases(comps []*parts.Component, phases [][]*parts.Component, m *Mission) []*Result {
	results := make([]*Result, 0, len(comps))
	for j, c := range comps {
		r := fit(c, m)
		r.FIT, r.Err, r.Phases = 0, nil, nil
		for i, ph := range m.Phases {
			pc := c
			if phases[i] != nil && phases[i][j] != nil {
				pc = phases[i][j]
			}
			x := fit(pc, &Mission{Ttotal: m.Ttotal, Phases: []*fides.Phase{ph}})
			for _, w := range x.Warnings {
				if !slices.Contains(r.Warnings, w) {
					r.Warnings = append(r.Warnings, w)
				}
			}
			if x.Err != nil && r.Err == nil {
				r.Err = x.Err
				if pc != c {
					r.Err = fmt.Errorf("phase %s: %w", ph.Name, x.Err)
				}
			}
			r.Phases = append(r.Phases, x.FIT)
			r.FIT += x.FIT
		}
		if r.Err != nil {
			r.FIT, r.Phases = math.NaN(), nil
		}
		results = append(results, r)
	}
	return results
}

// Total returns the sum of the FIT values that could be calculated, and the
// number of components without one.
func Total(results []*Result) (float64, int) {
	var total float64
	var missing int
	for _, r := range results {
		if math.IsNaN(r.FIT) {
			missing++
		} else {
			total += r.FIT
		}
	}
	return total, missing
}

func fit(c *parts.Component, m *fides.Mission) (r *Result) {

	r = &Result{Component: c, FIT: math.NaN(), Inputs: stress.Stress{}}

	// The fides library logs some problems, such as an unknown package: they
	// become warnings of the component. It can also panic on inputs it does
	// not expect.
	var buf bytes.Buffer
	w, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(w)
		log.SetFlags(flags)
		if p := recover(); p != nil {
			r.FIT, r.Err = math.NaN(), fmt.Errorf("fides: %v", p)
		}
		for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if l != "" {
				r.Warnings = append(r.Warnings, "fides: "+l)
			}
		}
	}()

	r.Fides = r.component()
	if ta, ok := c.Stress["ta"]; ok && ta.Value != 0 {
		m = localAmbient(m, ta.Value)
		r.Inputs["Ta"] = ta
	}
	f, err := fides.FIT(r.Fides, m)
	switch {
	case err != nil:
		r.Err = err
	case math.IsNaN(f) || math.IsInf(f, 0):
		r.Err = errors.New("the fides library returned no value")
	default:
		r.FIT = f
	}
	return r
}

// localAmbient returns mission m with the ambient and the largest
// temperature of its operating phases raised by rise, in K: the ambient of a
// component heated by the others (the stress ta, from a thermal network).
// Phases that are not operating dissipate no power.
func localAmbient(m *fides.Mission, rise float64) *fides.Mission {
	out := &fides.Mission{Ttotal: m.Ttotal}
	for _, ph := range m.Phases {
		if ph.On {
			x := *ph
			x.Tamb += rise
			x.Tmax += rise
			ph = &x
		}
		out.Phases = append(out.Phases, ph)
	}
	return out
}

// component builds the fides component.
func (r *Result) component() *fides.Component {

	c := r.Component
	fc := fides.NewComponent(c.Ref)

	fc.Class = c.Class
	fc.Tags = slices.Clone(c.Tags)
	fc.Type = c.Text("type")
	fc.Description = c.Text("description")
	fc.Block = c.Text("block")
	fc.Package = strings.ToUpper(c.Text("package"))

	if c.Element != nil && !math.IsNaN(c.Element.Value) {
		fc.Value = c.Element.Value
	} else if v, ok := c.Number("value"); ok {
		fc.Value = v
	}
	if v, ok := c.Number("tolerance"); ok {
		fc.Tolerance = v
	}
	fc.N = integer(c, "ndevices")
	fc.Np = integer(c, "npins")
	fc.Layers = integer(c, "layers")
	fc.Mounts = integer(c, "mounts")

	for field, dst := range map[string]*float64{
		"vmax": &fc.Vmax, "vpmax": &fc.Vpmax, "pmax": &fc.Pmax, "imax": &fc.Imax,
		"tmax": &fc.Tmax, "rtha": &fc.Rtha, "tc": &fc.TC,
		"piclass": &fc.PiClass, "pitechno": &fc.PiTechno,
	} {
		if v, ok := c.Number(field); ok {
			*dst = v
		}
	}

	// Working conditions. FIDES takes magnitudes; V is the reverse voltage
	// for a diode.
	s := c.Stress
	for _, in := range []struct {
		name, key string
		dst       *float64
	}{
		{"V", parts.VoltageKey(c.Class), &fc.V},
		{"I", "i", &fc.I},
		{"P", "p", &fc.P},
	} {
		if q, ok := s[in.key]; ok {
			q.Value = math.Abs(q.Value)
			*in.dst = q.Value
			if in.key != strings.ToLower(in.name) {
				q.Note = in.key + ": " + q.Note
			}
			r.Inputs[in.name] = q
		}
	}

	switch q, ok := s["t"]; {
	case ok:
		fc.T = q.Value
		r.Inputs["T"] = q
	case slices.Contains(selfHeated, c.Class):
		p, hasP := s["p"]
		rth, src := r.thermalResistance()
		switch {
		case !hasP:
			r.Warnings = append(r.Warnings, "self-heating not included: no power")
		case rth <= 0:
			r.Warnings = append(r.Warnings, "self-heating not included: no thermal resistance (set rth in the parts files)")
		default:
			fc.T = math.Abs(p.Value) * rth
			r.Inputs["T"] = stress.Quantity{
				Value:  fc.T,
				Unit:   "°C",
				Source: stress.Derived,
				Note:   fmt.Sprintf("p·rth, rth = %.4g K/W %s", rth, src),
			}
		}
	}

	return fc
}

// thermalResistance returns the junction-to-ambient thermal resistance of a
// semiconductor, and where it comes from: the parts field rth, else rtha,
// else the FIDES default for its package.
func (r *Result) thermalResistance() (float64, string) {
	c := r.Component
	for _, field := range []string{"rth", "rtha"} {
		if v, ok := c.Number(field); ok && v > 0 {
			return v, "from " + c.Source(field)
		}
	}
	pkg := strings.ToUpper(c.Text("package"))
	if pkg == "" {
		return 0, ""
	}
	v := fides.NewPackage(pkg).Rtha(substrateConductivity)
	if n := c.Text("npins"); v <= 0 && n != "" {
		// An IC family without its pin count, such as SOIC with npins 8
		pkg += n
		v = fides.NewPackage(pkg).Rtha(substrateConductivity)
	}
	if math.IsNaN(v) || v <= 0 {
		return 0, ""
	}
	return v, "(FIDES default for package " + pkg + " on an FR4 board)"
}

func integer(c *parts.Component, field string) int {
	n, _ := strconv.Atoi(c.Text(field))
	return n
}
