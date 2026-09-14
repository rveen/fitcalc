// Package parts loads component metadata from CSV files and merges it with
// the netlist elements.
//
// The BOM comes first: a fides CSV file keyed by 'name', or a KiCad BOM
// export with grouped references. Type files in the fides format follow,
// inherited through the 'type' field, which a BOM row can also get from its
// part number or footprint. The KiCad footprint gives the package and the
// technology tags that the parts files leave out.
//
// Every value keeps a record of the file and type it came from, so that the
// report can show it.
package parts
