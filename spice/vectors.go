package spice

import "strings"

// deviceParams are the ngspice device parameters saved for each element
// kind, verified with ngspice 47. The power parameters are only a
// cross-check. Diode power (@d[p]) is not saved: ngspice 47 returns
// inf for it.
var deviceParams = map[string][]string{
	"R": {"i", "p"},
	"D": {"id"},
	"Q": {"ic", "ib", "ie", "p"},
	"M": {"id", "vgs", "vds", "p"},
	"J": {"id", "vgs", "p"},
}

// tranParams are the device parameters saved besides deviceParams in a
// transient analysis, verified with ngspice 43: the capacitor current, 0 at
// the operating point.
var tranParams = map[string][]string{
	"C": {"i"},
}

// Vectors returns the device vectors to save for the elements of c, such as
// @r1[i]. Node voltages and branch currents are saved with "save all".
func Vectors(c *Circuit) []string {
	return vectors(c, deviceParams)
}

// TranVectors returns the device vectors to save for the elements of c in a
// transient analysis, besides Vectors: @c1[i].
func TranVectors(c *Circuit) []string {
	return vectors(c, tranParams)
}

func vectors(c *Circuit, params map[string][]string) []string {
	var v []string
	for _, e := range c.Elements {
		for _, p := range params[e.Kind] {
			v = append(v, "@"+strings.ToLower(e.Name)+"["+p+"]")
		}
	}
	return v
}
