# fitcalc

**Project plan: SPICE-driven FIDES 2022 component reliability analysis**

*Initial implementation: Go command-line utility*

*Version 0.2 — planning baseline, revised after a review of the locally available code (2026-09-11)*

---

## 0. Changes from version 0.1

Version 0.1 was written without looking at the code already on disk. After reviewing it, the
following decisions were taken (details in the sections referenced):

| # | Decision | Section |
|---|---|---|
| D1 | Depend on `github.com/trukeio/reliability/fides` (newer than `rveen/fides`). Add a `go.mod` to `trukeio/reliability`. | 4, 13 |
| D2 | Targeted fixes to the fides/electronics libraries are part of M1, so that SPICE stresses actually reach the FIT calculation. | 8 |
| D3 | A subcircuit instance (`X…`) is one reliability item. Its internal model elements are not components. | 9 |
| D4 | The report covers the union of netlist and BOM. BOM-only parts (connectors, PCB, …) get a FIT from metadata and are flagged "no SPICE stress". Simulation-only elements (sources, …) are listed but excluded from FIT. | 9, 12 |
| D5 | The parts/metadata input keeps the existing fides CSV format (`name` key, `type` inheritance, several files). | 10 |
| D6 | M1 adds a derating/overstress check (V/Vmax, P/Pmax, I/Imax) that does not depend on FIDES. | 11 |
| D7 | ngspice results are read through `rveen/logb/spice`, which is extended to support ngspice raw files. | 7 |
| D8 | The minimal netlist parser is written in fitcalc. `golib/spice` is too rudimentary and cannot be imported. | 4, 6 |
| D9 | Capacitors at 0 V DC: follow the FIDES formula (the thermo-electrical term becomes zero) and report its result as is. | 8 |
| D10 | *Decided during M1.10; replaces D1.* Milestone 1 is a first deliverable under the MIT license. The `fides` package moves from the private `trukeio/reliability` module into fitcalc (`fides/`, with `cmd/fides`), which then depends only on published open-source modules. `trukeio/reliability` stays unchanged. FIDES 2022 only. | 4, 13 |
| D11 | *Decided after M1.10; replaces the fides part of D10.* There is one FIDES library, `github.com/rveen/fides`. Its v0.1.0 is fitcalc's `fides` package with its tests and the guide cases, under the MIT license (earlier versions were BSD-2). fitcalc depends on it like on the other `rveen` modules and has no copy of its own. | 4, 13 |
| D12 | *Decided after M1.* Milestone 1 is published as `github.com/rveen/fitcalc` (public, MIT), which only receives bug fixes and small improvements. The later milestones are developed in `github.com/trukeio/fitcalc` (private), a fork that keeps the module path `github.com/rveen/fitcalc` and merges `rveen/fitcalc` as `upstream`. Changes flow from public to private only; this plan stays private. | 13 |

---

## 1. Purpose

fitcalc is a command-line reliability-analysis tool. It uses ngspice to get electrical operating
stresses from a SPICE circuit and applies the FIDES 2022 reliability models through the existing Go
fides library.

The central design principle: **fitcalc is not a SPICE simulator**. ngspice is the electrical
simulation engine and the fides library is the reliability calculation engine. fitcalc connects
them, supplies component metadata, derives stresses and produces auditable reports.

## 2. Initial scope (milestone 1)

- Go command-line application, single executable.
- SPICE netlist input (`.net`, `.cir`). KiCad's SPICE export is the main source.
- Batch execution of ngspice with a `.OP` analysis.
- Identification of circuit elements and their SPICE types (from the first letter of the element
  name). Part numbers and FIDES metadata come from a BOM/parts CSV (KiCad BOM export, fides CSV
  format).
- Extraction of DC node voltages, device currents and device-specific operating quantities.
- Calculation of component power where meaningful.
- Mapping of stresses to FIDES component inputs, FIT calculation through the fides library.
- Derating/overstress check against component ratings.
- Markdown and CSV output.
- Explicit warnings for missing, defaulted or inconsistent inputs, with the source of every value.

## 3. Non-goals for milestone 1

- Do not implement a SPICE simulator.
- Do not implement a complete SPICE language parser. Parse only what component identity needs.
- No graphical user interface, database or server.
- No transient, AC or DC-sweep analysis.
- No automatic thermal modelling beyond `ΔT = P · Rth` (already used by the fides library for resistors).
- Do not duplicate the FIDES calculation logic. It lives in one place: upstream in
  `trukeio/reliability` until M1.9 (D2), in fitcalc's own `fides` package in M1.10 (D10), and
  in `rveen/fides` since v0.1.0 (D11).

## 4. Inventory of existing code

| Location | Content | Use in fitcalc |
|---|---|---|
| `trukeio/reliability/fides` | FIDES 2022 implementation: `Component`, `Bom`, `Mission`/`Phase`, `FIT()` dispatch by class, per-family models, `FITAll`/`TotalFIT`, LED model, `Ttotal` fix in inductor model. No `go.mod`. | **Reliability engine** (D1). Needs `go.mod` and the fixes in section 8. Moved into fitcalc as `fides/` in M1.10 (D10), then into `rveen/fides` v0.1.0 (D11). |
| `rveen/fides` | Older version of the above, plus `cmd/fides` (BOM + db + mission CSV → FIT table). Published, in the module cache. | `cmd/fides` is the model for the CLI and the CSV formats. Since v0.1.0 (D11) it holds fitcalc's FIDES library and is the reliability engine. |
| `rveen/golib/csv` | `Read` (CSV → `[]map[string]string`, `#` comments, quotes) and `ReadTyped` (first file = instances keyed by `name`, other files = types; `type` inheritance, tag merging). | **Parts/metadata loading** (D5). Two small bugs (section 8). |
| `rveen/electronics` | `Value()` (SI-suffix parser), `Rth(package)` (default thermal resistance per package). | `Rth` is used through fides. `Value` has SPICE-incompatible semantics (section 8). |
| `rveen/logb/spice` | Well-structured, tested reader for SPICE **binary raw** files (`ReadRaw`, `Raw`, `Layout`, `Var`). Written for LTspice: it assumes float32 non-axis values, rejects ASCII raw files, and has no per-value accessor. Module deps: `google/uuid`, `klauspost/compress`. | **ngspice result reader** (D7), after the extension in section 7. |
| `golib/spice` (`/files/go/src/golib/spice`, module `golib`) | Netlist sketch (`ReadNet2`: line-based, no continuation lines, no subckt awareness), `Simulate()` = bare `exec ngspice -b -r`, Monte Carlo/FIT fault-injection helpers (`Context.Pick`, `Rfit`), older raw reader. | Not imported (module path `golib` is not importable and the parser is too minimal). Reference material only, and possibly useful for M10 (Monte Carlo). |
| `rveen/ltspice` | Older LTspice raw reader and `lta` statistics tool. | Superseded by `logb/spice`. |
| `golib/k/rel` (module `golib`) | IEC 61709 and FMD-91 (failure-mode distribution) material, and `reloop` (LTspice `.step` fault-injection deck generator). Not reviewed in detail. | Not in M1. Candidate for an IEC 61709 backend (section 20) and for M10. |
| `trukeio/reliability/thermal`, `mission`, `stat`, `pof`, `model` | Foster/Cauer thermal networks, mission time series, Weibull/Monte Carlo, physics-of-failure models (elco, semiconductor, solder). | Not in M1. Candidates for M6, M7, M10 and for a second reliability backend (section 16). |

ngspice 47 (Fedora 43 package, KLU solver) is installed at `/usr/bin/ngspice`. The ngspice
behaviour described in sections 7 and 9 was verified with it on a probe circuit containing R, C, L,
D, BJT, MOSFET, JFET and a subcircuit instance.

## 5. Conceptual architecture

```
SPICE netlist ─┬─> netlist parser ─> elements ─┐
               │                               ├─> merge ─> components ─┐
BOM/parts CSV ─┼─> metadata loader ────────────┘                        │
               │                                                        v
               └─> deck generator ─> ngspice -b ─> raw file ─> OP values ─> stresses
                                                                        │
mission CSV ─────────────────────────────────────────> FIDES adapter <──┤
                                                            │           │
                                                            v           v
                                                     FIT results   derating check
                                                            └─────┬─────┘
                                                                  v
                                                        Markdown / CSV report
```

Concerns are kept separate:

- **spice**: netlist parsing, deck generation, ngspice execution and mapping of raw vectors to
  element quantities.
- **parts**: metadata loading (golib/csv) and merging with netlist elements.
- **stress**: normalized, backend-independent stress data and derivations (power, reverse voltage, …).
- **derating**: stress-ratio checks.
- **rel/fides**: adapter between fitcalc data and the fides library.
- **report**: Markdown and CSV rendering.
- **cmd/fitcalc**: flags and orchestration.

## 6. Go project structure

```
fitcalc/
├── go.mod                  module github.com/rveen/fitcalc
├── cmd/fitcalc/main.go     CLI, orchestration
├── spice/
│   ├── netlist.go          minimal parser: title, continuation, comments, .subckt skip, .include
│   ├── element.go          Element{Name, Kind, Nodes, Value, Model, Params, Line}
│   ├── deck.go             simulation deck generation (.control injection)
│   ├── runner.go           ngspice execution, log capture, error detection
│   └── op.go               raw vectors → per-element OP quantities (uses logb/spice)
├── parts/
│   ├── parts.go            ReadTyped wrapper, provenance, validation
│   └── merge.go            element ↔ BOM matching, class/tag mapping
├── stress/
│   ├── quantity.go         Quantity{Value, Unit, Source}
│   ├── stress.go           Stress = map[string]Quantity + helpers
│   └── derive.go           per-device derivations (section 9)
├── derating/derating.go
├── rel/fides.go            build fides.Component / call fides.FIT
├── report/
│   ├── markdown.go
│   └── csv.go
└── testdata/
    ├── divider.net, rc.net, diode.net, mosfet.net, mixed.net
    ├── bom.csv, db.csv, mission.csv
    └── golden/             expected reports
```

The adapter package is named `rel`, not `fides`, to avoid clashing with the imported `fides`
package.

### Core data model

```go
type Source int // SPICE, Derived, Metadata, Default

type Quantity struct {
    Value  float64
    Unit   string // "V", "A", "W", "°C", …
    Source Source
    Note   string // e.g. "V = V(n1) - V(n2)", "P = V·I", "from db.csv type RT50"
}

type Stress map[string]Quantity // "v", "i", "p", "vr", "vgs", "vds", "id", "tj", …

type Component struct {
    Ref      string            // R17, U3 (BOM reference; netlist name mapped to it)
    Kind     string            // SPICE element letter: R, C, L, D, Q, M, J, X, …
    Nodes    []string
    Value    Quantity
    Model    string
    Meta     map[string]string // merged parts metadata (after type inheritance)
    Class    string            // FIDES class: R, C, L, D, Q, U, J, X, PCB
    Tags     []string
    InNet    bool              // present in the netlist
    InBom    bool              // present in the BOM
    Stress   Stress
    Warnings []string
}

type Result struct {
    Component *Component
    FIT       float64 // NaN if not computable
    Err       error
    Derating  []Ratio
}
```

The stress model is a map, not fixed V/I/P fields, so RMS/peak/AC, temperature and device-specific
quantities can be added later without changing the type. Every quantity carries its source, which
makes the report auditable.

## 7. ngspice interaction

### Deck generation (`spice/deck.go`)

fitcalc never modifies the user's netlist. It writes a derived deck into a temporary directory:

1. Copy the netlist unchanged (title line first, `.include`/`.lib` paths made absolute).
2. Remove existing `.control … .endc` blocks and comment out other analysis cards (`.tran`, `.ac`,
   `.dc`, `.op`, `.save`), with a warning for each one.
3. Before `.end`, inject:

   ```
   .control
   set filetype=binary
   save all <device vectors>
   op
   write <tmp>/op.raw
   quit
   .endc
   ```

   `<device vectors>` is generated from the parsed elements, for example `@r1[i] @d1[id] @m1[id]
   @m1[vgs] @m1[vds] @q1[ic] @q1[ib]`. Node voltages and the branch currents of V sources and
   inductors are covered by `save all`.

Verified with ngspice 47:

- The raw file lists variables in **alphabetical order**, not in `save` order, so lookup is always
  by name.
- Names in the raw file are wrapped by type: `v(out)`, `i(@r1[i])`, `v(@m1[vgs])`, but `@r1[p]`
  (power, no wrapper). Branch currents appear as `i(v1)` and `i(l1)`, not `v1#branch`. All names
  are lower case.
- Elements inside subcircuit instances can be addressed as `@r.x1.ra[i]` (not needed in M1, see
  D3, but useful for M3 supply currents).
- The `.OP` plot is `Plotname: Operating Point`, `Flags: real`, `No. Points: 1`.
- The header carries `Command: ngspice-47, Build …`, which identifies the dialect and the version.

### Execution (`spice/runner.go`)

- `ngspice -b <deck>`. The executable is taken from `--ngspice`, then `$NGSPICE`, then `PATH`.
- Capture stdout/stderr into `ngspice.log`. In ngspice 47 batch mode only errors while reading the
  netlist (a missing model or `.include` file) give exit status 1. Simulation failures, such as no
  convergence or a loop of voltage sources, exit with status **0**. Failure is therefore detected
  from:
  - the exit status;
  - `Error`, `DC solution failed` or `simulation(s) aborted` lines in the log;
  - a missing raw file, or a raw file whose plot is not `Operating Point`.

  The relevant log lines are quoted in the error message. ngspice's `Warning` lines become warnings,
  with repetitions merged.
- A clear message for: executable not found, netlist error, missing model/include, no convergence.
- `-k DIR` keeps deck, log and raw file for audit.
- The ngspice version (`ngspice -v`) is recorded in the report.

### Result reading: extension of `rveen/logb/spice` (D7)

The LTspice-only assumptions have to be lifted:

1. **ngspice binary layout.** ngspice (like Spice3) writes *every* value as float64 without a
   `double` flag. LTspice writes non-axis values as float32. Verified: for the 31-variable probe,
   ngspice wrote 248 value bytes (31 × 8). The current `ReadRaw` computes `PointBytes = 128`
   (float32 values) and returns **no error**, because its length check only rejects blocks that
   are too short. It would silently decode wrong values. The fix:
   - Detect the dialect from the `Command: ngspice…` header, with an explicit
     `ReadOptions{Dialect}` override.
   - Make the length check exact (`len(values) == points · PointBytes`), so that any future layout
     mismatch fails loudly.
2. **Value accessor.** Add `(*Raw).Value(l Layout, point, v int) float64` (and a lookup by variable
   name) so users do not decode the binary block themselves.
3. **ASCII raw (`Values:`).** Optional: support it for debugging and audit (`set filetype=ascii`).
4. **Tests** with fixtures generated by ngspice 47 (OP, and a small transient for later
   milestones). Note that `spice_test.go` already references `testdata/test.op.raw`, which is
   missing from the repository.

The mapping in `spice/op.go` normalizes case and strips the `v(…)`/`i(…)` wrappers before looking
up vectors by name.

## 8. Library changes (part of M1, D2)

With the current library, SPICE results would barely affect the FIT. Semiconductors use
`Tj = Tamb` (`// TODO Add disipated power`), so only R power, C voltage and signal-diode voltage
are consumed. These targeted, tested changes are made upstream:

| Library | Change | Reason |
|---|---|---|
| `trukeio/reliability` | Add `go.mod` (`module github.com/trukeio/reliability`, go 1.25). | Cannot be imported in module mode otherwise. |
| `reliability/fides/semiconductor.go` | `tj = ph.Tamb + comp.T`. fitcalc sets `comp.T = P · Rth` (Rth from metadata `rtha`, else a package default). `T = 0` keeps the current behaviour. | SPICE power reaches the FIT for D/Q/U. |
| `reliability/fides/semiconductor.go` | Signal diodes: accept `V = 0` (forward-biased or unbiased). `PiThermal_voltageFactor` already floors the ratio at 0.3, so only a NaN/missing V should be an error. | Forward-biased signal diodes currently fail with "working V not set". |
| `reliability/fides/cap.go` | Accept `V = 0` (DC-blocked or unbiased capacitors) and apply the FIDES formula as it stands: the thermo-electrical term `(V/(Sref·Vmax))³` becomes zero and the FIT comes from the remaining terms. The result is reported as is, with no special treatment (D9). | Capacitors at 0 V DC currently fail with an error. |
| `reliability/fides` (all models) | "Not set" becomes NaN instead of 0 for working conditions (`V`, `I`, `P`), so that 0 V is a valid value. `Bom.FromCsvs` initialises them to NaN when the CSV has no value, so `cmd/fides` still reports a missing V instead of computing with 0 V. | Needed by the capacitor and diode changes. |
| `reliability/fides/resistor.go` | I→P fallback: the condition `I != 0 && IsNaN(I)` is never true, and the formula `Value·I` should be `I²·R`. Do not overwrite a user-supplied `Rtha` with the package default. | Correctness, and metadata must win over defaults. |
| `reliability/fides/af.go`, `rveen/fides/af.go` | *Found during M1.0.* `Arrhenius25` used `1/293` and `PiTCCase`/`PiTCSolder` used `1/313`. Both are untyped integer constant divisions and evaluate to 0, so every thermal and temperature-cycling term was practically zero. Fixed to `1.0/293` and `1.0/313` in both repositories. | Every FIT was far too low: the regression sample total goes from 3.52 to 121 FIT with this fix alone. |
| `reliability/fides/semiconductor.go` | *Found during M1.0.* `Lchip_th` for class Q: the bipolar rate overwrote the MOSFET, JFET, IGBT, GaN, GaAs and thyristor rates. It now applies only when no type tag matched. | For example, IGBT 0.0138 instead of 0.3021. |
| `trukeio/reliability` (`pof`, `thermal`, `stat`, `qual`, `cmd/fides`) | Stale `golib/electronics/...` and `golib/sim/thermal` import paths now point at the module's own packages. `cmd/fides` uses `reliability/fides` instead of `rveen/fides`. Fixed the `go vet` errors. | Needed for `go build ./...` in the new module. |
| `rveen/electronics` | Add `SpiceValue(s)` with SPICE semantics: case-insensitive, `m` = milli, `meg` = mega, `mil`, trailing units ignored (`10uF`, `1kOhm`). Keep `Value()` for BOM strings, where `1M` conventionally means mega. That is why `Value` maps `m` to 1e6 today, and changing it would break existing db files. Fix the `4k7`-style fraction handling (`n2/10` is wrong for more than one digit). | SPICE values would otherwise be parsed wrongly (`1m` → 1e6). |
| `rveen/golib/csv` | `ReadTyped`: return file read errors instead of ignoring them. Fix tag/type duplication in `addTypeInfo` (`o[k] += o[k] + " " + v`). | A missing db file must not pass silently. Tags are currently duplicated. |
| `rveen/logb/spice` | ngspice support (section 7). | D7. |

Each change gets a unit test in its own repository. FIT regression values for the existing
`cmd/fides` sample (bom/db/mission CSV) are recorded before the changes, so any unintended change
in results shows up.

**Status after M1.0:** all of the above is implemented except `logb/spice`, which is part of M1.4.

- The final API is `electronics.SpiceValue(s) (float64, error)`, `csv.ReadTypedErr(files)
  (map…, error)` (`ReadTyped` keeps ignoring unreadable files), and `fides.NewComponent(name)`,
  which initialises V, I and P to NaN. `Bom.FromCsvs` uses it.
- The regression test is `reliability/fides/regression_test.go`. It covers 23 components across
  all models, including the cases the changes affect, and is compared against
  `testdata/golden.txt`. The golden file was recorded before the changes and then updated with
  them; every difference is one of the intended changes above.
- Independent FIDES reference calculations are still needed (M1.10). Until then, the absolute FIT
  values are unverified.

Since M1.10 the library was fitcalc's `fides` package (D10), and since v0.1.0 it is
`rveen/fides` (D11), where the regression test is `regression_test.go`. The rows above name the
repository where each change was first made.

## 9. Component identification and stress derivation

### Netlist parser (`spice/netlist.go`)

- The first line is the title and is never an element.
- `+` continuation lines, `*` comments, inline `;` and `$` comments, case-insensitive names.
- `.subckt … .ends` bodies are skipped for element discovery (D3). `.include` is followed for
  top-level element lines. `.lib` sections are assumed to hold models only.
- Each element keeps its original line. `.param` and `{expr}` values are left to ngspice. The
  resolved value is reported when ngspice provides it, otherwise "expression".

### Element → FIDES class mapping

SPICE letters and FIDES classes collide: SPICE `J` is a JFET but FIDES `J` is a connector, and
SPICE `X` is a subcircuit but FIDES `X` is a crystal. The mapping is therefore an explicit table,
and a `class` in the BOM always overrides it:

| SPICE | Device | Default FIDES class / tags | Stresses (OP) |
|---|---|---|---|
| R | resistor | R | V = V(n1)−V(n2), I = `@r[i]`, P = V·I |
| C | capacitor | C | V = V(n+)−V(n−) (sign checked for polarized parts: `alu`, `tant`) |
| L | inductor | L | I = `i(l1)`, P = I²·DCR if `dcr` is in metadata |
| D | diode | D | Vd, Id = `@d[id]`, Vr = max(−Vd, 0) → FIDES V, P = Vd·Id |
| Q | BJT | Q | Vce, Vbe, Ic, Ib (`@q[ie]` also available), P = Vce·Ic + Vbe·Ib |
| M | MOSFET | Q + `mos` | Vds, Vgs, Id, P = Vds·Id |
| J | JFET | Q + `jfet` | Vds, Vgs, Id, P = Vds·Id |
| X | subcircuit instance | from BOM (usually U), else warning | pin voltages; P only from metadata in M1, from the pin currents since M3 (P = Σ V·I) |
| V, I, E, F, G, H, B, K, S, W, T, … | sources, controlled sources, couplings, switches, lines | not a reliability item | listed as "simulation-only" |

Device vectors verified with ngspice 47: `@r[i]`, `@r[p]`, `@c[i]` (0 at DC), `@d[id]`, `@q[ic]`,
`@q[ib]`, `@q[ie]`, `@q[p]`, `@m[id]`, `@m[vgs]`, `@m[vds]`, `@m[p]`, `@j[id]`, `@j[vgs]`, `@j[p]`.
The `p` values of R, Q, M and J match V·I from node voltages and currents. **`@d[p]` returns
`inf`** for a forward-biased diode. Powers are therefore always derived in fitcalc as V·I, and
the device `p` parameters are used only as a cross-check where they are finite. The probe circuit
becomes a test fixture (`testdata/probe.net`).

### Netlist ↔ BOM matching

- The key is the BOM `name` (the reference, upper-cased).
- KiCad prefixes the device letter when the reference does not start with it (e.g. reference
  `U1` with a subcircuit model becomes `XU1`, battery `BT1` becomes `VBT1`). If the element name
  is the device letter followed by an existing BOM reference, it maps to that reference.
- A netlist value that disagrees with a BOM value (beyond tolerance) is a warning.
- Netlist-only element without BOM data: stresses are reported, and FIT is attempted with
  defaults and a warning for each missing FIDES input.
- BOM-only part: FIT from metadata (as `cmd/fides` does), flagged "no SPICE stress" (D4).

### FIDES inputs from stresses

| fides.Component field | Source |
|---|---|
| `V` | R: \|V\|, C: \|V\|, D: Vr, Q/M/J: Vce/Vds (reported; not used by the current FIDES models) |
| `I` | device current |
| `P` | derived power |
| `T` | `P · Rth` for D/Q/U (after the section 8 change) |
| `Value` | netlist value (SPICE semantics), else BOM value |
| all ratings, package, class, tags | metadata |

Operating point temperature: M1 runs one `.OP` at the ngspice default (27 °C). M6 adds one `.OP`
per operating mission phase temperature (section 19).

## 10. Component metadata (D5)

The `cmd/fides` format and loader (`golib/csv.ReadTyped`) are reused unchanged:

- `-p` can be repeated. The first file is the BOM (instances keyed by `name`). The following files
  (db.csv, pkg.csv, …) provide types, inherited through the `type` field, recursively.
- Recognized fields: `name, type, class, tags, value, tolerance, package, ndevices, npins, tmax,
  tmin, vmax, vpmax, pmax, imax, rtha, tc, description, block`. New in fitcalc: `dcr` (inductors)
  and `rth` (semiconductor junction-to-ambient, if different from `rtha`). New in M1.10 for the
  PCB model (FIDES 2022, pp. 174-176): `layers`, `mounts` (number of mounted component
  terminations), `piclass` and `pitechno`.
- `v`, `i`, `p`, `t` columns in the BOM are still accepted as **manual overrides** of SPICE
  stresses, marked as `Metadata` in the report.

fitcalc builds `fides.Component` itself from the `ReadTyped` result instead of calling
`Bom.FromCsvs`. This keeps provenance (which file and type supplied each value), reports parse
errors instead of dropping them, and applies the right value parser per source.

Example (as in the `rveen/fides` samples, originally in its `cmd/fides`):

```
# bom.csv
name, type, block, tags
R17,  RT50, B1,
C4,   CAP,  B1,

# db.csv
name, class, value, tolerance, tags,     package, tmax, vmax, pmax
CAP,  C,     10n,   5%,        x7r flex, 0603,    155,  50,
RT50, R,     51,    5%,        thick,    0805,    155,  200,  0.125
```

A large component database is not a prerequisite for M1. M2 adds KiCad BOM exports as the first
file, and types found from part numbers and footprints (section 19).

## 11. Derating / overstress check (D6)

For each component with a stress and a matching rating:

| Ratio | Stress | Rating |
|---|---|---|
| V/Vmax | \|V\| (C, R), Vr (D), Vds/Vce (Q) | `vmax` |
| P/Pmax | P | `pmax` |
| I/Imax | \|I\| | `imax` |

M1 uses fixed thresholds: above 100 % is an **overstress** (error in the report), and above 80 %
is a **warning**. M3 adds per-class derating rules (`--derating`, e.g. from a derating standard)
and the ratios V/Vpmax, Vgs/Vgsmax, Vcb/Vcbmax and Veb/Vebmax (section 19). The check is independent of FIDES, so it also covers quantities the
FIDES models ignore.

## 12. CLI

```
fitcalc circuit.net
fitcalc -p bom.csv -p db.csv -m mission.csv circuit.net
fitcalc -o reliability.md -p bom.csv -p db.csv -m mission.csv circuit.net
fitcalc -f csv -p bom.csv -p db.csv -m mission.csv circuit.net
```

| Flag | Meaning |
|---|---|
| `-p, --parts FILE` | Parts metadata CSV. Repeatable; the first one is the BOM. |
| `-m, --mission FILE` | FIDES mission profile CSV. Without it, fitcalc reports stresses and derating only and warns that no FIT was computed. |
| `-o, --output FILE` | Output file. Default: the netlist path with `.md` (or `.csv`) as extension, so `dir/circuit.net` gives `dir/circuit.md`. `-` writes to stdout. The report may not overwrite an input file. |
| `-f, --format markdown\|csv` | Report format. Default: markdown. |
| `--ngspice PATH` | ngspice executable (default `$NGSPICE`, then `PATH`). |
| `-k, --keep DIR` | Keep the generated deck, ngspice log and raw file. |
| `-v, --verbose` | Diagnostic output to stderr. |

The standard library `flag` package is used, with short and long forms registered for the same
variable. Exit status: 0 on success (warnings allowed), 1 on usage/input errors, 2 on simulation
failure.

## 13. Build setup

- `go.mod`: `module github.com/rveen/fitcalc`, go 1.25.
- License: MIT (`LICENSE`). The dependencies are open source: `rveen/electronics`, `rveen/golib`
  and `rveen/ogdl` (BSD-2), `rveen/logb` and `rveen/fides` (MIT), `google/uuid` and
  `klauspost/compress` (BSD).
- The FIDES implementation is `rveen/fides` (D11). Its v0.1.0 is the `fides` package this module
  had in M1.10 (D10), which was copied from `trukeio/reliability` at a60488e with the M1.0 and
  M1.10 fixes. `trukeio/reliability` is not a dependency, so no private module, `GOPRIVATE`
  setting or git URL rewrite is needed.
- Committed `go.mod` files require only published versions, with no `replace` directives. Local
  development across repositories uses a `go.work` file, which is gitignored. It uses `.`,
  `../../rveen/golib` and `../../rveen/logb`, and replaces `github.com/rveen/fides` with
  `../../rveen/fides` (a `use` of fides would still look up the required version remotely).
- Publishing order for library changes: push `rveen/electronics`, `rveen/golib`, `rveen/logb`
  and `rveen/fides` first (tagging `rveen/fides`), then bump their versions in `go.mod`, then
  push fitcalc. Before pushing, run the tests
  with `GOWORK=off`, so they use only published versions.
- Repositories (D12): `github.com/rveen/fitcalc` is public and `github.com/trukeio/fitcalc` is
  its private fork, with the same module path. The history of the private repository before the
  fork is tagged `m1-original`.
  - Bug fixes, small improvements and library fixes are made in `rveen/fitcalc` (and the `rveen`
    libraries) and then merged into the fork: `git fetch upstream && git merge upstream/main`.
  - A fix found during private work is made in `rveen/fitcalc` first, or cherry-picked there as
    a single commit. The fork is never merged into `rveen/fitcalc`, and pushing to `upstream` is
    disabled in the fork's clone (`git remote set-url --push upstream DISABLE`).
  - Private code may depend on private modules (`trukeio/reliability`, with `GOPRIVATE`);
    `rveen/fitcalc` may not. The code taken from `rveen/fitcalc` keeps its MIT notice.
  - The fork is built from its checkout (`go build ./cmd/fitcalc`), not with `go install …@latest`,
    which would fetch the public module.
- ngspice 47 (installed) is required at runtime and for integration tests. Integration tests call `t.Skip`
  when ngspice is not found, so unit tests still run everywhere.

## 14. Milestone 1 work packages

| ID | Work package | Deliverable |
|---|---|---|
| M1.0 | Library preparation | `go.mod` in `trukeio/reliability`. FIT regression baseline of the `cmd/fides` sample. Library fixes of section 8, each with tests. **Done** (section 8, status). |
| M1.1 | Project skeleton | Go module, CLI entry point, package boundaries, error handling, version info. **Done:** `cmd/fitcalc` (flags of section 12, which may also follow the netlist; exit statuses; `slog` diagnostics with `-v`), one package with its doc comment per boundary, `spice.ErrSimulation` for exit status 2, and `internal/version` (the module version stamped by the go command, printed by `--version`). The analysis itself ends with "not implemented yet". |
| M1.2 | Netlist parsing | Minimal parser (section 9), `SpiceValue`, KiCad name mapping, unit tests on netlist fragments. **Done:**<br>• `spice.Parse` returns the title, the top-level elements (name, kind, nodes, value, model, parameters, source location and text), the models, subcircuits, directives and included files, plus warnings (duplicates, unknown element types, missing nodes, unclosed blocks).<br>• Relative include paths resolve against the including file's directory; verified with ngspice 47 on a nested include run from another working directory.<br>• A BJT's fourth (substrate) node and ngspice's 3-node VDMOS transistors are recognised from the model names in the netlist, its `.include` files and its `.lib` files.<br>• `spice.Ref` removes KiCad's added device letter (`XU1` → `U1`).<br>• Fixtures: `testdata/probe.net` and a hand-written `testdata/kicad.net`, both accepted by ngspice. KiCad writes net names such as `Net-_D1-A_`, since ngspice treats parentheses as separators. The KiCad fixture should be replaced by one exported from the KiCad version in use. |
| M1.3 | ngspice runner | Deck generation (golden tests, no ngspice needed), execution, log capture, failure detection. **Done:**<br>• `spice.Deck` is a pure function with golden tests.<br>• `spice.Runner.OP` runs `ngspice -b -n` (`-n` keeps any `.spiceinit` file from changing the result) and writes `fitcalc.cir`, `ngspice.log` and `op.raw`, which `-k` keeps. Exit status 2 on simulation failure.<br>• Findings with ngspice 47:<br>  – `save` commands add up.<br>  – A device vector of a missing element, such as an element line ngspice ignored as invalid, is reported as "not available", and then no raw file is written at all.<br>  – An unknown parameter of an existing element is written silently as 0, marked `dims=0` in the raw header.<br>  – Both kinds are removed and ngspice runs again, at most 3 times; they are reported as unavailable. **M1.4 must also ignore any `dims=0` variable.**<br>  – A floating node converges through ngspice's transient-op fallback, with singular-matrix warnings, which are passed on. |
| M1.4 | logb/spice extension and OP extraction | ngspice dialect, value accessor, fixtures. Mapping of raw vectors to element quantities. Device vector names verified against ngspice 47. **Done:**<br>• **`logb/spice`**:<br>  – The dialect is detected from `Command: ngspice…` and can be overridden with `ReadOptions`; ngspice files are read with f64 values.<br>  – The length check is exact (`ErrLongValues`).<br>  – ASCII (`Values:`) files are read.<br>  – New accessors `Index`, `Value` and `ComplexValue`.<br>  – An ngspice axis keeps its sign (DC sweeps); the absolute value is taken only for LTspice binary files.<br>  – The type column is matched by its first word (`frequency grid=3`).<br>  – ngspice 47 fixtures (OP, transient, DC, AC; binary and ASCII) come from `testdata/ngspice.cir`. The LTspice OP test skips while its fixture is missing.<br>• **fitcalc**:<br>  – `spice.ReadOP` returns the values by vector name and leaves out `dims=0` variables.<br>  – `Values.Element` gives each element its node voltages, the device parameters of its kind, its branch current (V, L) and any expected vector that is missing.<br>  – Tested on the probe and KiCad circuits. ngspice's power parameters match V·I, and the MOSFET `vds` matches the node voltages. |
| M1.5 | Stress derivation | `Stress`/`Quantity` with provenance, per-device rules (section 9). **Done:**<br>• `stress.Quantity` holds a value, a unit, a source (SPICE, derived, metadata, default; report marks S/D/M/?) and a note on how it was obtained, e.g. `@r1[i]`, `v(vcc) − v(out)` or `vce·ic + vbe·ib`.<br>• `stress.Derive`/`DeriveAll` apply the section 9 rules:<br>  – every stressed component gets the generic `v`, `i`, `p`, plus its specific quantities: `vd`, `vr` for diodes; `vce`, `vbe`, `ic`, `ib`, `ie` for BJTs; `vds`, `vgs`, `id` for MOSFETs and JFETs; `v:<port>` per subcircuit pin, with `v` the largest voltage between two pins;<br>  – power is V·I and is checked against ngspice's `@x[p]` where it exists: a match is noted, a difference is a warning;<br>  – sources and other simulation-only elements get no stress.<br>• Quantities that need metadata (inductor power from `dcr`, junction temperature from `rtha`) are added in M1.6/M1.7. |
| M1.6 | Parts metadata | `ReadTyped` wrapper with error reporting, merge with netlist, class/tag mapping, warnings. **Done:**<br>• **`golib/csv`**. The old `ReadTyped` let a grandparent type override its parent (a resistor typed R0603 → SMD became class X), ignored multiple types, and could not tell where a value came from. Fixed (decision taken in M1.6):<br>  – The nearest definition wins, and multiple types are taken in order.<br>  – Rows with the same name merge, the earlier row winning.<br>  – Tags accumulate without repetition.<br>  – Undefined types and cycles give warnings.<br>  – New `ReadTypedFields` returns each value with its file and row.<br>  – `Read` no longer panics on an empty file.<br>• **`parts.Load`** reads the BOM and type files (references upper case).<br>• **`parts.Merge`** matches elements to parts through `spice.Ref` and returns the components, followed by the BOM-only parts ("no SPICE stress"), and the simulation-only elements.<br>  – The class comes from the parts files, or else from the element kind (M and J also add the tags `mos` and `jfet`); a conflict is a warning.<br>  – R, C and L values are checked against the BOM value within its tolerance.<br>  – The parts fields `v`, `i`, `p`, `t` override the stress, marked Metadata; `v` goes to `parts.VoltageKey(class)`, i.e. `vr` for diodes.<br>  – Inductor power is i²·`dcr`.<br>• Fixtures: `testdata/probe.bom.csv` and `testdata/parts.db.csv`. |
| M1.7 | FIDES adapter | Build `fides.Component`, load the mission, call `fides.FIT`, collect errors per component. **Done:**<br>• `rel.FIT` builds a `fides.NewComponent` per component:<br>  – class, tags, package, ratings, value (netlist, else parts files), `ndevices`, `npins`;<br>  – FIDES V from `parts.VoltageKey` (`vr` for diodes), and I and P, all as magnitudes;<br>  – T: the parts field `t`, else P·Rth for D, Q and U, with Rth from `rth`, then `rtha`, then the FIDES default for the package on an FR4 board (`Package.Rtha(0)`). A missing power or thermal resistance is a warning.<br>• Every FIDES input is recorded with its origin, for the report.<br>• What the fides library logs (e.g. an unknown package) becomes a warning of that component, and a panic in the library becomes that component's error.<br>• `rel.LoadMission` loads the mission; `main` loads the parts files and the mission before running ngspice.<br>• Probe circuit with `testdata/mission.csv` (the `cmd/fides` sample): every component gets a FIT, about 66 FIT in total. |
| M1.8 | Derating check | Ratios and thresholds (section 11). **Done:**<br>• `derating.Check`/`CheckAll` compute V/Vmax (with the stress from `parts.VoltageKey`), P/Pmax and I/Imax, as magnitudes and with their rating source, for every component that has both the stress and the rating. Parts-file overrides count too.<br>• Above 80 % is a warning, above 100 % an overstress.<br>• A negative voltage on a polarized capacitor (tags `alu`, `elco`, `tant`, `tantalium`) is an overstress.<br>• The probe circuit is within its ratings. With R3 as a 0603 part, P/Pmax = 127 % is reported as an overstress; the fides library then gives no FIT for it ("Actual power exceeds its Pmax"). |
| M1.9 | Reports | Markdown and CSV (section 15), golden-file tests. **Done:**<br>• `report.Markdown` has these sections:<br>  – header: netlist, date, versions of fitcalc, the FIDES library (from the build info) and ngspice;<br>  – inputs with their SHA-256, the netlist includes among them;<br>  – analysis: the simulation temperature ngspice reports, and the assumptions;<br>  – summary: total FIT, MTBF, components without FIT, derating counts, FIT per block, the 10 largest contributors;<br>  – components: value, V, I, P, T and FIT, each value marked ˢ/ᴰ/ᴹ/ˀ for its source;<br>  – derating;<br>  – FIDES metadata with the rows of the parts files it comes from;<br>  – simulation-only elements and BOM-only parts;<br>  – mission profile;<br>  – warnings, general and per component.<br>• `report.CSV` writes one row per component, with a source column for every stress.<br>• `report.Warnings(row)` is shared by the report and the log.<br>• `fitcalc` writes the report to the output file (or stdout) and prints a one-line summary on stderr.<br>• Golden tests on a fixed sample report (`testdata/golden/report.md`, `.csv`); `cmd/fitcalc` checks both formats on the probe circuit. |
| M1.10 | Validation | Reference circuits and independently checked values (section 17). **Done:**<br>• `testdata/validation/fides_ref.py` computes the FIT of the probe circuit's 11 models (thick film resistor, X7R capacitor, power inductor, signal diodes, BJT, MOS, JFET, analog IC in SO8, PCB connector) from the FIDES Guide 2022 Edition A, without the fides library.<br>• `TestFIDESReference` requires fitcalc to match it within 10⁻⁴; it matches within 2·10⁻⁶.<br>• This required FIDES 2022 fixes in `trukeio/reliability/fides`, decided during M1.10: the ΠTCy cycle duration exponent (1.3 → 1/3), the 2022 discrete package table, ΠPM 1.6 for passives, the resistor temperature Tamb + A·P/Prated, the high-power resistor choice by rating, analog IC λ0TH 0.086, QFN 64–80 coefficients, ceramic category 3 γMech, environmental pollution "moderate" = 1.5, the diode ΠEl floor 0.056 and 1/k_B = 11604.<br>• **Library moved (D10):** the `fides` package and `cmd/fides` are now part of fitcalc, which is MIT-licensed and no longer depends on the private module. The report header names fitcalc's version only, since the FIDES implementation is part of it.<br>• **All implemented families validated:** `testdata/validation/cases.csv` has 99 cases covering every model and table row the library implements: resistors, ceramic (type I/II, categories 1–3, flexible terminations), aluminium and tantalum capacitors, magnetic components, discrete semiconductors in every package group, power MOS and IGBTs, LEDs, optocouplers, crystals and oscillators, PCBs, connectors, and ICs in 27 package families. `fides_cases.py` computes them from the guide; `TestGuideCases` (fides package) requires the library to choose the same model from class, tags and package and to match within 10⁻⁹.<br>• This found and fixed further deviations: optocouplers always gave 0 FIT; the LED model and wirewound, network and potentiometer resistors were not the guide's; tantalum and piezo rows; the PCB model was unreachable and had wrong coefficients; power MOS and IGBTs lacked ΠPW and the 60 °C reference; connector placement; Csensitivity per table row; missing IC packages and 2022 Rth defaults.<br>• Not supported, and reported as such: fuses, relays, switches, film capacitors, ASICs, GaN/GaAs (RF) components.<br>• Details, the list of families not validated yet, and how to rerun it are in `testdata/validation/README.md`. |

Suggested order: M1.0 → M1.1 → M1.2 → M1.3/M1.4 → M1.5 → M1.6/M1.7 → M1.8 → M1.9, with M1.10
growing alongside. The first vertical slice (section 18) should run end to end after M1.7.

## 15. Report design

The Markdown report must be auditable, not just a list of FIT values:

1. **Header**: circuit title and file, date, fitcalc version, fides module version (from
   `debug.ReadBuildInfo`), ngspice version, SHA-256 of every input file.
2. **Analysis**: `.OP`, simulation temperature, the stated assumptions (section 9).
3. **Summary**: total FIT, FIT per block (`FITByBlock`), MTBF, number of components with errors
   or warnings, top contributors (sorted, % and cumulative %, as in `Bom.Report`).
4. **Component table**: reference, class, value, V, I, P, FIT. The source of each value is marked
   (e.g. `S` = SPICE, `D` = derived, `M` = metadata, `?` = default).
5. **Derating table**: ratios, overstress and warnings.
6. **FIDES metadata** per component (class, tags, package, ratings).
7. **Excluded elements**: simulation-only elements and BOM-only parts without stress.
8. **Mission profile** (`Mission.ToMD()`).
9. **Warnings and errors**, grouped by component.

Example component table:

| Ref | Class | Value | V | I | P | FIT |
|---|---|---|---|---|---|---|
| R1 | R | 10 kΩ | 5.0 V ˢ | 0.5 mA ˢ | 2.5 mW ᵈ | 0.32 |
| R2 | R | 1 kΩ | 4.5 V ˢ | 4.5 mA ˢ | 20.3 mW ᵈ | 0.38 |

The CSV report has one row per component with fixed columns (ref, class, tags, package, value,
v, i, p, t, fit, vratio, pratio, iratio, warnings), plus provenance columns (`v_src`, …).

## 16. Important technical decisions

| Decision | Reason |
|---|---|
| ngspice remains authoritative for simulation | Avoid reproducing SPICE device physics in fitcalc. |
| Minimal parser in fitcalc | Parse only what component identity needs and let ngspice interpret the circuit. Existing parsers are too rudimentary or not importable. |
| Stress model independent of FIDES | Allows other reliability backends (e.g. `reliability/pof`) or revised FIDES implementations. |
| One FIDES implementation, `rveen/fides` (D10, D11) | Milestone 1 is an MIT-licensed deliverable that must build from public modules, and the FIDES code is maintained in one place for fitcalc and other users. The private `trukeio/reliability` stays available for later, possibly commercial, work. |
| Auditability is a first-class feature | Every value records its source: SPICE, derived, metadata or default. Inputs are hashed and tool versions recorded. |
| Generated deck, never an edited netlist | The user's netlist stays the single source. The deck is reproducible and can be kept with `-k`. |
| Raw files instead of parsing printed output | Structured, lossless, and reuses a tested reader. |
| Explicit SPICE → FIDES class table | SPICE and FIDES letters collide (J, X). |
| No GUI in M1 | The CLI and analysis API become the stable foundation for any future UI. |

## 17. Validation strategy

- **Unit**: `SpiceValue` (`1m`, `1meg`, `4k7`, `10uF`, `1e-3`), netlist parsing (continuation,
  comments, subckt skipping, title line), KiCad name mapping, class table, deck generation
  (golden), raw reading (ngspice fixtures).
- **Resistor divider**: V, I, P against hand calculation.
- **RC network**: capacitor DC voltage, zero DC current. A capacitor at 0 V DC gets the
  FIDES-formula FIT without an error (D9). A BOM-only capacitor without any V is still reported as
  missing V.
- **Diode**: forward case (Vr = 0 still gives a FIT) and reverse-biased case (Vr → FIDES V).
- **MOSFET/BJT**: Vds/Vgs/Id or Vce/Ic extraction, P, `T = P·Rth` applied.
- **Mixed circuit**: every element appears exactly once. Sources are listed as simulation-only,
  and BOM-only parts appear with "no SPICE stress".
- **Metadata**: missing ratings and missing db files give warnings/errors, never silent
  assumptions. Type inheritance is honoured and BOM overrides work.
- **ngspice failures**: malformed netlist, missing model/include, executable not found, no
  convergence.
- **Derating**: overstress and warning thresholds.
- **FIT reference values**: an independent calculation (spreadsheet or script from the FIDES 2022
  guide) for the divider and diode circuits, plus the regression baseline from M1.0. Done in
  M1.10 for the probe circuit and for one case per implemented model and table row
  (`testdata/validation`).
- **Golden reports**: stable Markdown/CSV output for controlled inputs and pinned library versions
  (volatile header fields are masked).

## 18. Recommended first prototype

Do not start with a complicated real automotive circuit. Start with a small test circuit
containing a resistor divider, a capacitor, a diode and one MOSFET, and prove the complete
vertical slice:

**netlist → ngspice .OP → stresses → FIDES → Markdown report**

Once this slice is trustworthy, extend component coverage and metadata before adding UI or
advanced simulation modes.

## 19. Roadmap after milestone 1

| Milestone | Capability | Purpose / existing code |
|---|---|---|
| M2 | Component/BOM metadata | Direct KiCad BOM import (grouped references such as `R1,R2,R5` expanded), manufacturer/part-number mapping, package and technology mapping. **Done:**<br>• **KiCad BOM**: the first `-p` file can be a KiCad BOM export, recognized by its `Reference` column and read with `encoding/csv` (quoted fields with commas). Groups such as `R1,R2,R5` and ranges such as `R4-R6` are expanded (`parts.ExpandRefs`, in fides BOMs too); `Qty` is checked; `DNP` rows are left out and listed in the report, and a DNP part in the netlist is a warning. Column names map to fields through aliases (`MPN`, `Manufacturer Part Number`, `Mfr`, `Designator`, …); other columns become fields of the same name, so KiCad symbol fields can carry `class`, `tags`, `tmax`, …. The rows are converted to the fides format and resolved by `golib/csv.ReadTypedFields`, with the provenance pointing to the KiCad file.<br>• **Part-number mapping**: a BOM row without a type gets the type that lists its `mpn` in its `mpns` field or is named after it, else the first type whose `footprints` pattern (`*`, `?`) matches its footprint. Part numbers without a type are one warning. The report shows manufacturer and part number, and notes where each type came from.<br>• **Package and technology mapping**: `parts.ParseFootprint` reads KiCad's footprint names for the FIDES package of semiconductors and ICs (SOT, SOD, SMA/B/C, MELF, DO, TO with DPAK/D2PAK, and SOIC, TSSOP, MSOP, SSOP, QFN, DFN, LQFP, TQFP, DIP and BGA with their pin counts; every name tested against the fides package table), chip sizes, connector contacts and technology tags (`cer`, `alu`, `tant`, `thick`, `melf`, `network`, `pot`, `led`, `tht`). They only fill in what the parts files leave out, a technology tag only if the component has none of its class. A component without a class gets one from its reference prefix.<br>• `testdata/probe.kicad.csv`, the probe BOM as a KiCad export, gives the same FIT as `probe.bom.csv` (`TestKiCadBOM`). |
| M3 | Advanced device stresses | Device-specific vectors, subcircuit supply currents, Vpmax, configurable per-class derating rules. **Done:**<br>• **Subcircuit supply currents**: the deck comments out each subcircuit instance of the netlist file and writes it again with a zero-volt source in series with each pin (`spice.PinCurrent`). Subcircuits get `i:<port>` per pin, `i` (the largest) and P = Σ V·I over the pins, which gives ICs their self-heating; `p` in the BOM overrides it. Instances in `.include` files are not measured.<br>• **Device-specific vectors**: bipolar transistors get `vcb` = vce − vbe and `veb`, the reverse emitter-base voltage (by the NPN or PNP model type). With `vgs` of MOSFETs and JFETs, they are checked against the new ratings `vcbmax`, `vebmax` and `vgsmax`.<br>• **Vpmax**: V/Vpmax compares the working voltage with the transient rating (with M5, the peak).<br>• **Derating rules**: `--derating FILE`, a CSV of class, tags, rating and limit. Each ratio takes the most specific matching rule (most tags, then class, then rating), 80 % where none applies; above 100 % is always an overstress. The report lists the rules with their file and line, and warnings give the limit. `testdata/derating.csv` is an example.<br>• Found on the way: `fides.Package.Rtha` loses the pin count of IC packages (`NewPackage` keeps it apart from the name), which gives an infinite Rth; fixed in fides v0.1.1, where `Rtha` uses the package's own pin count and table values. With its measured 36 mW, the probe's X1 has a 4.96 K rise: 38.7 FIT instead of 28.4, and the validation reference follows (the probe total is 74.9 FIT). |
| M4 | DC sweep | Stress versus operating condition, max/average stress. **Done:**<br>• `--sweep "SOURCE START STOP STEP"`: ngspice's `dc` of a voltage or current source of the netlist instead of `op` (`spice.Sweep`, `DeckOptions.Sweep`, `spice.ReadSweep`), at the nominal temperature and at every phase temperature (M6). Verified with ngspice 43: the plot is `DC transfer characteristic`, and device vectors and pin currents are saved at every point. `spice.Deck` now takes its options as `DeckOptions`.<br>• The stresses are derived at every point. Their **average** (`stress.MeanAll`) goes to the FIT and the tables: the operating condition spread evenly over the range. The derating check takes the **largest** ratio of each kind over the points (`derating.Worst`), and its warnings name the point.<br>• Report: a DC sweep section with each component's range of V and I and its mean and largest P, with the point of the largest; CSV columns `v_min` … `p_max`.<br>• Sample circuit with the supply swept from 10.8 V to 13.2 V: 75 FIT (74.9 at 12 V), but RC reaches 86 % of its power rating at 13.2 V, and 103 % of its rating derated for 85 °C. |
| M5 | Transient analysis | Time-domain V/I/P, peaks, RMS, averages, stress envelopes (logb/spice already handles transient raw files). **Done:**<br>• `--tran "TSTEP TSTOP [TSTART [TMAX]]"`: ngspice's `tran` instead of `op` (`spice.Tran`, `spice.ReadTran`), at the nominal temperature and at every phase temperature (M6); results before TSTART are not recorded, which leaves out the start-up. `spice.Analysis` is now what a deck runs instead of the operating point, a `*Sweep` or a `*Tran` (`DeckOptions.Analysis`, `Runner.Analysis`); `--sweep` and `--tran` exclude each other. Verified with ngspice 43: the plot is `Transient Analysis`, device vectors and pin currents are saved at every time point, and so is the capacitor current `@c[i]` (0 at the operating point), which capacitors now get as `i` in a transient.<br>• `stress.TimeStats`: of each quantity over time its minimum and maximum with their times, and its mean and RMS value, integrated with the quantity linear between the time points.<br>• **FIT**: each stress at its **effective value** (`stress.Effective`): the mean power, and of the others the RMS value with the sign of the mean. Both are the operating point's values for a circuit that does not change (tested: the probe's FIT with `--tran` is that of `.op`).<br>• **Derating** (`stress.Extremes`): the **peak voltages** (maximum and minimum), the **RMS currents** and the **mean power**, as continuous current and power ratings are thermal; each ratio says which it takes ("with the peak at t = 1.25 ms", "with the mean power"). `derating.Worst` keeps the ratios' `At` without labels.<br>• Report: a Transient section with each component's peak V, I and P with their times, the RMS V and I and the mean P; CSV columns `v_peak` … `p_peak_time`, and `v_min` … `p_max` over the transient. The warnings of the stresses over the points of a sweep or transient are at most three per element.<br>• Probe with a 1 kHz, 1 V ripple on its 12 V supply: 75 FIT (74.9 without); RC at 86 % of its power rating derated for 85 °C, with the mean power. |
| M6 | Mission profiles | One `.OP` per mission phase at the phase temperature (`.temp`), mission-weighted reliability (`reliability/mission`). **Done:**<br>• One `.OP` per distinct ambient temperature of the operating phases, set with `option temp` in the control block, which overrides a `.temp` card of the netlist (`spice.Runner.OPAt`). A phase at the nominal temperature reuses the nominal run; `-k DIR` keeps each run in `DIR/<T>C`.<br>• `rel.FITPhases`: every FIDES model sums per-phase terms weighted by duration/Ttotal, times factors that do not depend on the phase, so each phase's contribution is calculated with its own stresses and they add up exactly (tested against `fides.FIT`). Phases that are not operating use the nominal stresses, which FIDES does not use there. The mission weighting is FIDES's own; `reliability/mission` (time series) is not needed for it.<br>• Derating at each phase temperature with temperature-derated power ratings (`derating.CheckAt`: linear from `trated`, default 70 °C for R and 25 °C for other classes, to 0 at `tmax`; an ambient temperature at or above `tmax` is an overstress). `--hot TEMP` adds a derating-only temperature.<br>• Report: each phase's share of the FIT; each component's FIT with the nominal stresses for comparison, with a warning above 10 %; the largest stress ratios over the temperatures. CSV columns `fit_nominal`, `*_ratio_max`, `*_ratio_max_temp`, `derating_max`. A failed simulation is a warning, and its phases use the nominal stresses.<br>• Sample circuit: 64.6 FIT, against 64.9 with the nominal stresses (M1 −6.2 %); RC at 85 % of its power rating derated for 85 °C. |
| M7 | Thermal coupling | Temperature from power and thermal resistances, later thermal networks (`reliability/thermal`: Foster/Cauer). **Done** (steady state):<br>• `--thermal FILE`: a CSV of thermal resistances (`from, to, rth` in K/W) between component references, passive nodes (board, heat sink, air) and `amb`, the phase ambient (`thermal.Load`). The network is solved once for its transfer impedances (conductance matrix inverted by Gauss-Jordan); every node needs a path to `amb`. A node that looks like a reference but is no component is a warning.<br>• **Electro-thermal iteration** (`simulateThermal`): at every simulated temperature, the network is solved for the power of the components (`p`) and ngspice runs again with each R, C, L, D, Q, M and J instance of the netlist file at its temperature (`dtemp`, verified with ngspice 43; `DeckOptions.Dtemp`, `Runner.Dtemp`), until no temperature changes by more than 0.01 K, at most 20 simulations (else a thermal-runaway warning). Instances with their own `temp`/`dtemp`, in included files or in subcircuits keep the simulation temperature (a note).<br>• **FIDES**: each network component gets `ta`, its rise from the other components, and `rel` raises the ambient and Tmax of the operating phases by it (`localAmbient`), for every class; diodes, transistors and ICs get `t`, their own rise in the network (p·Z), instead of the package Rth, unless the parts files give `t`.<br>• **Derating**: at the local ambient (power derating, tmax); a temperature ambient + ta + t at or above tmax is an overstress.<br>• Report: a Thermal network section (the resistances; each node's power, own rise, rise from the others, and temperature at every simulated temperature); CSV columns `thermal_rise`, `thermal_coupled`.<br>• Sample circuit on a board 25 K/W above the ambient (`testdata/thermal.csv`): 94.7 FIT instead of 74.9, mostly X1 (38.7 → 54.1, 10.5 K above the ambient); RC at 92 % of its power rating derated for 85 °C.<br>• Left for later: transient thermal networks (Foster/Cauer), temperature cycling (`tdelta`) from self-heating. |
| M8 | Project configuration | Project directory and `fitcalc.toml` with circuit, metadata, mission and output settings. **Done:**<br>• `fitcalc.toml` (TOML, `github.com/BurntSushi/toml` v1.5.0, no dependencies): the long flags as keys, `netlist` required, `parts` a list with the BOM first, `hot` a number; unknown keys are errors. Paths are relative to the project file.<br>• `fitcalc DIR` reads `DIR/fitcalc.toml`, `fitcalc file.toml` that file, and `fitcalc` without an argument `./fitcalc.toml`. Flags on the command line override the file (`-p` replaces its parts list); errors in values from the file name it ("bad tran in dir/fitcalc.toml").<br>• The project file is an input of the report (role `project`, SHA-256), and the header names it.<br>• `testdata/fitcalc.toml` is the sample circuit's project: `fitcalc testdata` writes `testdata/probe.md`. |
| M9 | Web UI | Browser front end built on the same core Go engine. **Deferred permanently** (2026-09-14): fitcalc stays a command line tool; the project file (M8) and the tutorial (`doc/tutorial.md`) cover its use. |
| M10 | Statistical / Monte Carlo analysis | Parameter variation and statistical stress/reliability distributions (`reliability/stat`, ideas from `golib/spice` `Context.Pick`). **Done:**<br>• `--mc N` (at most 10000), `--vary "NAME=TOL%, …"`, `--seed N`; project keys `mc`, `vary`, `seed`. The varied elements are the R, C and L components with a `tolerance` (%) in the parts files, and the elements of `--vary`, which win (any element with a numeric value, such as a supply). Elements of included files do not vary (a warning).<br>• Each value is drawn from a normal distribution with σ = tolerance/3, truncated at ±tolerance, from a PCG generator seeded with the seed and the number of the run, so the runs do not depend on their order.<br>• `DeckOptions.Values`, `Runner.Values`: the deck writes the element again with the value replaced (`withValue`: the token after the nodes, or the key=value parameter, whose SPICE value is the element's), together with `dtemp`.<br>• Each run is a board: the simulations at the nominal and the corner temperatures (with sweep, transient and thermal network), in parallel over the processor cores; then, one after the other (the fides library logs through the `log` package), `rel.FITPhases` and the derating checks at every temperature. `phaseComponents` is shared with the nominal analysis.<br>• Report: a Monte Carlo section (the varied elements; the total FIT with mean, σ, 5 % and 95 %; per component its FIT over the runs, its largest ratios and the share of runs above a limit and overstressed), a summary line, a warning per component above a limit in some runs; CSV columns `fit_mc_mean` … `mc_overstress_share`. The nominal analysis stays the report's.<br>• Sample circuit, 40 runs with V1 ±10 %, RC and R3 ±5 %: about 1 s; total FIT 73.7 to 76.9 (5–95 %); RC above its derating limit in 85 % of the runs and overstressed in 2.5 %.<br>• Left for later: model parameters (BF, VTO), other distributions, statistical reliability beyond the FIT spread (`reliability/stat`). |

## 20. Future architecture

Several reliability models should be able to consume the same normalized stress representation:

**SPICE → normalized stress → FIDES / physics-of-failure (`reliability/pof`) / other models →
reports / UI**

This makes fitcalc useful beyond FIDES while keeping the initial project's scope narrow.

## 21. Open points and risks

- **ngspice device vector names**: verified with ngspice 47 (section 9). Other ngspice versions may
  differ, so the ngspice version is recorded in the report and the probe fixture is re-run on
  upgrades.
- **Temperature dependence of stresses**: resolved in M6 (one `.OP` per phase temperature). The
  result is only as good as the temperature behaviour of the SPICE models. Self-heating and
  mutual heating are simulated with a thermal network since M7 (steady-state only).
- **Publishing**: resolved. fitcalc builds from published public modules only (section 13); the
  FIDES code is `rveen/fides` since v0.1.0 (D11).
- **`electronics.Value` compatibility**: resolved by adding `SpiceValue` instead of changing
  `Value`.
- **KiCad netlist variations** between KiCad versions (name prefixing, `.include` of model
  libraries): covered by fixtures exported from the KiCad version in use.

## 22. Definition of done for milestone 1

- A clean Go build produces a single `fitcalc` executable.
- A known SPICE test circuit is analyzed without manual intervention.
- ngspice runs in batch mode, and failures are reported clearly.
- All intended M1 element types are identified and mapped to FIDES classes, and simulation-only
  elements are listed.
- DC voltages, currents and power are correctly extracted or derived, each with its source.
- FIDES receives the correct component and mission information, and semiconductor self-heating is
  taken into account.
- FIT values match the reference calculations, and the `cmd/fides` regression baseline is
  unchanged except for the intended fixes.
- The derating check flags overstress.
- Markdown and CSV reports are generated.
- Missing metadata and assumptions are visible in the report.
- Automated tests cover the core pipeline (unit tests always, integration tests when ngspice is
  present).

## 23. Project principle

Keep fitcalc small, deterministic and inspectable. The value of the tool should come from
connecting existing engineering tools cleanly and making the reliability calculation traceable,
rather than from building a large software platform.
