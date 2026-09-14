package spice

import "errors"

// ErrSimulation is wrapped by every error that ngspice itself causes
// (netlist errors, missing models or include files, no convergence), as
// opposed to errors in fitcalc's own inputs. fitcalc exits with status 2 for
// these.
var ErrSimulation = errors.New("simulation failed")
