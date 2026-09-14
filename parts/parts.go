package parts

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/rveen/golib/csv"
)

// Parts is the component metadata read from the parts CSV files.
type Parts struct {
	Files    []string
	Format   string                          // format of the BOM: FormatFides or FormatKiCad
	Items    map[string]map[string]csv.Field // by upper-case reference; lower-case field names
	DNP      []string                        // references marked do-not-populate in the BOM, in natural order
	Notes    map[string][]string             // by reference: how its BOM row was read
	Warnings []string
}

// Load reads the parts CSV files. The first file is the BOM: a fides CSV file
// with one row per component, its reference in the name field, or a KiCad
// BOM export (see readBOM). The others define types in the fides format,
// which BOM rows and other types inherit through their type field
// (github.com/rveen/golib/csv, ReadTypedFields). Field names are lower case,
// as in the fides files.
//
// A BOM row without a type gets the type that lists its part number (mpn)
// in its mpns field, or whose name is its part number, or that lists a
// pattern matching its footprint in its footprints field.
func Load(files []string) (*Parts, error) {

	b, err := readBOM(files[0])
	if err != nil {
		return nil, err
	}
	types, err := readTypes(files[1:])
	if err != nil {
		return nil, err
	}

	p := &Parts{Files: files, Format: b.format, Items: map[string]map[string]csv.Field{}, DNP: b.dnp, Notes: b.notes}
	p.Warnings = append(b.warnings, types.warnings...)

	unknown := map[string][]string{} // part numbers without a type, with their references
	for _, r := range b.rows {
		f := r.fields
		if f["type"] != "" {
			continue
		}
		name, how := types.match(f["mpn"], f["footprint"])
		switch {
		case name != "":
			f["type"] = name
			up := strings.ToUpper(r.ref)
			p.Notes[up] = append(p.Notes[up], fmt.Sprintf("type %s from the %s", name, how))
		case f["mpn"] != "":
			unknown[f["mpn"]] = append(unknown[f["mpn"]], r.ref)
		}
	}
	if len(unknown) > 0 {
		var s []string
		for _, pn := range slices.Sorted(maps.Keys(unknown)) {
			s = append(s, pn+" ("+strings.Join(unknown[pn], ", ")+")")
		}
		p.Warnings = append(p.Warnings, "parts: part numbers without a type in the parts files: "+strings.Join(s, ", "))
	}

	tmp, err := writeBOM(b.rows)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp)

	t, err := csv.ReadTypedFields(append([]string{tmp}, files[1:]...))
	if err != nil {
		return nil, err
	}
	for _, w := range t.Warnings {
		p.Warnings = append(p.Warnings, "parts: "+w)
	}

	for name, fields := range t.Items {
		out := map[string]csv.Field{}
		for k, f := range fields {
			f.Sources = slices.Clone(f.Sources)
			for i := range f.Sources {
				if f.Sources[i].File == tmp {
					f.Sources[i].File = files[0]
				}
			}
			out[strings.ToLower(k)] = f
		}
		p.Items[strings.ToUpper(strings.TrimSpace(name))] = out
	}

	if len(p.Items) == 0 {
		return nil, fmt.Errorf("%s: no parts (the BOM needs a name or Reference column with the references)", files[0])
	}
	return p, nil
}

// typeIndex finds the type of a component from its part number or footprint.
type typeIndex struct {
	names      map[string]string // type names by upper-case name
	mpns       map[string]string // type names by upper-case part number
	footprints []footprintType   // in the order of the files
	warnings   []string
}

type footprintType struct{ pattern, name string }

// readTypes reads the type names of the type files, with the part numbers
// (mpns) and footprint patterns (footprints) they list.
func readTypes(files []string) (*typeIndex, error) {
	x := &typeIndex{names: map[string]string{}, mpns: map[string]string{}}
	for _, file := range files {
		rows, err := csv.Read(file)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			row := map[string]string{}
			for k, v := range r {
				row[fieldName(k)] = v
			}
			name := row["name"]
			if name == "" {
				continue
			}
			if _, ok := x.names[strings.ToUpper(name)]; !ok {
				x.names[strings.ToUpper(name)] = name
			}
			for _, pn := range listField(row["mpns"]) {
				if prev, ok := x.mpns[strings.ToUpper(pn)]; ok && prev != name {
					x.warnings = append(x.warnings, fmt.Sprintf("parts: part number %s is listed by the types %s and %s; %s is used", pn, prev, name, prev))
					continue
				}
				x.mpns[strings.ToUpper(pn)] = name
			}
			for _, fp := range listField(row["footprints"]) {
				x.footprints = append(x.footprints, footprintType{fp, name})
			}
		}
	}
	return x, nil
}

// match returns the type of a part number or footprint, and how it was found.
func (x *typeIndex) match(mpn, footprint string) (name, how string) {
	if mpn != "" {
		if n, ok := x.mpns[strings.ToUpper(mpn)]; ok {
			return n, "part number " + mpn
		}
		if n, ok := x.names[strings.ToUpper(mpn)]; ok {
			return n, "part number " + mpn
		}
	}
	if footprint != "" {
		for _, f := range x.footprints {
			if footprintMatch(f.pattern, footprint) {
				return f.name, "footprint " + footprint
			}
		}
	}
	return "", ""
}

// listField splits a field that lists values, separated by spaces or
// semicolons.
func listField(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ';' || unicode.IsSpace(r) })
}

// Refs returns the references in natural order: C1, R2, R10.
func (p *Parts) Refs() []string {
	refs := make([]string, 0, len(p.Items))
	for ref := range p.Items {
		refs = append(refs, ref)
	}
	SortRefs(refs)
	return refs
}

// SortRefs sorts references in natural order: by their letters, then by
// their number.
func SortRefs(refs []string) {
	sort.Slice(refs, func(i, j int) bool { return refLess(refs[i], refs[j]) })
}

func refLess(a, b string) bool {
	pa, na, ra := splitRef(a)
	pb, nb, rb := splitRef(b)
	switch {
	case pa != pb:
		return pa < pb
	case na != nb:
		return na < nb
	}
	return ra < rb
}

// splitRef splits R10A into R, 10 and A.
func splitRef(s string) (string, int, string) {
	i := strings.IndexFunc(s, unicode.IsDigit)
	if i < 0 {
		return s, -1, ""
	}
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	n, _ := strconv.Atoi(s[i:j])
	return s[:i], n, s[j:]
}
