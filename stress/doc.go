// Package stress holds the normalized electrical stresses of a component and
// derives the device-specific ones (power, reverse voltage, …).
//
// A stress is a set of quantities, each with a unit and a source (SPICE,
// derived, metadata or default). The representation does not depend on any
// reliability model.
package stress
