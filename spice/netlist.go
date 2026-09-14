package spice

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Circuit is what fitcalc needs to know about a netlist: its title, its
// top-level elements, and the models and subcircuits it defines or includes.
type Circuit struct {
	Title      string
	File       string
	Elements   []*Element
	Models     map[string]*Model  // by lower-case name
	Subckts    map[string]*Subckt // by lower-case name
	Directives []*Directive       // top-level dot cards, in order
	Includes   []string           // files read through .include and .lib, in order
	Warnings   []string           // "file:line: message"
}

// Element returns the element with the given name (case-insensitive), or
// nil.
func (c *Circuit) Element(name string) *Element {
	name = strings.ToUpper(name)
	for _, e := range c.Elements {
		if e.Name == name {
			return e
		}
	}
	return nil
}

// Model is a .model card.
type Model struct {
	Name string // lower case
	Type string // lower case: d, npn, pnp, nmos, pmos, njf, vdmos, …
	File string
	Line int
}

// Subckt is a .subckt definition.
type Subckt struct {
	Name  string   // lower case
	Ports []string // lower case
	File  string
	Line  int
}

// Directive is a top-level dot card, such as .op or .include.
type Directive struct {
	Name string // lower case, with the dot: ".op"
	Text string
	File string
	Line int
}

// line is a logical netlist line: continuation lines joined, comments
// removed.
type line struct {
	text string
	file string
	num  int // first physical line
}

type parser struct {
	c        *Circuit
	elements []line          // top-level element lines, parsed once all models are known
	active   map[string]bool // files being read, for include cycles
	seen     map[string]bool // files already in c.Includes
}

// Parse reads a netlist file and the files it includes.
func Parse(file string) (*Circuit, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseReader(f, file)
}

// ParseReader reads a netlist from r. name is used in messages and to
// resolve relative include paths, which are relative to the directory of
// the file that includes them.
//
// Only what element identity needs is parsed. Elements inside .subckt
// definitions, .control blocks and library sections are not elements of the
// circuit; .include files are followed, and files included with .lib are
// only searched for .model and .subckt names.
func ParseReader(r io.Reader, name string) (*Circuit, error) {

	p := &parser{
		c: &Circuit{
			File:    name,
			Models:  map[string]*Model{},
			Subckts: map[string]*Subckt{},
		},
		active: map[string]bool{},
		seen:   map[string]bool{},
	}
	if abs, err := filepath.Abs(name); err == nil {
		p.active[abs] = true
	}

	title, lines, err := readLines(r, name, true)
	if err != nil {
		return nil, err
	}
	p.c.Title = titleOf(title)

	if err := p.process(lines, true); err != nil {
		return nil, err
	}

	// Elements are parsed last: node counts can depend on model names
	// defined anywhere.
	first := map[string]*Element{}
	for _, l := range p.elements {
		e := p.element(l)
		if e == nil {
			continue
		}
		if f := first[e.Name]; f != nil {
			p.warn(l, "duplicate element %s (first at %s:%d)", e.Name, f.File, f.Line)
		} else {
			first[e.Name] = e
		}
		p.c.Elements = append(p.c.Elements, e)
	}

	return p.c, nil
}

// titleOf returns the title from the first netlist line, which may be a
// .title card (KiCad writes one).
func titleOf(s string) string {
	s = strings.TrimSpace(s)
	if f := strings.Fields(s); len(f) > 0 && strings.EqualFold(f[0], ".title") {
		return strings.TrimSpace(s[len(f[0]):])
	}
	return s
}

// process handles the logical lines of one file. top is true for the netlist
// and the files it includes at top level: only there are element lines
// elements of the circuit.
func (p *parser) process(lines []line, top bool) error {

	depth := 0   // .subckt nesting
	lib := false // in a library section: .lib name … .endl
	skip := ""   // card that ends a .control block

	for _, l := range lines {

		f := strings.Fields(l.text)
		card := strings.ToLower(f[0])
		outer := top && depth == 0 && !lib

		if skip != "" {
			if card == skip {
				skip = ""
				if outer {
					p.directive(card, l)
				}
			}
			continue
		}

		if card[0] != '.' {
			if outer {
				p.elements = append(p.elements, l)
			}
			continue
		}

		if outer {
			p.directive(card, l)
		}

		switch card {

		case ".end":
			if depth == 0 {
				return nil
			}

		case ".title":
			if outer {
				p.c.Title = strings.TrimSpace(l.text[len(f[0]):])
			}

		case ".subckt":
			depth++
			if len(f) < 2 {
				p.warn(l, ".subckt without a name")
				continue
			}
			name := strings.ToLower(f[1])
			if _, dup := p.c.Subckts[name]; !dup {
				s := &Subckt{Name: name, File: l.file, Line: l.num}
				for _, port := range f[2:] {
					if strings.Contains(port, "=") || strings.EqualFold(port, "params:") {
						break
					}
					s.Ports = append(s.Ports, strings.ToLower(port))
				}
				p.c.Subckts[name] = s
			}

		case ".ends":
			if depth == 0 {
				p.warn(l, ".ends without .subckt")
			} else {
				depth--
			}

		case ".control":
			skip = ".endc"

		case ".model":
			if len(f) < 3 {
				p.warn(l, ".model without a name or type")
				continue
			}
			name := strings.ToLower(f[1])
			typ, _, _ := strings.Cut(strings.ToLower(f[2]), "(")
			if _, dup := p.c.Models[name]; !dup {
				p.c.Models[name] = &Model{Name: name, Type: typ, File: l.file, Line: l.num}
			}

		case ".include", ".inc":
			args := tokenize(strings.TrimSpace(l.text[len(f[0]):]))
			if len(args) == 0 {
				p.warn(l, "%s without a file", card)
				continue
			}
			if err := p.include(l, args[0], outer); err != nil {
				return err
			}

		case ".lib":
			args := tokenize(strings.TrimSpace(l.text[len(f[0]):]))
			switch len(args) {
			case 0:
				p.warn(l, ".lib without arguments")
			case 1:
				// Start of a library section: its elements are not part
				// of the circuit, its models are
				lib = true
			default:
				// A section of a library file: models only
				if err := p.include(l, args[0], false); err != nil {
					return err
				}
			}

		case ".endl":
			lib = false
		}
	}

	if depth > 0 {
		p.warn(lines[len(lines)-1], "missing .ends")
	}
	if lib {
		p.warn(lines[len(lines)-1], "missing .endl")
	}
	if skip != "" {
		p.warn(lines[len(lines)-1], "missing %s", skip)
	}
	return nil
}

func (p *parser) directive(card string, l line) {
	p.c.Directives = append(p.c.Directives, &Directive{Name: card, Text: l.text, File: l.file, Line: l.num})
}

// include reads a file named in an .include or .lib card.
func (p *parser) include(l line, arg string, top bool) error {

	path := resolvePath(l.file, arg)

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if p.active[abs] {
		return fmt.Errorf("%s:%d: include cycle: %s", l.file, l.num, path)
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%s:%d: %w", l.file, l.num, err)
	}
	defer f.Close()

	_, lines, err := readLines(f, path, false)
	if err != nil {
		return err
	}
	if !p.seen[abs] {
		p.seen[abs] = true
		p.c.Includes = append(p.c.Includes, path)
	}

	p.active[abs] = true
	defer delete(p.active, abs)

	if len(lines) == 0 {
		return nil
	}
	return p.process(lines, top)
}

// resolvePath returns the path of a file named in an .include or .lib card
// of the file including: unquoted, with ~ expanded, and relative to the
// directory of the including file, as ngspice resolves it.
func resolvePath(including, arg string) string {

	if len(arg) >= 2 && (arg[0] == '"' || arg[0] == '\'') && arg[len(arg)-1] == arg[0] {
		arg = arg[1 : len(arg)-1]
	}
	if arg == "~" || strings.HasPrefix(arg, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			arg = filepath.Join(home, arg[1:])
		}
	}
	if !filepath.IsAbs(arg) {
		arg = filepath.Join(filepath.Dir(including), arg)
	}
	return arg
}

func (p *parser) warn(l line, format string, a ...any) {
	p.c.Warnings = append(p.c.Warnings, fmt.Sprintf("%s:%d: %s", l.file, l.num, fmt.Sprintf(format, a...)))
}

// readLines returns the logical lines of a netlist file. If hasTitle is set,
// the first line is the title and is returned separately.
func readLines(r io.Reader, file string, hasTitle bool) (string, []line, error) {

	var title string
	var lines []line

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)

	n := 0
	for sc.Scan() {
		n++
		s := sc.Text()
		if n == 1 {
			s = strings.TrimPrefix(s, "\ufeff")
			if hasTitle {
				title = s
				continue
			}
		}

		s = strings.TrimSpace(stripComment(s))
		if s == "" {
			continue
		}

		if s[0] == '+' {
			if len(lines) == 0 {
				return "", nil, fmt.Errorf("%s:%d: continuation line without a preceding line", file, n)
			}
			last := &lines[len(lines)-1]
			last.text += " " + strings.TrimSpace(s[1:])
			continue
		}

		lines = append(lines, line{text: s, file: file, num: n})
	}
	if err := sc.Err(); err != nil {
		return "", nil, fmt.Errorf("%s: %w", file, err)
	}

	// A continuation may have left an empty first field
	for i := range lines {
		lines[i].text = strings.TrimSpace(lines[i].text)
	}
	return title, lines, nil
}

// stripComment removes comments: whole-line comments starting with *, and
// inline comments starting with ;, or with $ or // preceded by white space
// (ngspice syntax).
func stripComment(s string) string {

	if strings.HasPrefix(strings.TrimLeft(s, " \t"), "*") {
		return ""
	}

	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case inQuote:
		case c == ';':
			return s[:i]
		case c == '$' && (i == 0 || isSpace(s[i-1])) && (i+1 == len(s) || isSpace(s[i+1])):
			return s[:i]
		case c == '/' && i+1 < len(s) && s[i+1] == '/' && (i == 0 || isSpace(s[i-1])):
			return s[:i]
		}
	}
	return s
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t'
}
