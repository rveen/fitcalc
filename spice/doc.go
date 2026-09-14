// Package spice identifies the elements of a SPICE netlist, generates the
// ngspice deck for an operating point analysis, runs ngspice in batch mode
// and maps its results to the elements.
//
// The netlist is parsed only as far as element identity needs (name, type,
// nodes, value, model). ngspice interprets the circuit; this package never
// simulates, and never modifies the user's netlist.
package spice
