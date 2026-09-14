package spice

import (
	"math"
	"strings"

	"github.com/rveen/electronics"
)

// Element is one element (instance) line of the netlist.
type Element struct {
	Name      string            // upper case, e.g. R1, XU1
	Kind      string            // the element letter, upper case: "R", "X", …
	Nodes     []string          // lower case, as ngspice names them
	Value     float64           // SI units; NaN if there is none or it is an expression
	ValueText string            // the value as written: "10k", "{rload}", "dc 5"
	Model     string            // lower-case model or subcircuit name
	Params    map[string]string // key=value parameters; keys in lower case
	File      string
	Line      int    // first physical line
	Text      string // the logical line: continuations joined, comments removed
}

// nodeCount is the number of nodes of the element kinds with a fixed count.
// Q, M, and X, N, A (nodes followed by a model name) are handled apart.
var nodeCount = map[string]int{
	"R": 2, "C": 2, "L": 2, "D": 2,
	"J": 3, "Z": 3, "U": 3,
	"S": 4, "T": 4, "O": 4, "Y": 4,
	"V": 2, "I": 2, "E": 2, "F": 2, "G": 2, "H": 2, "B": 2, "W": 2,
}

// valueParams are the parameters that can hold the value of R, C and L.
var valueParams = map[string][]string{
	"R": {"r", "resistance"},
	"C": {"c", "capacitance"},
	"L": {"l", "inductance"},
}

// element parses an element line. For controlled sources, only the output
// nodes are kept.
func (p *parser) element(l line) *Element {

	toks := tokenize(l.text)
	name := strings.ToUpper(toks[0])
	if c := name[0]; c < 'A' || c > 'Z' {
		p.warn(l, "not an element: %s", toks[0])
		return nil
	}

	e := &Element{
		Name:   name,
		Kind:   name[:1],
		Value:  math.NaN(),
		Params: map[string]string{},
		File:   l.file,
		Line:   l.num,
		Text:   l.text,
	}

	var pos []string // positional tokens
	for _, t := range toks[1:] {
		if k, v, ok := param(t); ok {
			e.Params[k] = v
		} else if !strings.EqualFold(t, "params:") {
			pos = append(pos, t)
		}
	}

	switch e.Kind {

	case "R", "C", "L":
		for _, t := range p.nodes(e, pos, 2, l) {
			if e.ValueText == "" {
				if v, err := electronics.SpiceValue(t); err == nil {
					e.Value, e.ValueText = v, t
					continue
				}
				if isExpr(t) {
					e.ValueText = t
					continue
				}
			}
			if e.Model == "" {
				e.Model = strings.ToLower(t)
			}
		}
		if e.ValueText == "" {
			for _, k := range valueParams[e.Kind] {
				if v, ok := e.Params[k]; ok {
					e.ValueText = v
					e.Value, _ = electronics.SpiceValue(v)
					break
				}
			}
		}

	case "Q":
		// A fourth node (substrate) is there if the fourth token is not a
		// model and the fifth one is a model, or not an area (a number)
		n := 3
		if len(pos) > 4 && !p.isModel(pos[3]) &&
			(p.isModel(pos[4]) || !isNumber(pos[4]) && !strings.EqualFold(pos[4], "off")) {
			n = 4
		}
		if rest := p.nodes(e, pos, n, l); len(rest) > 0 {
			e.Model = strings.ToLower(rest[0])
		}

	case "M":
		// ngspice VDMOS transistors have three nodes
		n := 4
		if len(pos) > 3 && p.isModel(pos[3]) {
			n = 3
		}
		if rest := p.nodes(e, pos, n, l); len(rest) > 0 {
			e.Model = strings.ToLower(rest[0])
		}

	case "X", "N", "A":
		if len(pos) == 0 {
			p.warn(l, "%s: no subcircuit or model name", e.Name)
			break
		}
		for _, n := range pos[:len(pos)-1] {
			e.Nodes = append(e.Nodes, strings.ToLower(n))
		}
		e.Model = strings.ToLower(pos[len(pos)-1])

	case "V", "I":
		rest := p.nodes(e, pos, 2, l)
		e.ValueText = strings.Join(rest, " ")
		if len(rest) > 0 {
			t := rest[0]
			if strings.EqualFold(t, "dc") && len(rest) > 1 {
				t = rest[1]
			}
			if v, err := electronics.SpiceValue(t); err == nil {
				e.Value = v
			}
		} else if v, ok := e.Params["dc"]; ok {
			e.ValueText = v
			e.Value, _ = electronics.SpiceValue(v)
		}

	case "E", "F", "G", "H", "B":
		e.ValueText = strings.Join(p.nodes(e, pos, 2, l), " ")

	case "K":
		// Coupled inductors: no nodes
		e.ValueText = strings.Join(pos, " ")

	case "W":
		// n+ n- vcontrol model
		if rest := p.nodes(e, pos, 2, l); len(rest) > 1 {
			e.Model = strings.ToLower(rest[1])
		}

	default:
		n, ok := nodeCount[e.Kind]
		if !ok {
			p.warn(l, "%s: unknown element type %s", e.Name, e.Kind)
			n = 2
		}
		if rest := p.nodes(e, pos, n, l); len(rest) > 0 && e.Kind != "T" {
			e.Model = strings.ToLower(rest[0])
		}
	}

	return e
}

// nodes takes the first n positional tokens as nodes and returns the rest.
func (p *parser) nodes(e *Element, pos []string, n int, l line) []string {
	if len(pos) < n {
		p.warn(l, "%s: %d nodes expected, found %d", e.Name, n, len(pos))
		n = len(pos)
	}
	for _, s := range pos[:n] {
		e.Nodes = append(e.Nodes, strings.ToLower(s))
	}
	return pos[n:]
}

func (p *parser) isModel(tok string) bool {
	_, ok := p.c.Models[strings.ToLower(tok)]
	return ok
}

func isNumber(tok string) bool {
	_, err := electronics.SpiceValue(tok)
	return err == nil
}

// isExpr reports whether a token is a parameter expression: {…} or '…'.
func isExpr(tok string) bool {
	return tok != "" && (tok[0] == '{' || tok[0] == '\'' || tok[0] == '"')
}

// param splits a key=value token. Expressions such as {a=b} are not
// parameters.
func param(tok string) (string, string, bool) {
	i := strings.IndexByte(tok, '=')
	if i <= 0 || strings.ContainsRune("{('\"[", rune(tok[0])) {
		return "", "", false
	}
	return strings.ToLower(tok[:i]), tok[i+1:], true
}

// tokenize splits a line at white space. Parentheses, braces, brackets and
// quotes group their content into one token, and "a = b" becomes "a=b".
func tokenize(s string) []string {

	var toks []string
	var cur strings.Builder
	depth := 0
	var quote byte

	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
			cur.WriteByte(c)
		case c == '(' || c == '{' || c == '[':
			depth++
			cur.WriteByte(c)
		case (c == ')' || c == '}' || c == ']') && depth > 0:
			depth--
			cur.WriteByte(c)
		case isSpace(c) && depth == 0:
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()

	// Join "a = b", "a= b" and "a =b"
	var out []string
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		for i+1 < len(toks) && (strings.HasSuffix(t, "=") || strings.HasPrefix(toks[i+1], "=")) {
			t += toks[i+1]
			i++
		}
		out = append(out, t)
	}
	return out
}
