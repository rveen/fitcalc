package parts

import (
	"bytes"
	stdcsv "encoding/csv"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/rveen/golib/csv"
)

// BOM formats
const (
	FormatFides = "fides" // fides CSV: one row per reference, in the name field
	FormatKiCad = "KiCad" // KiCad BOM export: grouped references in a Reference column
)

// bom is the first parts file: its components, one row per reference.
type bom struct {
	format   string
	rows     []bomRow
	dnp      []string            // upper-case references marked do-not-populate
	notes    map[string][]string // by upper-case reference
	warnings []string
}

// bomRow is a component of the BOM.
type bomRow struct {
	ref    string            // one reference, as written
	fields map[string]string // by field name, name included
}

func (b *bom) warn(format string, a ...any) {
	b.warnings = append(b.warnings, "parts: "+fmt.Sprintf(format, a...))
}

// columnAliases are the column names, normalized by fieldName, that KiCad and
// other BOM tools use for a field of fitcalc.
var columnAliases = map[string]string{
	"reference": "reference", "references": "reference", "ref": "reference", "refs": "reference",
	"designator": "reference", "designators": "reference", "ref des": "reference", "refdes": "reference",
	"reference designator": "reference", "reference designators": "reference",

	"qty": "qty", "quantity": "qty", "qnty": "qty",

	"dnp": "dnp", "do not populate": "dnp", "do not place": "dnp", "not populated": "dnp",

	"mpn": "mpn", "manufacturer part number": "mpn", "mfr part number": "mpn", "mfr part #": "mpn",
	"mfr pn": "mpn", "mfg part number": "mpn", "mfg pn": "mpn", "manufacturer pn": "mpn",
	"part number": "mpn", "partnumber": "mpn", "pn": "mpn",

	"manufacturer": "manufacturer", "mfr": "manufacturer", "mfg": "manufacturer", "manufacturer name": "manufacturer",
}

// fieldName is the field that a column stands for: an alias, or the column
// name in lower case with underscores between its words.
func fieldName(column string) string {
	s := strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(strings.ToLower(column))
	norm := strings.Join(strings.Fields(s), " ")
	if f, ok := columnAliases[norm]; ok {
		return f
	}
	return strings.ReplaceAll(norm, " ", "_")
}

// readBOM reads the BOM: a fides CSV file keyed by name, or a KiCad BOM
// export, recognized by a Reference column and no name column. In both, a
// row can stand for a group of references (ExpandRefs), and rows marked DNP
// are left out.
func readBOM(file string) (*bom, error) {

	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte("\ufeff"))

	b := &bom{format: FormatFides, notes: map[string][]string{}}
	var raw []map[string]string
	if isKiCad(data) {
		b.format = FormatKiCad
		raw, err = readKiCad(data)
	} else {
		raw, err = readFides(file)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}

	seen := map[string]bool{}
	for _, r := range raw {
		group := r["name"]
		if b.format == FormatKiCad {
			group = r["reference"]
		}
		if group == "" {
			continue
		}
		refs, err := ExpandRefs(group)
		if err != nil {
			b.warn("%s: %v", file, err)
		}
		if n, err := strconv.Atoi(r["qty"]); err == nil && n != len(refs) {
			b.warn("%s: %s: quantity %d for %d references", file, group, n, len(refs))
		}
		dnp := isDNP(r["dnp"])

		for _, ref := range refs {
			up := strings.ToUpper(ref)
			if seen[up] {
				b.warn("%s appears more than once in %s (references are not case-sensitive); the first row is used", up, file)
				continue
			}
			seen[up] = true
			if dnp {
				b.dnp = append(b.dnp, up)
				continue
			}
			fields := maps.Clone(r)
			delete(fields, "reference")
			delete(fields, "qty")
			delete(fields, "dnp")
			fields["name"] = ref
			if len(refs) > 1 {
				b.notes[up] = append(b.notes[up], fmt.Sprintf("one of %s in the BOM", group))
			}
			b.rows = append(b.rows, bomRow{ref, fields})
		}
	}
	SortRefs(b.dnp)
	return b, nil
}

// isKiCad reports whether the header of a CSV file has a reference column
// and no name column.
func isKiCad(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		r := stdcsv.NewReader(strings.NewReader(line))
		r.LazyQuotes = true
		head, err := r.Read()
		if err != nil {
			return false
		}
		var fields []string
		for _, h := range head {
			fields = append(fields, fieldName(h))
		}
		return slices.Contains(fields, "reference") && !slices.Contains(fields, "name")
	}
	return false
}

// readKiCad reads a KiCad BOM export: quoted fields, the column names in the
// first row.
func readKiCad(data []byte) ([]map[string]string, error) {
	r := stdcsv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	r.Comment = '#'
	records, err := r.ReadAll()
	if err != nil || len(records) == 0 {
		return nil, err
	}
	var head []string
	for _, h := range records[0] {
		head = append(head, fieldName(h))
	}
	var rows []map[string]string
	for _, rec := range records[1:] {
		row := map[string]string{}
		for i, v := range rec {
			if v = strings.TrimSpace(v); v != "" && i < len(head) && head[i] != "" {
				row[head[i]] = v
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// readFides reads a fides CSV file, with the field names of fieldName.
func readFides(file string) ([]map[string]string, error) {
	a, err := csv.Read(file)
	if err != nil {
		return nil, err
	}
	var rows []map[string]string
	for _, r := range a {
		row := map[string]string{}
		for k, v := range r {
			row[fieldName(k)] = v
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// isDNP reports whether the value of a DNP column marks a part as not
// mounted: any value but empty, 0, no, n, false or -.
func isDNP(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v != "" && !slices.Contains([]string{"0", "no", "n", "false", "-"}, v)
}

var rangeRE = regexp.MustCompile(`^([A-Za-z_]+)(\d+)-([A-Za-z_]*)(\d+)$`)

// ExpandRefs expands a group of references as BOM tools write them: with
// commas, semicolons or spaces between them, and ranges such as R1-R3 or
// R1-3. "R1-R3, R5" gives R1, R2, R3 and R5. A range that cannot be expanded
// is an error, and stays as written.
func ExpandRefs(s string) ([]string, error) {
	var refs, bad []string
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || unicode.IsSpace(r) }) {
		m := rangeRE.FindStringSubmatch(tok)
		if m == nil {
			refs = append(refs, tok)
			continue
		}
		a, _ := strconv.Atoi(m[2])
		z, _ := strconv.Atoi(m[4])
		if m[3] != "" && !strings.EqualFold(m[3], m[1]) || z < a || z-a >= 10000 {
			bad = append(bad, tok)
			refs = append(refs, tok)
			continue
		}
		for n := a; n <= z; n++ {
			refs = append(refs, m[1]+strconv.Itoa(n))
		}
	}
	if len(bad) > 0 {
		return refs, fmt.Errorf("cannot expand the reference range %s", strings.Join(bad, ", "))
	}
	return refs, nil
}

// writeBOM writes BOM rows in the fides format to a temporary file, for
// csv.ReadTypedFields, and returns its path.
func writeBOM(rows []bomRow) (string, error) {

	keys := map[string]bool{}
	for _, r := range rows {
		for k := range r.fields {
			keys[k] = true
		}
	}
	delete(keys, "name")
	cols := append([]string{"name"}, slices.Sorted(maps.Keys(keys))...)

	// golib/csv unquotes "…" but knows no escapes
	clean := strings.NewReplacer(`"`, "'", `\`, "/", "\n", " ", "\r", " ")
	var sb strings.Builder
	sb.WriteString(strings.Join(cols, ",") + "\n")
	for _, r := range rows {
		for i, k := range cols {
			if i > 0 {
				sb.WriteByte(',')
			}
			if v := r.fields[k]; v != "" {
				sb.WriteString(`"` + clean.Replace(v) + `"`)
			}
		}
		sb.WriteByte('\n')
	}

	f, err := os.CreateTemp("", "fitcalc-bom-*.csv")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(sb.String()); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), f.Close()
}
