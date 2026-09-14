package stress

import "fmt"

// Source is where a value comes from.
type Source int

const (
	SPICE    Source = iota + 1 // read from the ngspice results
	Derived                    // calculated from other values
	Metadata                   // taken from the parts CSV files
	Default                    // assumed
)

func (s Source) String() string {
	switch s {
	case SPICE:
		return "SPICE"
	case Derived:
		return "derived"
	case Metadata:
		return "metadata"
	case Default:
		return "default"
	}
	return "unknown"
}

// Mark is the one-letter mark of a source in reports: S, D, M or ?.
func (s Source) Mark() string {
	switch s {
	case SPICE:
		return "S"
	case Derived:
		return "D"
	case Metadata:
		return "M"
	}
	return "?"
}

// Quantity is a value with its unit and its origin.
type Quantity struct {
	Value  float64
	Unit   string // SI unit: V, A, W, °C
	Source Source
	Note   string // how it was obtained: "@r1[i]", "v(vcc) − v(out)", "vd·id"
}

func (q Quantity) String() string {
	return fmt.Sprintf("%g %s (%s: %s)", q.Value, q.Unit, q.Source, q.Note)
}
