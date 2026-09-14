package spice

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rveen/electronics"
)

// outputCards are the analysis and output cards that the deck comments out:
// it runs its own operating point analysis and saves its own vectors.
var outputCards = map[string]bool{
	".op": true, ".tran": true, ".ac": true, ".dc": true, ".noise": true, ".tf": true,
	".disto": true, ".pz": true, ".sens": true, ".sp": true, ".pss": true,
	".four": true, ".fourier": true, ".print": true, ".plot": true, ".save": true,
	".probe": true, ".meas": true, ".measure": true,
}

// commentPrefix marks the netlist lines that the deck comments out.
const commentPrefix = "* fitcalc: "

// savesPerLine is the number of vectors per save command in the deck.
const savesPerLine = 8

// DeckOptions are what Deck adds to the netlist.
type DeckOptions struct {
	// Vectors are the device vectors to save, besides all node voltages and
	// branch currents.
	Vectors []string

	// Raw is the name of the binary raw file to write.
	Raw string

	// Temp is the simulation temperature in °C, set with option temp in the
	// control block, which overrides a .temp card of the netlist. nil keeps
	// the temperature of the netlist, or ngspice's default.
	Temp *float64

	// Pins are the subcircuit instances whose pin currents are measured.
	// Those in the netlist file are commented out and written again after
	// the netlist, each pin with a zero-volt source in series, whose current
	// is PinCurrent.
	Pins []*Element

	// Analysis is the analysis to run instead of the operating point: a DC
	// sweep or a transient analysis; nil for the operating point.
	Analysis Analysis

	// Dtemp are the temperatures of elements above the simulation
	// temperature, in K (Heatable elements). Those in the netlist file are
	// commented out and written again after the netlist with dtemp.
	Dtemp map[*Element]float64

	// Values are other values of elements with a numeric value, such as
	// those of a Monte Carlo run. Those in the netlist file are commented out
	// and written again after the netlist with the value (withValue).
	Values map[*Element]float64
}

// withValue returns the card of e with its value replaced by v: the first
// token after the nodes, or the value of a key=value parameter, whose SPICE
// value is e's. ok is false if there is none.
func withValue(e *Element, v float64) (string, bool) {
	if math.IsNaN(e.Value) {
		return "", false
	}
	toks := tokenize(e.Text)
	for i := 1 + len(e.Nodes); i < len(toks); i++ {
		key, val, param := strings.Cut(toks[i], "=")
		if !param {
			val = toks[i]
		}
		x, err := electronics.SpiceValue(val)
		if err != nil || math.Abs(x-e.Value) > 1e-12*math.Abs(e.Value) {
			continue
		}
		toks[i] = strconv.FormatFloat(v, 'g', 9, 64)
		if param {
			toks[i] = key + "=" + toks[i]
		}
		return strings.Join(toks, " "), true
	}
	return "", false
}

// Heatable reports whether ngspice takes a temperature (dtemp) for element
// e: R, C, L, D, Q, M and J, verified with ngspice 43.
func Heatable(e *Element) bool {
	return strings.Contains("RCLDQMJ", e.Kind) && len(e.Kind) == 1
}

// Deck returns the ngspice deck for an operating point analysis, or o's
// Analysis, of the netlist src, read from file, and notes on what it changed.
//
// The deck is the netlist, line by line, with its analysis and output cards
// and .control blocks commented out (so ngspice's line numbers still match
// the netlist), include paths made absolute, and nothing after .end. It ends
// with a .control block that saves all node voltages and branch currents
// plus the device vectors of o, runs the analysis and writes the raw file.
func Deck(src []byte, file string, o DeckOptions) ([]byte, []string) {

	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}

	rewrite := map[int][]string{} // by line number: the lines that replace an instance
	for _, e := range o.Pins {
		if e.File == file {
			if r := ammeters(e); r != nil {
				rewrite[e.Line] = r
			}
		}
	}

	// Elements written again, with a varied value or at their temperature
	heated := map[int][]string{} // by line number
	var notes []string
	elems := slices.Collect(maps.Keys(o.Dtemp))
	for e := range o.Values {
		if _, ok := o.Dtemp[e]; !ok {
			elems = append(elems, e)
		}
	}
	slices.SortFunc(elems, func(a, b *Element) int { return cmp.Compare(a.Name, b.Name) })
	for _, e := range elems {
		v, hasValue := o.Values[e]
		dt, hasTemp := o.Dtemp[e]
		hasTemp = hasTemp && Heatable(e)
		switch {
		case !hasValue && !hasTemp || rewrite[e.Line] != nil:
			continue
		case e.File != file:
			notes = append(notes, fmt.Sprintf("%s:%d: %s is in an included file: fitcalc does not change its value or temperature", e.File, e.Line, e.Name))
			continue
		}
		text := e.Text
		if hasValue {
			if t, ok := withValue(e, v); ok {
				text = t
			} else {
				notes = append(notes, fmt.Sprintf("%s:%d: %s: fitcalc cannot change its value %s", e.File, e.Line, e.Name, e.ValueText))
			}
		}
		if hasTemp {
			if e.Params["temp"] != "" || e.Params["dtemp"] != "" {
				notes = append(notes, fmt.Sprintf("%s:%d: %s sets its own temperature: the thermal network does not change it", e.File, e.Line, e.Name))
			} else {
				text += fmt.Sprintf(" dtemp=%.6g", dt)
			}
		}
		if text != e.Text {
			heated[e.Line] = []string{text}
		}
	}

	var out, commented, instances, warm []string
	inControl := false  // in a .control block
	commenting := false // the current card is commented out, and so are its continuation lines

loop:
	for i, s := range lines {

		if i == 0 {
			out = append(out, s) // title
			continue
		}

		t := strings.TrimSpace(s)
		card := ""
		if f := strings.Fields(t); len(f) > 0 {
			card = strings.ToLower(f[0])
		}

		switch {

		case inControl:
			out = append(out, commentPrefix+s)
			if card == ".endc" {
				inControl = false
			}

		case strings.HasPrefix(t, "+"):
			if commenting {
				s = commentPrefix + s
			}
			out = append(out, s)

		case t == "" || t[0] == '*':
			out = append(out, s)

		default:
			commenting = false
			if r, ok := rewrite[i+1]; ok {
				commenting = true
				out = append(out, commentPrefix+s)
				instances = append(instances, r...)
				continue
			}
			if r, ok := heated[i+1]; ok {
				commenting = true
				out = append(out, commentPrefix+s)
				warm = append(warm, r...)
				continue
			}

			switch {
			case card == ".end":
				break loop
			case card == ".control":
				inControl = true
				out = append(out, commentPrefix+s)
				notes = append(notes, fmt.Sprintf("%s:%d: .control block removed", file, i+1))
			case outputCards[card]:
				commenting = true
				out = append(out, commentPrefix+s)
				commented = append(commented, fmt.Sprintf("%s (line %d)", card, i+1))
			case card == ".include" || card == ".inc" || card == ".lib":
				out = append(out, absInclude(s, file))
			default:
				out = append(out, s)
			}
		}
	}

	if len(commented) > 0 {
		notes = append(notes, fmt.Sprintf("%s: analysis and output cards commented out: %s",
			file, strings.Join(commented, ", ")))
	}

	if len(instances) > 0 {
		out = append(out, "* fitcalc: subcircuit instances, with their pin currents measured")
		out = append(out, instances...)
	}
	if len(warm) > 0 {
		out = append(out, "* fitcalc: elements with a varied value or at their temperature")
		out = append(out, warm...)
	}

	analysis, title := "op", "operating point analysis"
	if o.Analysis != nil {
		analysis, title = o.Analysis.command(), o.Analysis.title()
	}
	out = append(out, "* fitcalc: "+title, ".control", "set filetype=binary", "save all")
	for i := 0; i < len(o.Vectors); i += savesPerLine {
		out = append(out, "save "+strings.Join(o.Vectors[i:min(i+savesPerLine, len(o.Vectors))], " "))
	}
	if o.Temp != nil {
		out = append(out, fmt.Sprintf("option temp=%g", *o.Temp))
	}
	out = append(out, analysis, "write "+o.Raw, "quit", ".endc", ".end")

	return []byte(strings.Join(out, "\n") + "\n"), notes
}

// ammeters returns the lines that measure the pin currents of subcircuit
// instance e: its line with each node replaced by an internal node, and a
// zero-volt source from each node to that internal node. It returns nil if
// the line does not start with the nodes.
func ammeters(e *Element) []string {
	toks := tokenize(e.Text)
	if len(e.Nodes) == 0 || len(toks) < len(e.Nodes)+2 {
		return nil
	}
	inst := []string{toks[0]}
	var sources []string
	for k, n := range e.Nodes {
		tok := toks[1+k]
		if strings.ToLower(tok) != n {
			return nil
		}
		pin := pinNode(e.Name, k)
		inst = append(inst, pin)
		sources = append(sources, "v"+pin+" "+tok+" "+pin+" 0")
	}
	inst = append(inst, toks[1+len(e.Nodes):]...)
	return append([]string{strings.Join(inst, " ")}, sources...)
}

// pinNode is the internal node of pin k (from 0) of subcircuit instance name.
func pinNode(name string, k int) string {
	return fmt.Sprintf("fitcalc_%s_%d", strings.ToLower(name), k+1)
}

// PinCurrent is the vector of the current into pin k (from 0) of subcircuit
// instance name, measured by the deck.
func PinCurrent(name string, k int) string {
	return "i(v" + pinNode(name, k) + ")"
}

// absInclude rewrites an .include or .lib card of file with an absolute,
// quoted path. A .lib card that starts a library section has no path and is
// returned as is.
func absInclude(s, file string) string {

	t := strings.TrimSpace(stripComment(s))
	keyword := strings.Fields(t)[0]
	args := tokenize(strings.TrimSpace(t[len(keyword):]))

	isLib := strings.EqualFold(keyword, ".lib")
	if len(args) == 0 || isLib && len(args) < 2 {
		return s
	}

	path := resolvePath(file, args[0])
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}

	card := keyword + ` "` + path + `"`
	if isLib {
		card += " " + args[1]
	}
	return card
}
