// Package thermal is the steady-state thermal network of a circuit: thermal
// resistances between components, passive nodes (a board, a heat sink, the
// air of an enclosure) and the ambient, from which the temperature rise of
// every node follows from the power of all components.
package thermal

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"

	"github.com/rveen/electronics"
)

// Ambient is the node at the ambient temperature, the reference of the
// temperature rises.
const Ambient = "AMB"

// Edge is a thermal resistance between two nodes.
type Edge struct {
	From, To string  // upper case
	Rth      float64 // K/W
	Line     int     // in the file
}

// Network is a thermal network: its nodes are component references and
// passive nodes, besides Ambient.
type Network struct {
	File  string
	Edges []Edge
	Nodes []string    // in the order of the file, without Ambient; upper case
	z     [][]float64 // the transfer impedances, K/W: the rise of node i per W into node j
}

// Load reads a thermal network from a CSV file with the columns from, to and
// rth (K/W, SPICE suffixes allowed), in any order. Lines starting with # are
// comments. Node names are case-insensitive; amb is the ambient. Every node
// needs a path to the ambient.
func Load(file string) (*Network, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	n, err := Read(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	n.File = file
	return n, nil
}

// Read is Load from a reader.
func Read(r io.Reader) (*Network, error) {

	cr := csv.NewReader(r)
	cr.Comment = '#'
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1

	n := &Network{}
	col := map[string]int{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		line, _ := cr.FieldPos(0)
		for i := range rec {
			rec[i] = strings.TrimSpace(rec[i])
		}
		if len(col) == 0 {
			for i, h := range rec {
				col[strings.ToLower(h)] = i
			}
			for _, h := range []string{"from", "to", "rth"} {
				if _, ok := col[h]; !ok {
					return nil, fmt.Errorf("line %d: no %s column (want from, to, rth)", line, h)
				}
			}
			continue
		}
		field := func(h string) string {
			if i := col[h]; i < len(rec) {
				return rec[i]
			}
			return ""
		}
		e := Edge{From: strings.ToUpper(field("from")), To: strings.ToUpper(field("to")), Line: line}
		switch {
		case e.From == "" || e.To == "":
			return nil, fmt.Errorf("line %d: an edge needs from and to", line)
		case e.From == e.To:
			return nil, fmt.Errorf("line %d: an edge from %s to itself", line, e.From)
		}
		rth, err := electronics.SpiceValue(field("rth"))
		if err != nil || !(rth > 0) || math.IsInf(rth, 0) {
			return nil, fmt.Errorf("line %d: rth %q is not a positive thermal resistance in K/W", line, field("rth"))
		}
		e.Rth = rth
		n.Edges = append(n.Edges, e)
		for _, x := range []string{e.From, e.To} {
			if x != Ambient && !slices.Contains(n.Nodes, x) {
				n.Nodes = append(n.Nodes, x)
			}
		}
	}
	if len(n.Edges) == 0 {
		return nil, errors.New("no edges")
	}
	if err := n.solve(); err != nil {
		return nil, err
	}
	return n, nil
}

// solve calculates the transfer impedances: the inverse of the conductance
// matrix, by Gauss-Jordan elimination.
func (n *Network) solve() error {

	if err := n.grounded(); err != nil {
		return err
	}

	k := len(n.Nodes)
	index := map[string]int{}
	for i, x := range n.Nodes {
		index[x] = i
	}
	// [G | I]
	a := make([][]float64, k)
	for i := range a {
		a[i] = make([]float64, 2*k)
		a[i][k+i] = 1
	}
	for _, e := range n.Edges {
		g := 1 / e.Rth
		i, iok := index[e.From]
		j, jok := index[e.To]
		if iok {
			a[i][i] += g
		}
		if jok {
			a[j][j] += g
		}
		if iok && jok {
			a[i][j] -= g
			a[j][i] -= g
		}
	}
	for c := range k {
		p := c
		for r := c + 1; r < k; r++ {
			if math.Abs(a[r][c]) > math.Abs(a[p][c]) {
				p = r
			}
		}
		if a[p][c] == 0 {
			return errors.New("the network cannot be solved")
		}
		a[c], a[p] = a[p], a[c]
		f := a[c][c]
		for j := range a[c] {
			a[c][j] /= f
		}
		for r := range k {
			if r != c && a[r][c] != 0 {
				f := a[r][c]
				for j := range a[r] {
					a[r][j] -= f * a[c][j]
				}
			}
		}
	}
	n.z = make([][]float64, k)
	for i := range a {
		n.z[i] = a[i][k:]
	}
	return nil
}

// grounded returns an error naming the nodes without a path to the ambient.
func (n *Network) grounded() error {
	reached := map[string]bool{Ambient: true}
	for changed := true; changed; {
		changed = false
		for _, e := range n.Edges {
			if reached[e.From] != reached[e.To] {
				reached[e.From], reached[e.To] = true, true
				changed = true
			}
		}
	}
	var lost []string
	for _, x := range n.Nodes {
		if !reached[x] {
			lost = append(lost, x)
		}
	}
	if len(lost) > 0 {
		return fmt.Errorf("no path to %s from %s", strings.ToLower(Ambient), strings.Join(lost, ", "))
	}
	return nil
}

// Has reports whether node is a node of the network.
func (n *Network) Has(node string) bool {
	return slices.Contains(n.Nodes, strings.ToUpper(node))
}

// Z returns the transfer impedance from node b to node a, in K/W: the rise
// of a per W into b. It is 0 if either is not a node.
func (n *Network) Z(a, b string) float64 {
	i := slices.Index(n.Nodes, strings.ToUpper(a))
	j := slices.Index(n.Nodes, strings.ToUpper(b))
	if i < 0 || j < 0 {
		return 0
	}
	return n.z[i][j]
}

// Solution is the temperature of the nodes of a network for the power into
// them.
type Solution struct {
	Power map[string]float64 // W, by node; the nodes without power are left out
	Rise  map[string]float64 // K above the ambient, by node
	Own   map[string]float64 // K: the part of Rise from the node's own power
}

// Coupled is the rise of node from the power of the other nodes, K.
func (s *Solution) Coupled(node string) float64 {
	node = strings.ToUpper(node)
	return s.Rise[node] - s.Own[node]
}

// Solve returns the temperature rises of the nodes for the power into them,
// in W by node. Power into a name that is not a node is ignored.
func (n *Network) Solve(power map[string]float64) *Solution {
	s := &Solution{Power: map[string]float64{}, Rise: map[string]float64{}, Own: map[string]float64{}}
	for x, p := range power {
		if n.Has(x) && p != 0 {
			s.Power[strings.ToUpper(x)] = p
		}
	}
	for i, a := range n.Nodes {
		var rise float64
		for j, b := range n.Nodes {
			rise += n.z[i][j] * s.Power[b]
		}
		s.Rise[a] = rise
		s.Own[a] = n.z[i][i] * s.Power[a]
	}
	return s
}
