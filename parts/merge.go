package parts

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/rveen/electronics"
	"github.com/rveen/fitcalc/spice"
	"github.com/rveen/fitcalc/stress"
	"github.com/rveen/golib/csv"
)

// Component is a reliability item: a netlist element, a part in the parts
// files, or both.
type Component struct {
	Ref      string               // the reference, upper case: the BOM name, or the element name
	Element  *spice.Element       // nil for a part that is not in the netlist
	Meta     map[string]csv.Field // parts metadata by lower-case field name; nil if not in the parts files
	Class    string               // FIDES class: R, C, L, D, Q, U, J, X, PCB; "" if unknown
	Tags     []string
	Stress   stress.Stress // nil if there is no stress at all
	Notes    []string      // where the class, tags and derived values come from
	Warnings []string
}

// InNet reports whether the component is in the netlist.
func (c *Component) InNet() bool { return c.Element != nil }

// InBom reports whether the component is in the parts files.
func (c *Component) InBom() bool { return c.Meta != nil }

// Text returns a metadata field, or "".
func (c *Component) Text(field string) string {
	return c.Meta[field].Value
}

// Number returns a metadata field as a number, with the BOM conventions of
// electronics.Value (1M is mega, 5% is 5).
func (c *Component) Number(field string) (float64, bool) {
	s := c.Text(field)
	if s == "" {
		return math.NaN(), false
	}
	v := electronics.Value(s)
	return v, !math.IsNaN(v)
}

// Source tells where a metadata field comes from: "db.csv (R0603)", or
// several such for tags.
func (c *Component) Source(field string) string {
	var s []string
	for _, src := range c.Meta[field].Sources {
		s = append(s, src.File+" ("+src.Name+")")
	}
	return strings.Join(s, ", ")
}

func (c *Component) warn(format string, a ...any) {
	c.Warnings = append(c.Warnings, fmt.Sprintf(format, a...))
}

func (c *Component) note(format string, a ...any) {
	c.Notes = append(c.Notes, fmt.Sprintf(format, a...))
}

// VoltageKey is the stress quantity that is the working voltage of a
// component of the given class, the one compared with vmax: the reverse
// voltage vr for diodes, v for any other class.
func VoltageKey(class string) string {
	if class == "D" {
		return "vr"
	}
	return "v"
}

// kindClass is the FIDES class of the SPICE element kinds that are
// reliability items, and the tag the kind implies.
var kindClass = map[string]struct{ class, tag string }{
	"R": {"R", ""}, "C": {"C", ""}, "L": {"L", ""}, "D": {"D", ""},
	"Q": {"Q", ""}, "M": {"Q", "mos"}, "J": {"Q", "jfet"},
}

// transistorTags are the tags that give the type of a class Q component.
var transistorTags = []string{"mos", "mosfet", "jfet", "igbt", "gan", "gaas", "triac", "thyristor"}

// overrides are the parts fields that override stress quantities, with their
// units. The stress quantity of v depends on the class (VoltageKey).
var overrides = []struct{ field, unit string }{
	{"v", "V"}, {"i", "A"}, {"p", "W"}, {"t", "°C"},
}

// Merge matches the netlist elements with the parts and returns the
// components, in netlist order followed by the parts that are not in the
// netlist; the simulation-only elements that are not in the parts files; and
// general warnings. p can be nil: there are no parts files.
//
// An element stands for the part whose reference is its name, or its name
// without the device letter KiCad adds (spice.Ref). The class comes from the
// parts files, or else from the element kind; M and J imply the tags mos and
// jfet. The parts fields v, i, p and t override the stress quantities, and
// the power of an inductor with a dcr field is i²·dcr.
func Merge(c *spice.Circuit, stresses map[string]stress.Stress, p *Parts) ([]*Component, []*spice.Element, []string) {

	var comps []*Component
	var simOnly []*spice.Element
	var warnings []string

	known := func(ref string) bool {
		if p == nil {
			return false
		}
		_, ok := p.Items[ref]
		return ok || slices.Contains(p.DNP, ref)
	}
	byRef := map[string]*Component{}

	for _, e := range c.Elements {
		ref := spice.Ref(e.Name, known)
		var meta map[string]csv.Field
		if p != nil {
			if slices.Contains(p.DNP, ref) {
				warnings = append(warnings, fmt.Sprintf("%s: DNP in the BOM, but netlist element %s is simulated; it is left out of the components",
					ref, e.Name))
				continue
			}
			meta = p.Items[ref]
		}
		s := stresses[e.Name]
		if s == nil && meta == nil {
			simOnly = append(simOnly, e)
			continue
		}
		if prev := byRef[ref]; prev != nil {
			warnings = append(warnings, fmt.Sprintf("%s: elements %s and %s both stand for it; %s is ignored",
				ref, prev.Element.Name, e.Name, e.Name))
			continue
		}

		comp := &Component{Ref: ref, Element: e, Meta: maps.Clone(meta), Stress: maps.Clone(s)}
		if ref != e.Name {
			comp.note("netlist element %s", e.Name)
		}
		if p != nil {
			comp.Notes = append(comp.Notes, p.Notes[ref]...)
		}
		switch {
		case s == nil:
			comp.warn("no SPICE stress: %s is simulated as a %s", e.Name, KindName(e.Kind))
		case meta == nil && p != nil:
			comp.warn("not in the parts files: no FIDES metadata")
		}
		byRef[ref] = comp
		comps = append(comps, comp)
	}

	if p == nil {
		warnings = append(warnings, "no parts files (-p): no FIDES metadata for any component")
	} else {
		for _, ref := range p.Refs() {
			if byRef[ref] != nil {
				continue
			}
			comp := &Component{Ref: ref, Meta: maps.Clone(p.Items[ref]), Notes: slices.Clone(p.Notes[ref])}
			comp.warn("not in the netlist: no SPICE stress")
			comps = append(comps, comp)
		}
	}

	for _, comp := range comps {
		comp.classify()
		comp.fromFootprint()
		comp.checkValue()
		comp.override()
		comp.derive()
	}
	return comps, simOnly, warnings
}

// classify sets the class and the tags: the class from the parts files, else
// from the SPICE element type, else from the reference prefix.
func (c *Component) classify() {

	kind := ""
	if c.Element != nil {
		kind = c.Element.Kind
	}
	kc, fromKind := kindClass[kind]
	prefix, _, _ := splitRef(c.Ref)
	pc, fromPrefix := prefixClass[prefix]
	prefixTag := ""

	switch cl := strings.ToUpper(c.Text("class")); {
	case cl != "":
		c.Class = cl
		if fromKind && kc.class != cl {
			c.warn("class %s from %s, but netlist element %s is a %s (class %s)",
				cl, c.Source("class"), c.Element.Name, KindName(kind), kc.class)
		}
	case fromKind:
		c.Class = kc.class
		c.note("class %s from the SPICE element type (%s)", kc.class, KindName(kind))
	case fromPrefix:
		c.Class, prefixTag = pc.class, pc.tag
		c.note("class %s from the reference prefix %s", pc.class, prefix)
	default:
		c.warn("no class: set it in the parts files")
	}

	c.Tags = strings.Fields(strings.ToLower(c.Text("tags")))
	if fromKind && kc.tag != "" && !slices.ContainsFunc(c.Tags, func(t string) bool {
		return slices.Contains(transistorTags, t)
	}) {
		c.Tags = append(c.Tags, kc.tag)
		c.note("tag %s from the SPICE element type (%s)", kc.tag, KindName(kind))
	}
	if prefixTag != "" && !slices.Contains(c.Tags, prefixTag) {
		c.Tags = append(c.Tags, prefixTag)
		c.note("tag %s from the reference prefix %s", prefixTag, prefix)
	}
}

// checkValue compares the value of a resistor, capacitor or inductor in the
// netlist with the one in the parts files, within the tolerance there (in
// percent).
func (c *Component) checkValue() {

	if c.Element == nil || !strings.Contains("RCL", c.Element.Kind) {
		return
	}
	bom, ok := c.Number("value")
	net := c.Element.Value
	if !ok || bom == 0 || math.IsNaN(net) {
		return
	}
	tol := 1e-6
	if t, ok := c.Number("tolerance"); ok && t > 0 {
		tol = t / 100
	}
	if math.Abs(net-bom) > tol*math.Abs(bom) {
		c.warn("value %s in the netlist, %s in %s", c.Element.ValueText, c.Text("value"), c.Source("value"))
	}
}

// override replaces stress quantities with the parts fields v, i, p and t.
func (c *Component) override() {

	for _, o := range overrides {
		x, ok := c.Number(o.field)
		if !ok {
			continue
		}
		key := o.field
		if key == "v" {
			key = VoltageKey(c.Class)
		}
		if c.Stress == nil {
			c.Stress = stress.Stress{}
		}
		note := fmt.Sprintf("parts field %s from %s", o.field, c.Source(o.field))
		if old, had := c.Stress[key]; had {
			note += fmt.Sprintf(", instead of %g %s (%s)", old.Value, old.Unit, old.Source)
			c.warn("%s from the parts files overrides the simulated value; quantities derived from it are not recalculated", key)
		}
		c.Stress[key] = stress.Quantity{Value: x, Unit: o.unit, Source: stress.Metadata, Note: note}
	}
}

// derive adds the stress quantities that need metadata: the power of an
// inductor from its dc resistance.
func (c *Component) derive() {

	if c.Class != "L" {
		return
	}
	dcr, ok := c.Number("dcr")
	i, hasI := c.Stress["i"]
	if !ok || !hasI {
		return
	}
	if _, hasP := c.Stress["p"]; hasP {
		return
	}
	c.Stress["p"] = stress.Quantity{
		Value:  i.Value * i.Value * dcr,
		Unit:   "W",
		Source: stress.Derived,
		Note:   "i²·dcr, dcr from " + c.Source("dcr"),
	}
}

// KindName names a SPICE element kind: resistor, voltage source, …
func KindName(kind string) string {
	switch kind {
	case "R":
		return "resistor"
	case "C":
		return "capacitor"
	case "L":
		return "inductor"
	case "D":
		return "diode"
	case "Q":
		return "BJT"
	case "M":
		return "MOSFET"
	case "J":
		return "JFET"
	case "X":
		return "subcircuit"
	case "V":
		return "voltage source"
	case "I":
		return "current source"
	case "E", "F", "G", "H":
		return "controlled source"
	case "B":
		return "behavioural source"
	case "K":
		return "inductor coupling"
	case "S", "W":
		return "switch"
	case "T", "O", "U", "Y":
		return "transmission line"
	}
	return "element of type " + kind
}
