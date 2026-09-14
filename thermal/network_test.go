package thermal

import (
	"math"
	"strings"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b)) }

// TestSolve: two components on a board, and a heat sink on one of them.
func TestSolve(t *testing.T) {
	n, err := Read(strings.NewReader(`# K/W
rth, from, to
10,  q1,   board
20,  R1,   board
5,   board, amb
2,   Q1,   sink
2k,  sink, amb
`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(n.Nodes, " ") != "Q1 BOARD R1 SINK" || len(n.Edges) != 5 || n.Edges[3].Line != 6 {
		t.Fatalf("nodes %v, edges %+v", n.Nodes, n.Edges)
	}

	// Without the heat sink (2 kΩ-like), Q1 sees 10 + 5 K/W; with it, less
	zq := n.Z("q1", "Q1")
	if zq >= 15 || zq < 14.8 {
		t.Errorf("Z(Q1, Q1) = %g, want a little below 15", zq)
	}
	if !near(n.Z("R1", "Q1"), n.Z("Q1", "R1")) || n.Z("R1", "X9") != 0 {
		t.Errorf("Z not symmetric, or not 0 for a missing node")
	}

	s := n.Solve(map[string]float64{"Q1": 1, "r1": 0.5, "C1": 3})
	if _, ok := s.Power["C1"]; ok {
		t.Error("power into C1, not a node")
	}
	for node, want := range map[string]float64{
		"Q1": 1*zq + 0.5*n.Z("Q1", "R1"),
		"R1": 1*n.Z("R1", "Q1") + 0.5*n.Z("R1", "R1"),
	} {
		if !near(s.Rise[node], want) {
			t.Errorf("rise of %s %g, want %g", node, s.Rise[node], want)
		}
	}
	if !near(s.Own["Q1"], zq) || !near(s.Coupled("q1"), 0.5*n.Z("Q1", "R1")) || s.Own["BOARD"] != 0 {
		t.Errorf("own %v, coupled %g", s.Own, s.Coupled("Q1"))
	}
}

// TestSolveSeries: a chain to the ambient adds up.
func TestSolveSeries(t *testing.T) {
	n, err := Read(strings.NewReader("from,to,rth\nU1,board,40\nboard,amb,10\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := n.Solve(map[string]float64{"U1": 0.5})
	if !near(s.Rise["U1"], 25) || !near(s.Rise["BOARD"], 5) || !near(s.Own["U1"], 25) || s.Coupled("U1") != 0 {
		t.Errorf("solution %+v", s)
	}
}

func TestReadErrors(t *testing.T) {
	for name, text := range map[string]string{
		"no rth column":   "from,to\nQ1,amb\n",
		"no edges":        "from,to,rth\n",
		"to itself":       "from,to,rth\nQ1,q1,10\n",
		"missing to":      "from,to,rth\nQ1,,10\n",
		"zero rth":        "from,to,rth\nQ1,amb,0\n",
		"bad rth":         "from,to,rth\nQ1,amb,ten\n",
		"no path to amb":  "from,to,rth\nQ1,amb,10\nR1,board,20\n",
		"only the ground": "from,to,rth\namb,AMB,1\n",
	} {
		if _, err := Read(strings.NewReader(text)); err == nil {
			t.Errorf("%s: no error", name)
		} else if name == "no path to amb" && !strings.Contains(err.Error(), "no path to amb from R1, BOARD") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
