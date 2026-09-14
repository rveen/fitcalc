package stress

import "math"

// Mean returns the mean of each quantity over the stresses of an element at
// the points of a sweep, with the unit and source of the first. A quantity
// missing at one of the points is left out.
func Mean(points []Stress) Stress {
	if len(points) == 0 {
		return nil
	}
	out := Stress{}
	for name, q := range points[0] {
		var sum float64
		n := 0
		for _, s := range points {
			if x, ok := s[name]; ok {
				sum += x.Value
				n++
			}
		}
		if n < len(points) {
			continue
		}
		note := "mean over the sweep"
		if q.Note != "" {
			note += " of " + q.Note
		}
		out[name] = Quantity{sum / float64(n), q.Unit, q.Source, note}
	}
	return out
}

// MeanAll is Mean for the stresses of all elements at the points of a sweep
// (DeriveAll of each point). An element without stress at one of the points
// is left out.
func MeanAll(points []map[string]Stress) map[string]Stress {
	out := map[string]Stress{}
	if len(points) == 0 {
		return out
	}
	for name := range points[0] {
		var ss []Stress
		for _, p := range points {
			if s, ok := p[name]; ok {
				ss = append(ss, s)
			}
		}
		if len(ss) == len(points) {
			out[name] = Mean(ss)
		}
	}
	return out
}

// Range returns the least and the largest value of a quantity over the
// stresses of an element at the points of a sweep, and the index of the point
// of the largest. ok is false if a point does not have it.
func Range(points []Stress, name string) (lo, hi float64, at int, ok bool) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for k, s := range points {
		q, has := s[name]
		if !has {
			return math.NaN(), math.NaN(), -1, false
		}
		lo = math.Min(lo, q.Value)
		if q.Value > hi {
			hi, at = q.Value, k
		}
	}
	return lo, hi, at, len(points) > 0
}
