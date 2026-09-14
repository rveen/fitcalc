package spice

import (
	"fmt"
	"math"
	"strings"

	"github.com/rveen/electronics"
)

// maxSweepPoints bounds the number of points of a sweep.
const maxSweepPoints = 1000

// Sweep is a DC sweep of a voltage or current source, as ngspice's dc
// command: from Start to Stop in steps of Step.
type Sweep struct {
	Source            string // upper case: V1
	Start, Stop, Step float64
}

// ParseSweep reads a sweep written as SOURCE START STOP STEP, with SPICE
// values: "V1 10.8 13.2 0.2", "I1 0 10m 1m". The source is a voltage (V) or
// current (I) source; the step goes from start to stop.
func ParseSweep(s string) (*Sweep, error) {

	f := strings.Fields(s)
	if len(f) != 4 {
		return nil, fmt.Errorf("sweep %q: want SOURCE START STOP STEP", s)
	}
	w := &Sweep{Source: strings.ToUpper(f[0])}
	if c := w.Source[0]; c != 'V' && c != 'I' {
		return nil, fmt.Errorf("sweep %q: %s is not a voltage (V) or current (I) source", s, f[0])
	}
	for i, dst := range []*float64{&w.Start, &w.Stop, &w.Step} {
		v, err := electronics.SpiceValue(f[i+1])
		if err != nil {
			return nil, fmt.Errorf("sweep %q: %s is not a number", s, f[i+1])
		}
		*dst = v
	}
	if w.Step == 0 || (w.Stop-w.Start)/w.Step < 0 {
		return nil, fmt.Errorf("sweep %q: the step must go from start to stop", s)
	}
	if n := w.Points(); n > maxSweepPoints {
		return nil, fmt.Errorf("sweep %q: %d points, at most %d", s, n, maxSweepPoints)
	}
	return w, nil
}

// Points returns the number of points of the sweep, both ends included.
func (w *Sweep) Points() int {
	return int(math.Floor((w.Stop-w.Start)/w.Step+1e-9)) + 1
}

// Unit is the unit of the swept quantity: V or A.
func (w *Sweep) Unit() string {
	if w.Source[0] == 'I' {
		return "A"
	}
	return "V"
}

// Label names a point of the sweep: "V1 = 13.2 V".
func (w *Sweep) Label(x float64) string {
	return fmt.Sprintf("%s = %.4g %s", w.Source, x, w.Unit())
}

// String describes the sweep: "V1 from 10.8 V to 13.2 V in steps of 0.2 V
// (13 points)".
func (w *Sweep) String() string {
	u := w.Unit()
	return fmt.Sprintf("%s from %.4g %s to %.4g %s in steps of %.4g %s (%d points)",
		w.Source, w.Start, u, w.Stop, u, math.Abs(w.Step), u, w.Points())
}

func (w *Sweep) command() string {
	return fmt.Sprintf("dc %s %g %g %g", strings.ToLower(w.Source), w.Start, w.Stop, w.Step)
}
