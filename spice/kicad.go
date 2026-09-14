package spice

import "strings"

// Ref returns the component reference that an element name stands for.
//
// KiCad names a SPICE element by its reference, and prefixes the device
// letter when the reference does not start with it: reference U1 with a
// subcircuit model becomes XU1, battery BT1 simulated as a voltage source
// becomes VBT1. If the name itself is not a known reference but the name
// without its first letter is, Ref returns the latter; otherwise it returns
// the name. Both are upper case, and known is called with upper-case
// references.
func Ref(name string, known func(ref string) bool) string {
	name = strings.ToUpper(name)
	if len(name) < 2 || known(name) {
		return name
	}
	if ref := name[1:]; known(ref) {
		return ref
	}
	return name
}
