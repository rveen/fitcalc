package parts

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/rveen/golib/csv"
)

// Footprint is what the name of a KiCad footprint tells about a part.
type Footprint struct {
	Package string   // FIDES package name: 0603, SOT23, SOD123, SOIC8, QFN32, DPAK, …
	Pins    int      // number of contacts of a connector: 2 for PinHeader_1x02
	Tags    []string // technology tags: cer, alu, tant, thick, melf, network, pot, led, tht
}

var (
	chipRE  = regexp.MustCompile(`^(?:R|C|L|LED|D|Fuse)_(\d{4})_\d{4}Metric`)
	sotRE   = regexp.MustCompile(`SOT-(\d+)(?:-(\d+))?`)
	sodRE   = regexp.MustCompile(`SOD-(\d+)`)
	smxRE   = regexp.MustCompile(`^D_SM([ABC])(?:_|$)`)
	doRE    = regexp.MustCompile(`DO-(15|35|41)(?:_|$)`)
	toRE    = regexp.MustCompile(`TO-(\d+)`)
	icRE    = regexp.MustCompile(`(?:^|_)(HTSSOP|TSSOP|MSOP|VSSOP|SSOP|QSOP|TSOP|SOIC|SO|LQFP|TQFP|PQFP|[A-Z]*QFN|[A-Z]*DFN|PLCC|SOJ|LGA|DIP|BGA)-(\d+)`)
	pinsRE  = regexp.MustCompile(`_(\d{1,2})x(\d{1,2})(?:_|$)`)
	thtRE   = regexp.MustCompile(`Axial|Radial|DIP-|TO-(?:92|220|247|126|218|3)\b|PinHeader|PinSocket|TerminalBlock|THT|C_Disc|D_DO-`)
	cerRE   = regexp.MustCompile(`^C_(?:\d{4}_|Disc)`)
	aluRE   = regexp.MustCompile(`^(?:CP_Elec|CP_Radial|CP_Axial|C_Elec)`)
	tantRE  = regexp.MustCompile(`^CP_(?:EIA|Tantalum)`)
	thickRE = regexp.MustCompile(`^R_\d{4}_`)
	melfRE  = regexp.MustCompile(`^R_(?:Mini)?MELF`)
	netRE   = regexp.MustCompile(`^R_(?:Array|Pack)`)
)

// toPackages are the FIDES names of TO packages that have another name.
var toPackages = map[int]string{252: "DPAK", 263: "D2PAK", 268: "D3PAK", 251: "TO251AA"}

// ParseFootprint reads the package, the number of contacts and the
// technology of a part from its KiCad footprint, library:name, as the
// footprints of KiCad's libraries are named.
func ParseFootprint(fp string) Footprint {

	lib, name := "", fp
	if i := strings.LastIndex(fp, ":"); i >= 0 {
		lib, name = fp[:i], fp[i+1:]
	}
	var f Footprint

	switch {
	case chipRE.MatchString(name):
		f.Package = chipRE.FindStringSubmatch(name)[1]
	case strings.Contains(name, "PowerPAK_SO-8"):
		f.Package = "SO8P"
	case sotRE.MatchString(name):
		m := sotRE.FindStringSubmatch(name)
		f.Package = "SOT" + m[1]
		if m[1] == "23" && (m[2] == "5" || m[2] == "6") {
			f.Package += "-" + m[2]
		}
	case sodRE.MatchString(name):
		f.Package = "SOD" + sodRE.FindStringSubmatch(name)[1]
	case smxRE.MatchString(name):
		f.Package = "SM" + smxRE.FindStringSubmatch(name)[1]
	case strings.HasPrefix(name, "D_MiniMELF"):
		f.Package = "SOD80"
	case strings.HasPrefix(name, "D_MELF"):
		f.Package = "SOD87"
	case doRE.MatchString(name):
		f.Package = "DO" + doRE.FindStringSubmatch(name)[1]
	case toRE.MatchString(name):
		n, _ := strconv.Atoi(toRE.FindStringSubmatch(name)[1])
		f.Package = toPackages[n]
		if f.Package == "" {
			f.Package = "TO" + strconv.Itoa(n)
		}
	case icRE.MatchString(name):
		m := icRE.FindStringSubmatch(name)
		family := m[1]
		switch {
		case family == "DIP":
			family = "PDIP"
		case family == "BGA":
			family = "PBGA"
		case strings.HasSuffix(family, "QFN"):
			family = "QFN"
		case strings.HasSuffix(family, "DFN"):
			family = "DFN"
		}
		f.Package = family + m[2]
	}

	if m := pinsRE.FindStringSubmatch(name); m != nil {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		f.Pins = a * b
	}

	switch {
	case cerRE.MatchString(name):
		f.Tags = append(f.Tags, "cer")
	case aluRE.MatchString(name):
		f.Tags = append(f.Tags, "alu")
	case tantRE.MatchString(name) || strings.Contains(lib, "Tantalum"):
		f.Tags = append(f.Tags, "tant")
	case thickRE.MatchString(name):
		f.Tags = append(f.Tags, "thick")
	case melfRE.MatchString(name):
		f.Tags = append(f.Tags, "melf")
	case netRE.MatchString(name):
		f.Tags = append(f.Tags, "network")
	case strings.HasPrefix(lib, "Potentiometer"):
		f.Tags = append(f.Tags, "pot")
	case strings.HasPrefix(name, "LED_") || strings.HasPrefix(lib, "LED"):
		f.Tags = append(f.Tags, "led")
	}
	if thtRE.MatchString(fp) && !strings.Contains(name, "SMD") {
		f.Tags = append(f.Tags, "tht")
	}
	return f
}

// techTags are the tags of a class that name its technology. A tag that
// the footprint implies is added only if the component has none of them.
var techTags = map[string][]string{
	"C": {"cer", "alu", "elco", "tant", "tantalium", "film"},
	"R": {"thick", "thin", "melf", "ww", "network", "pot", "potmeter", "potentiometer", "variable"},
	"D": {"led", "zener", "tvs", "rectifier"},
}

// tagGroup returns the tags that exclude a tag the footprint implies, and the
// class it applies to ("" for any).
func tagGroup(tag string) ([]string, string) {
	switch tag {
	case "tht":
		return []string{"tht", "smd"}, ""
	case "led":
		return techTags["D"], "D"
	case "cer", "alu", "tant":
		return techTags["C"], "C"
	}
	return techTags["R"], "R"
}

// fromFootprint fills in, from the KiCad footprint, what the parts files do
// not give: the package, the number of contacts of a connector, and the
// technology tags of its class.
func (c *Component) fromFootprint() {

	fp := c.Text("footprint")
	if fp == "" {
		return
	}
	f := ParseFootprint(fp)
	src := c.Meta["footprint"].Sources

	if f.Package != "" && c.Text("package") == "" {
		c.Meta["package"] = csv.Field{Value: f.Package, Sources: src}
		c.note("package %s from the footprint %s", f.Package, fp)
	}
	if f.Pins > 0 && c.Class == "J" && c.Text("npins") == "" {
		c.Meta["npins"] = csv.Field{Value: strconv.Itoa(f.Pins), Sources: src}
		c.note("npins %d from the footprint %s", f.Pins, fp)
	}
	for _, t := range f.Tags {
		group, class := tagGroup(t)
		if class != "" && class != c.Class || slices.ContainsFunc(c.Tags, func(x string) bool { return slices.Contains(group, x) }) {
			continue
		}
		c.Tags = append(c.Tags, t)
		c.note("tag %s from the footprint %s", t, fp)
	}
}

// footprintMatch reports whether a KiCad footprint, library:name, matches a
// pattern with the wildcards * and ?. A pattern without a library matches
// the name alone.
func footprintMatch(pattern, fp string) bool {
	if !strings.Contains(pattern, ":") {
		if i := strings.LastIndex(fp, ":"); i >= 0 {
			fp = fp[i+1:]
		}
	}
	ok, _ := path.Match(pattern, fp)
	return ok
}

// prefixClass is the class of a component, and the tag it implies, from the
// letters of its reference, for components that neither the parts files nor
// the netlist give a class.
var prefixClass = map[string]struct{ class, tag string }{
	"R": {"R", ""}, "C": {"C", ""}, "L": {"L", ""}, "D": {"D", ""}, "LED": {"D", "led"},
	"Q": {"Q", ""}, "U": {"U", ""}, "IC": {"U", ""}, "J": {"J", ""}, "P": {"J", ""}, "CN": {"J", ""},
	"Y": {"X", ""},
}
