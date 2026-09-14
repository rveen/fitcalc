package spice

import (
	"fmt"
	"math"
	"strings"

	"github.com/rveen/electronics"
)

// maxTranSteps bounds the number of steps of a transient analysis, from its
// start to its stop time.
const maxTranSteps = 100000

// Analysis is the analysis a deck runs instead of the operating point: a DC
// sweep (*Sweep) or a transient analysis (*Tran).
type Analysis interface {
	command() string // the ngspice command
	plot() string    // the name of the plot in the raw file
	title() string   // what the deck says it runs
}

func (w *Sweep) plot() string  { return "DC transfer characteristic" }
func (w *Sweep) title() string { return "DC sweep" }

// Tran is a transient analysis, as ngspice's tran command: from time 0 to
// Stop, with results recorded from Start on, in steps of Step and time steps
// of at most Max (0: ngspice's default).
type Tran struct {
	Step, Stop, Start, Max float64
}

// ParseTran reads a transient analysis written as TSTEP TSTOP [TSTART
// [TMAX]], with SPICE values: "10u 5m", "10u 5m 1m". Results before TSTART
// are not recorded, which leaves out the start-up of the circuit.
func ParseTran(s string) (*Tran, error) {

	f := strings.Fields(s)
	if len(f) < 2 || len(f) > 4 {
		return nil, fmt.Errorf("transient %q: want TSTEP TSTOP [TSTART [TMAX]]", s)
	}
	t := &Tran{}
	for i, dst := range []*float64{&t.Step, &t.Stop, &t.Start, &t.Max}[:len(f)] {
		v, err := electronics.SpiceValue(strings.TrimSuffix(strings.ToLower(f[i]), "s"))
		if err != nil {
			return nil, fmt.Errorf("transient %q: %s is not a time", s, f[i])
		}
		*dst = v
	}
	switch {
	case t.Step <= 0:
		return nil, fmt.Errorf("transient %q: the step must be positive", s)
	case t.Start < 0 || t.Stop <= t.Start:
		return nil, fmt.Errorf("transient %q: the stop time must be after the start time", s)
	case t.Max < 0:
		return nil, fmt.Errorf("transient %q: the largest time step must be positive", s)
	case (t.Stop-t.Start)/t.Step > maxTranSteps:
		return nil, fmt.Errorf("transient %q: %.0f steps, at most %d", s, (t.Stop-t.Start)/t.Step, maxTranSteps)
	}
	return t, nil
}

// Duration is the time over which the results are recorded.
func (t *Tran) Duration() float64 {
	return t.Stop - t.Start
}

// Label names a time point of the transient: "t = 1.25 ms".
func (t *Tran) Label(x float64) string {
	return "t = " + Time(x)
}

// String describes the transient: "from 0 to 5 ms in steps of 10 µs, recorded
// from 1 ms".
func (t *Tran) String() string {
	s := fmt.Sprintf("from 0 to %s in steps of %s", Time(t.Stop), Time(t.Step))
	if t.Max > 0 {
		s += ", time steps of at most " + Time(t.Max)
	}
	if t.Start > 0 {
		s += ", recorded from " + Time(t.Start)
	}
	return s
}

// command is the ngspice command, with 12 significant digits: SPICE values
// such as 10u are not exact in binary.
func (t *Tran) command() string {
	s := fmt.Sprintf("tran %.12g %.12g", t.Step, t.Stop)
	if t.Start > 0 || t.Max > 0 {
		s += fmt.Sprintf(" %.12g", t.Start)
	}
	if t.Max > 0 {
		s += fmt.Sprintf(" %.12g", t.Max)
	}
	return s
}

func (t *Tran) plot() string  { return "Transient Analysis" }
func (t *Tran) title() string { return "transient analysis" }

// timeUnits are the units of Time, from the largest.
var timeUnits = []struct {
	scale float64
	unit  string
}{{1, "s"}, {1e-3, "ms"}, {1e-6, "µs"}, {1e-9, "ns"}, {1e-12, "ps"}}

// Time formats a time with 4 significant digits in s, ms, µs, ns or ps:
// "1.25 ms", "0 s".
func Time(x float64) string {
	if x == 0 || math.IsNaN(x) {
		return fmt.Sprintf("%g s", x)
	}
	u := timeUnits[len(timeUnits)-1]
	for _, v := range timeUnits {
		if math.Abs(x) >= v.scale*(1-1e-9) {
			u = v
			break
		}
	}
	return fmt.Sprintf("%.4g %s", x/u.scale, u.unit)
}
