package derating

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/rveen/fitcalc/parts"
)

// Rule is a derating limit: the largest ratio of a stress to a rating, for
// the components of a class that have some tags.
type Rule struct {
	Class  string   // FIDES class, or * for any
	Tags   []string // tags a component must all have
	Rating string   // the rating field (Ratings), or * for any
	Limit  float64  // the largest ratio, 1 = 100 %
	Source string   // where the rule comes from: file:line, or "default"
}

// Rules are derating rules, in the order of their file.
type Rules []Rule

// Default is the rule where no other applies: 80 % of every rating.
var Default = Rules{{Class: "*", Rating: "*", Limit: WarnRatio, Source: "default"}}

// Custom reports whether rs has rules besides the default one.
func (rs Rules) Custom() bool {
	return slices.ContainsFunc(rs, func(r Rule) bool { return r.Source != "default" })
}

// For returns the rule for a rating of component c. Of the rules that match
// its class, its tags and the rating, the most specific wins: the one with
// the most tags, then one for its class over one for any class, then one for
// the rating over one for any rating, then the first.
func (rs Rules) For(c *parts.Component, rating string) Rule {
	if len(rs) == 0 {
		rs = Default
	}
	best, score := Rule{Limit: WarnRatio, Source: "default"}, -1
	for _, r := range rs {
		if r.Class != "*" && !strings.EqualFold(r.Class, c.Class) ||
			r.Rating != "*" && r.Rating != rating ||
			slices.ContainsFunc(r.Tags, func(t string) bool { return !slices.Contains(c.Tags, t) }) {
			continue
		}
		s := 4 * len(r.Tags)
		if r.Class != "*" {
			s += 2
		}
		if r.Rating != "*" {
			s++
		}
		if s > score {
			best, score = r, s
		}
	}
	return best
}

// LoadRules reads derating rules from a CSV file with the columns class
// (* or empty for any), tags (space separated; the component must have them
// all), rating (* or empty for any) and limit (in percent), and adds the
// default rule after them. Other columns, such as a note, are ignored; lines
// starting with # are comments.
func LoadRules(file string) (Rules, error) {

	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comment = '#'
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1

	head, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for _, c := range []string{"class", "rating", "limit"} {
		if _, ok := col[c]; !ok {
			return nil, fmt.Errorf("%s: no %s column (the columns are class, tags, rating and limit)", file, c)
		}
	}

	var rules Rules
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		line, _ := r.FieldPos(0)
		get := func(c string) string {
			if i, ok := col[c]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}

		rule := Rule{
			Class:  strings.ToUpper(get("class")),
			Tags:   strings.Fields(strings.ToLower(get("tags"))),
			Rating: strings.ToLower(get("rating")),
			Source: fmt.Sprintf("%s:%d", file, line),
		}
		if rule.Class == "" {
			rule.Class = "*"
		}
		if rule.Rating == "" {
			rule.Rating = "*"
		}
		if rule.Rating != "*" && !slices.Contains(Ratings(), rule.Rating) {
			return nil, fmt.Errorf("%s: unknown rating %q (use %s or *)", rule.Source, rule.Rating, strings.Join(Ratings(), ", "))
		}
		limit, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(get("limit"), "%")), 64)
		if err != nil || limit <= 0 || limit > 100 {
			return nil, fmt.Errorf("%s: limit %q is not a percentage above 0 and up to 100", rule.Source, get("limit"))
		}
		rule.Limit = limit / 100
		rules = append(rules, rule)
	}

	if len(rules) == 0 {
		return nil, fmt.Errorf("%s: no derating rules", file)
	}
	return append(rules, Default...), nil
}
