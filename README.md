# fitcalc

fitcalc computes the failure rate of an electronic circuit with the FIDES 2022
method, from the circuit's SPICE netlist. It runs a DC operating point analysis
with ngspice to get each component's working voltage, current and power, merges
them with component metadata (ratings, packages, technology) from CSV files, and
writes an auditable report with:

- the failure rate (FIT, failures in 10⁹ hours) of every component and of the
  whole circuit, for a given mission profile;
- a derating check of every stress against its rating;
- the origin of every value (simulation, derived, parts files or default) and the
  SHA-256 of every input file.

The FIDES 2022 models are those of [github.com/rveen/fides](https://github.com/rveen/fides),
which is validated against an independent calculation from the FIDES Guide 2022
Edition A.

## Contents

- [Installation](#installation)
- [Quick start](#quick-start)
- [Command line](#command-line)
- [How it works](#how-it-works)
- [The netlist](#the-netlist)
- [Parts files](#parts-files)
- [Mission profile](#mission-profile)
- [Stresses](#stresses)
- [Derating check](#derating-check)
- [Mission phase temperatures](#mission-phase-temperatures)
- [DC sweep](#dc-sweep)
- [Transient analysis](#transient-analysis)
- [Thermal network](#thermal-network)
- [Monte Carlo](#monte-carlo)
- [Reports](#reports)
- [Messages and troubleshooting](#messages-and-troubleshooting)
- [Limitations](#limitations)
- [Validation](#validation)
- [License](#license)

## Installation

fitcalc needs Go 1.25 or later to build and [ngspice](https://ngspice.sourceforge.io/)
to run. It is tested with ngspice 47.

    go install github.com/rveen/fitcalc/cmd/fitcalc@latest

ngspice is usually available as a package (`dnf install ngspice`,
`apt install ngspice`, `brew install ngspice`). fitcalc looks for it in this
order: the `--ngspice` flag, the `NGSPICE` environment variable, then `PATH`.

    fitcalc --version

prints the versions of fitcalc, of the fides library it was built with, and of Go.

## Quick start

For a step-by-step introduction with an example project, see the
[tutorial](doc/tutorial.md).

The repository has a sample circuit in `testdata`. From the repository root:

    fitcalc -p testdata/probe.bom.csv -p testdata/parts.db.csv \
            -m testdata/mission.csv testdata/probe.net

or, with the [project file](#project-file) `testdata/fitcalc.toml` that holds
these settings:

    fitcalc testdata

This simulates `probe.net`, merges the parts data, computes the FIT for the
mission profile and writes the report to `testdata/probe.md`. It simulates the
circuit at 27 °C and at the temperature of each operating phase of the mission
(see [Mission phase temperatures](#mission-phase-temperatures)). On stderr it
prints warnings and a summary:

    level=WARN msg="testdata/probe.net: analysis and output cards commented out: .op (line 29)"
    level=WARN msg="RC: at 85 °C (full-op): P/Pmax = 85% (0.07025 W of 0.08235 W): warning (limit 80 %)"
    level=WARN msg="J9: not in the netlist: no SPICE stress"
    fitcalc: 18 components, 74.9 FIT in total; at 32, 60 and 85 °C: 0 overstressed, 1 derating warnings; report: testdata/probe.md

Other common uses:

    # Stresses and derating only, no FIT (no mission profile)
    fitcalc -p bom.csv -p db.csv circuit.net

    # Only the simulated stresses, without any metadata
    fitcalc circuit.net

    # CSV report, for a spreadsheet or further processing
    fitcalc -f csv -p bom.csv -p db.csv -m mission.csv circuit.net

    # Report to stdout
    fitcalc -o - -p bom.csv -p db.csv -m mission.csv circuit.net | less

    # Keep the ngspice deck, log and raw file for inspection
    fitcalc -k work -p bom.csv -p db.csv -m mission.csv circuit.net

## Command line

    fitcalc [flags] circuit.net
    fitcalc [flags] [DIR | project.toml]

There is exactly one netlist, or a [project file](#project-file) that names it.
Flags may come before or after it.

| Flag | Meaning |
|---|---|
| `-p`, `--parts FILE` | Parts metadata CSV. Repeatable: the first file is the BOM (fides CSV or [KiCad BOM export](#kicad-bom)), the others hold part types (see [Parts files](#parts-files)). Without parts files fitcalc reports only the simulated stresses. |
| `-m`, `--mission FILE` | FIDES mission profile CSV (see [Mission profile](#mission-profile)). Without it fitcalc reports stresses and derating, but no FIT, and says so in a warning. |
| `-o`, `--output FILE` | Report file. The default is the netlist path with the extension of the format: `dir/circuit.net` gives `dir/circuit.md` (or `dir/circuit.csv`). `-` writes the report to stdout. fitcalc refuses to overwrite one of its input files. |
| `-f`, `--format FORMAT` | `markdown` (default) or `csv`. |
| `--ngspice PATH` | The ngspice executable. Default: `$NGSPICE`, then `ngspice` in `PATH`. |
| `-k`, `--keep DIR` | Keep the generated deck (`fitcalc.cir`), the ngspice log (`ngspice.log`) and the raw results (`op.raw`) in `DIR`, and those of the simulations at other temperatures in subdirectories such as `DIR/85C`. Without it they go to a temporary directory that is removed afterwards. |
| `--hot TEMP` | Also check the derating at this ambient temperature in °C, besides the phase temperatures (see [Mission phase temperatures](#mission-phase-temperatures)): a design temperature beyond the mission, or a derating check without a mission. |
| `-d`, `--derating FILE` | Derating rules CSV (see [Derating rules](#derating-rules)). Without it, the derating limit is 80 % of every rating. |
| `--sweep "SOURCE START STOP STEP"` | DC sweep of a voltage or current source of the netlist instead of the operating point, such as `--sweep "V1 10.8 13.2 0.2"` (see [DC sweep](#dc-sweep)). |
| `--tran "TSTEP TSTOP [TSTART [TMAX]]"` | Transient analysis instead of the operating point, recorded from `TSTART`, such as `--tran "10u 5m 1m"` (see [Transient analysis](#transient-analysis)). Excludes `--sweep`. |
| `--thermal FILE` | Thermal network CSV: the temperature of each component from the power of all, simulated with each element at its temperature (see [Thermal network](#thermal-network)). |
| `--mc N` | Monte Carlo: N more runs of the whole analysis, with element values drawn within their tolerances (see [Monte Carlo](#monte-carlo)); at most 10000. |
| `--vary "NAME=TOL%, …"` | With `--mc`: also vary these elements of the netlist, such as supplies (`V1=5%`), or with another tolerance. |
| `--seed N` | With `--mc`: the seed of the draws (default 0). The same seed gives the same runs. |
| `-v`, `--verbose` | Debug output on stderr: the configuration, the parsed netlist, and every component's class, tags, stresses (with their source) and FIT. |
| `--version` | Print the versions and exit. |
| `-h`, `--help` | Print the usage and exit. |

### Project file

A project keeps the netlist and its settings together, so that the analysis is
repeated with one short command. The project file is TOML, with the long flags as
keys (all but `verbose`):

    # fitcalc.toml
    netlist  = "probe.net"
    parts    = ["probe.bom.csv", "parts.db.csv"] # the BOM first
    mission  = "mission.csv"
    derating = "derating.csv"
    thermal  = "thermal.csv"
    sweep    = "V1 10.8 13.2 0.2"   # or tran = "10u 5m 1m"
    hot      = 70
    ngspice  = "/usr/bin/ngspice"
    keep     = "work"
    output   = "probe.md"           # "-" for stdout
    format   = "markdown"           # or "csv"
    mc       = 200
    vary     = "V1=5%"
    seed     = 1

Only `netlist` is required; an unknown key is an error. Paths are relative to the
project file, as are the default report path (next to the netlist) and `keep`.
fitcalc reads the project file when its argument is one (`fitcalc probe.toml`), or
a directory with a `fitcalc.toml` (`fitcalc testdata`), or, without an argument,
the `fitcalc.toml` of the current directory. Flags on the command line override
the file, with paths relative to the current directory; `-p` replaces the file's
`parts` list. The project file is an input of the report, with its SHA-256, and
the report header names it. `testdata/fitcalc.toml` is the project of the sample
circuit.

### Exit status

| Status | Meaning |
|---|---|
| 0 | The report was written. There may be warnings. |
| 1 | Usage or input error: a bad flag, a missing or unreadable file, a netlist or CSV error, ngspice not found, or a report that would overwrite an input. |
| 2 | The ngspice simulation failed (for example no convergence, or a missing model). |

Warnings never change the exit status. They go to stderr and also into the report.

### Output on stderr

- Warnings, one per line, as `level=WARN msg="…"`.
- With `-v`, debug lines (`level=DEBUG`).
- Errors as `fitcalc: <message>`.
- When the report has been written, a summary line: the number of components, the
  total FIT, how many components have no FIT, the derating results, the derating
  results at the phase temperatures, and where the report is.

## How it works

1. **Parse the netlist.** fitcalc finds the top-level elements, follows `.include`
   files and skips subcircuit bodies (see [The netlist](#the-netlist)).
2. **Read the parts files and the mission profile**, so that errors in them show
   before the simulation.
3. **Simulate.** fitcalc writes a deck derived from the netlist into a temporary
   directory and runs `ngspice -b` on it. The deck asks ngspice for a DC operating
   point (`.op`), or a DC sweep with `--sweep`, or a transient analysis with `--tran`, and saves the node
   voltages and the device currents fitcalc needs. Your netlist is never modified.
4. **Derive stresses.** From the operating point (or each point of the sweep or
   the transient), fitcalc computes each
   component's voltage, current, power and, for semiconductors and ICs, the
   temperature rise (see [Stresses](#stresses)).
5. **Merge.** Netlist elements are matched to BOM rows by reference. The class and
   tags come from the parts files, or else from the SPICE element type.
6. **FIT.** With a mission profile, every component goes through the FIDES 2022
   model of its class and tags.
7. **Derating.** Every stress is compared with its rating.
8. **Phase temperatures.** fitcalc simulates the circuit again at the ambient
   temperature of each operating mission phase. Each phase's share of the FIT
   uses the stresses at its temperature, and the derating is checked there, with
   the power ratings derated for the temperature (see
   [Mission phase temperatures](#mission-phase-temperatures)).
9. **Report** in Markdown or CSV.

### The generated deck

The deck is your netlist with these changes:

- `.include` and `.lib` paths are made absolute, so the deck works from the
  temporary directory.
- Existing `.control … .endc` blocks are removed. Analysis and output cards
  (`.op`, `.tran`, `.ac`, `.dc`, `.save`, `.probe`, …) are commented out, with a
  warning that lists them. A netlist that has only `.op` still gets this
  warning; it is harmless.
- Subcircuit instances of the netlist file are commented out and written again
  after the netlist, with a zero-volt source in series with each pin, so that
  ngspice reports the current into each pin:

      * fitcalc: subcircuit instances, with their pin currents measured
      XU1 fitcalc_xu1_1 fitcalc_xu1_2 fitcalc_xu1_3 fitcalc_xu1_4 fitcalc_xu1_5 opamp
      vfitcalc_xu1_1 /in fitcalc_xu1_1 0
      vfitcalc_xu1_2 /fb fitcalc_xu1_2 0
      …

- Before `.end`, fitcalc adds its own control block, for example:

      * fitcalc: operating point analysis
      .control
      set filetype=binary
      save all
      save @r1[i] @r1[p] @r2[i] @r2[p] @d1[id] @r3[i] @r3[p]
      op
      write op.raw
      quit
      .endc

Use `-k DIR` to see the deck, the ngspice log and the raw file.

## The netlist

Any netlist ngspice accepts can be used, including KiCad's SPICE export. The
first line is the title, as in every SPICE netlist, and is shown in the report.

fitcalc reads only what it needs to identify components. Everything else,
including `.param`, `{expressions}` and models, is left to ngspice:

- `+` continuation lines, `*` comment lines, and `;` and `$` inline comments are
  understood. Names are not case-sensitive.
- `.include` files are followed for top-level elements, and are hashed in the
  report. `.lib` sections are assumed to hold models only.
- `.subckt … .ends` bodies are skipped: a subcircuit instance (`X…`) is one
  component, such as an IC, and its internal elements are not components.

### Which elements are components

| SPICE element | Device | Default FIDES class | Tags added |
|---|---|---|---|
| `R` | resistor | R | |
| `C` | capacitor | C | |
| `L` | inductor | L | |
| `D` | diode | D | |
| `Q` | bipolar transistor | Q | |
| `M` | MOSFET | Q | `mos` |
| `J` | JFET | Q | `jfet` |
| `X` | subcircuit instance | from the parts files, usually U | |
| `V`, `I`, `E`, `F`, `G`, `H`, `B`, `K`, `S`, `W`, `T`, `O`, `U`, `Y` | sources, controlled sources, couplings, switches, lines | not components | |

Sources and the other simulation-only elements are listed in the report and left
out of the FIT, unless they appear in the BOM. SPICE and FIDES letters differ (a
SPICE `J` is a JFET, a FIDES `J` is a connector; a SPICE `X` is a subcircuit, a
FIDES `X` is a crystal), so the default class comes from this table. A `class` in
the parts files always wins. The `mos` and `jfet` tags are added only if the
parts files give no transistor type tag (`mos`, `mosfet`, `jfet`, `igbt`, `gan`,
`gaas`, `triac`, `thyristor`).

### KiCad netlists

KiCad adds the device letter to references that don't start with it: an op-amp
`U1` with a subcircuit model becomes `XU1`, a battery `BT1` becomes `VBT1`. When
an element name is a device letter followed by a reference that is in the BOM,
fitcalc uses that reference. `XU1` is reported as `U1` if the BOM has `U1`.

Sample: `testdata/kicad.net` with `testdata/models/opamp.lib`.

## Parts files

Parts files give what the netlist doesn't have: the FIDES class and tags,
package, ratings, and the part data FIDES needs. They use the CSV format of the
fides library, read with [github.com/rveen/golib/csv](https://github.com/rveen/golib):

- The **first** `-p` file is the **BOM**: one row per component, whose `name` is
  the reference (R1, C4, U1), or a [KiCad BOM export](#kicad-bom). References are
  not case-sensitive and must be unique. A row can stand for a group of
  references: `"R1,R2,R5"`, or a range such as `R4-R6`.
- The **other** `-p` files hold **types**: rows whose `name` is a type name
  (R0603, BC847). A row with a `type` field inherits every field it doesn't set
  from that type. Types can inherit from other types.
- Field names are lower case. Spaces around values are ignored. Lines starting
  with `#` are comments.
- Numbers use SPICE-like suffixes as in BOMs: `10k`, `100n`, `4.7u`, `1M` (mega),
  and `5%` for 5.

A BOM and a type file, from `testdata`:

    # probe.bom.csv
    name, type,     value
    R1,   R0603,    10k
    C1,   C0603X7R, 100n
    Q1,   BC847,
    X1,   DIVIC,
    J9,   TB2,

    # parts.db.csv
    name,     class, tags,    package, npins, tmax, vmax, pmax, imax, dcr,  mpns,    footprints,                       description
    R0603,    R,     thick,   0603,    ,      155,  75,   0.1,  ,     ,     ,        Resistor_SMD:R_0603_1608Metric*,  thick film resistor 0603
    C0603X7R, C,     cer x7r, 0603,    ,      125,  50,   ,     ,     ,     ,        Capacitor_SMD:C_0603_1608Metric*, ceramic capacitor X7R 0603
    BC847,    Q,     ,        SOT23,   ,      150,  45,   0.25, 0.1,  ,     ,        ,                                 NPN transistor
    DIVIC,    U,     analog,  SOIC8,   ,      125,  36,   ,     ,     ,     ,        ,                                 divider IC (the subcircuit DIV)
    TB2,      J,     tht,     ,        2,     105,  300,  ,     10,   ,     1715721, ,                                 2-way terminal block

The report's metadata table names the file and row each component's data came
from, such as `probe.bom.csv (R1), parts.db.csv (R0603)`.

### KiCad BOM

The BOM can also be a CSV export of KiCad's BOM tool (Symbol Fields Table,
Export). fitcalc recognizes it by its `Reference` column and reads:

| Column | Read as |
|---|---|
| `Reference` (or `Ref`, `Refs`, `Designator`) | the references, grouped as KiCad writes them: `R1,R2,R5`, `R4-R6` |
| `Value`, `Footprint`, `Description` | `value`, `footprint`, `description` |
| `Qty` (or `Quantity`) | checked against the number of references |
| `DNP` | any value but empty, `0` or `no`: not mounted, left out of the analysis |
| `MPN` (or `Manufacturer Part Number`, `Part Number`, …) | `mpn`, which selects the type (see below) |
| `Manufacturer` (or `Mfr`) | `manufacturer` |
| any other column | the field of the same name in lower case: KiCad symbol fields `Class`, `Tags`, `Tmax`, … set those fields |

The sample circuit's BOM as a KiCad export, `testdata/probe.kicad.csv`, gives the
same FIT as `probe.bom.csv`:

    fitcalc -p testdata/probe.kicad.csv -p testdata/parts.db.csv \
            -m testdata/mission.csv testdata/probe.net

A fides BOM can have the `mpn`, `manufacturer`, `footprint` and `dnp` columns too.

### Part numbers and footprints

A BOM row without a `type` gets one from its part number or its footprint, in
this order:

1. the type that lists its `mpn` in its `mpns` field (part numbers separated by
   spaces), or whose name is its `mpn`;
2. the first type with a pattern in its `footprints` field that matches its
   `footprint`. Patterns use `*` and `?`; a pattern without a library (`R_0603*`)
   matches the footprint name alone.

Footprint patterns suit generic parts such as resistors and capacitors, whose part
numbers are rarely in the parts files. The report notes where a type came from
("type D1N4148 from the part number 1N4148W"), and a warning lists the part
numbers that match no type.

KiCad's footprint names also give the FIDES data that the parts files leave out:

- the **package** of semiconductors and ICs: `SOT-23` → `SOT23`, `SOD-123` →
  `SOD123`, `D_SMA` → `SMA`, `TO-252` → `DPAK`, `SOIC-8_…` → `SOIC8`,
  `TSSOP-16_…` → `TSSOP16`, `QFN-32-1EP_…` → `QFN32`, `DIP-8_…` → `PDIP8`, and the
  size of chip parts (`R_0603_1608Metric` → `0603`);
- the number of contacts of a connector: `PinHeader_2x05` → `npins` 10;
- the **technology tags** of its class, if it has none of them: `cer` (`C_0603…`),
  `alu` (`CP_Elec…`, `CP_Radial…`), `tant` (tantalum), `thick` (`R_0603…`),
  `melf`, `network` (`R_Array…`), `pot`, `led` (`LED_…`), and `tht` for
  through-hole footprints.

A component that neither the parts files nor the netlist give a class gets one
from its reference prefix: R, C, L, D (LED, with the tag `led`), Q, U (IC), J (P,
CN) for connectors and Y for crystals. The notes of the report's metadata table
say which values come from the footprint or the prefix.

### Fields

| Field | Meaning | Unit |
|---|---|---|
| `name` | reference (BOM) or type name (other files) | |
| `type` | type to inherit fields from | |
| `class` | FIDES class: `R`, `C`, `L`, `D`, `Q`, `U`, `J` (connector), `X` (crystal), `PCB` | |
| `tags` | FIDES tags, space separated (see below) | |
| `value` | nominal value. For R, C and L it is compared with the netlist value | Ω, F, H |
| `tolerance` | tolerance for that comparison | % |
| `package` | package name: `0603`, `SOT23`, `SOD123`, `SOIC8`, `LQFP64`, `QFN32`, … | |
| `npins` | number of pins (ICs if the package name has none) or of contacts (connectors) | |
| `ndevices` | number of devices in one package (arrays, networks, multiple diodes or LEDs) | |
| `vmax` | voltage rating (permanent) | V |
| `vpmax` | transient (peak) voltage rating, checked against the working voltage (V/Vpmax) | V |
| `pmax` | power rating | W |
| `imax` | current rating | A |
| `vgsmax` | gate-source voltage rating of a MOSFET or JFET | V |
| `vcbmax` | collector-base voltage rating of a bipolar transistor (V_CBO) | V |
| `vebmax` | emitter-base voltage rating of a bipolar transistor (V_EBO) | V |
| `tmax` | maximum working temperature. Most FIDES models need it | °C |
| `tmin` | minimum working temperature (informative) | °C |
| `trated` | ambient temperature up to which `pmax` applies; above it the derating check at the phase temperatures derates `pmax` linearly to 0 at `tmax`. Default 70 °C for resistors, 25 °C for other classes | °C |
| `rth` | junction-to-ambient thermal resistance, for the temperature rise of D, Q and U | K/W |
| `rtha` | the same, as in the fides files. `rth` wins if both are set | K/W |
| `dcr` | DC resistance of an inductor, for its power I²·DCR | Ω |
| `tc` | temperature coefficient (passed to fides) | |
| `layers`, `mounts`, `piclass`, `pitechno` | PCB: number of layers, number of mounted component terminations, ΠClass (1–6) and ΠTechnology (FIDES 2022, pp. 174–176) | |
| `block` | functional block. The report adds a FIT-per-block table when blocks are used | |
| `description` | free text | |
| `mpn` | part number. Selects the type of a BOM row that has none | |
| `manufacturer` | manufacturer (informative) | |
| `footprint` | KiCad footprint, `library:name`. Selects the type of a BOM row that has none, and gives the package, contacts and tags the parts files leave out | |
| `dnp` | not mounted: any value but empty, `0` or `no` | |
| `mpns` | in a type: the part numbers it stands for, separated by spaces | |
| `footprints` | in a type: footprint patterns it stands for, with `*` and `?` | |
| `v`, `i`, `p`, `t` | manual stresses, see below | V, A, W, °C |

Other columns are allowed and ignored.

### Classes and tags

The class selects the FIDES model family, and the tags select the model and
table row within it. For example:

| Class | Tags (examples) |
|---|---|
| R | `thick`, thin film (default), `melf`, `ww` (wirewound), `network`, `pot` |
| C | `cer` with `x7r`, `c0g`, … and `flex`; `alu`/`elco` with `solid`; `tant` with `wet`, … |
| L | `power`, `multilayer`, `trafo` (default: low-current wirewound) |
| D | `zener`, `tvs`, `rectifier`, `led` (default: signal diode) |
| Q | `mos`, `jfet`, `igbt`, `thyristor`, `triac` (default: bipolar) |
| U | `digital`, `analog`, `mixed`, `microprocessor`, `dram`, `sram`, `fpga`, `flash`, `opto` |
| J | `pressfit`, `tht` (default: SMD) |
| X | crystals and resonators; `osc` for oscillators |
| PCB | the PCB fields above |

All classes accept `smd` (default) or `tht`, and the placement tags `analog`,
`interface` and `power`. The complete list of tags and package names, and the
component families FIDES 2022 covers but the library doesn't (fuses, relays,
switches, film capacitors, …), is in the
[fides readme](https://github.com/rveen/fides#class-and-tags).

### Components in only one place

- **In the netlist, not in the BOM:** its stresses are reported and the FIT is
  attempted with the SPICE element's default class. Most FIDES models then fail
  for lack of `tmax`, and the component is reported without FIT.
- **In the BOM, not in the netlist** (connectors, the PCB, parts that are not
  simulated): its FIT comes from its metadata alone, and the report flags it with
  "no SPICE stress". The sample BOM has such a connector, J9.
- **Simulation-only elements** (sources, …) not in the BOM are listed in the report
  and are not components.
- **Not mounted** (DNP in the BOM): left out of the analysis and listed in the
  report. If the netlist simulates it anyway, a warning says so.

### Manual stresses: `v`, `i`, `p`, `t`

BOM columns `v`, `i`, `p` and `t` set a component's working voltage, current,
power or temperature rise by hand. They are marked ᴹ in the report. A manual
value replaces the simulated one, with a warning. Quantities derived from it are
not recalculated: a manual `v` doesn't change the simulated `p`. Use them for
parts outside the simulation (a connector's current), or to correct a simulation
you know to be unrepresentative. For a diode, `v` is the reverse voltage.

## Mission profile

The mission profile gives the life cycle as phases, one per row, in the CSV
format of the fides library. From `testdata/mission.csv`:

    phase,       duration,  on,  tamb, rh, tdelta, ncycles, tcycle, tmax, grms, saline_pollution, env_pollution, app_pollution, ip,      pi_app
    off-day,     720,       off, 14,   70, 10,     30,      24,     19,   0.01, low,              moderate,      moderate,      sealed,  3.09
    off-night,   7532,      off, 14,   70, 10,     335,     22.5,   19,   0.01, low,              moderate,      moderate,      sealed,  3.09
    start-night, 117,       on,  32,   50, 22,     670,     0.2,    32,   2,    low,              moderate,      moderate,      sealed,  4.8
    start-day,   58,        on,  32,   60, 18,     1340,    0.04,   32,   2,    low,              moderate,      moderate,      sealed,  4.8
    full-op,     201,       on,  85,   30, 53,     335,     0.6,    85,   1,    low,              moderate,      moderate,      sealed,  4.8
    motorway,    131,       on,  60,   30, 28,     30,      4.4,    60,   2,    low,              moderate,      moderate,      sealed,  4.8

| Column | Meaning | Unit |
|---|---|---|
| `phase` | phase name | |
| `duration` | time spent in the phase per year (the durations usually add up to 8760 h) | h |
| `on` | `on` (or `true`) if the equipment is powered, else `off` | |
| `tamb` | ambient temperature around the equipment | °C |
| `rh` | relative humidity | % |
| `tdelta` | amplitude of the phase's thermal cycles | °C |
| `ncycles` | number of thermal cycles in the phase | |
| `tcycle` | duration of one cycle | h |
| `tmax` | maximum temperature during the cycles | °C |
| `grms` | random vibration | Grms |
| `saline_pollution` | saline pollution: `low`, `moderate` or `high` | |
| `env_pollution` | environmental pollution: `low`, `moderate` or `high` | |
| `app_pollution` | application pollution: `low`, `moderate` or `high` | |
| `ip` | `sealed` or `hermetic` for a sealed enclosure, anything else for non-hermetic | |
| `pi_app` | Πapplication, the application factor of the phase (FIDES guide, p. 113) | |

The FIDES guide explains how to derive these values from a product's use. fitcalc
gives Πapplication directly rather than through the guide's questionnaire.
ΠRuggedizing, ΠPM and ΠProcess are the guide's defaults (1.7, 1.7 for active and
1.6 for passive components, and 4).

A mission file without phases, or with a total duration of 0, is an error.

## Stresses

The DC operating point is simulated at the netlist's temperature: ngspice's
default, 27 °C, unless the netlist has a `.temp` card. These are the nominal
stresses in the tables. With a mission profile, the circuit is also simulated at
the temperature of each operating phase (see
[Mission phase temperatures](#mission-phase-temperatures)).

| Class | Voltage (V) | Current (I) | Power (P) |
|---|---|---|---|
| R | voltage across it | `@r[i]` | V·I |
| C | voltage across it | | |
| L | voltage across it | branch current | I²·`dcr` if `dcr` is set |
| D | reverse voltage max(−Vd, 0), the voltage FIDES uses | `@d[id]` | Vd·Id |
| Q (bipolar) | V_CE | I_C | V_CE·I_C + V_BE·I_B |
| Q (MOSFET, JFET) | V_DS | I_D | V_DS·I_D |
| subcircuit (X) | largest voltage between two of its pins | largest pin current | Σ V·I over its pins |

Powers are always computed from voltages and currents, because ngspice reports
an infinite `@d[p]` for a forward-biased diode. FIDES takes magnitudes, so the
sign of V and I only matters for the reverse voltage of diodes and for polarized
capacitors (see [Derating check](#derating-check)).

**Temperature rise T** for D, Q and U: T = P·Rth, added to the ambient
temperature of each phase by the FIDES model. Rth is `rth` from the parts files,
else `rtha`, else the FIDES default of the package on an FR4 board. Without power
or without a thermal resistance, the component gets a warning and T = 0.

**Device-specific stresses.** Bipolar transistors also get V_CB = V_CE − V_BE
(`vcb`) and the reverse voltage of the emitter-base junction (`veb`: −V_BE for an
NPN, V_BE for a PNP transistor, 0 when forward-biased). MOSFETs and JFETs have
V_GS (`vgs`). Each has its rating: `vcbmax`, `vebmax`, `vgsmax`.

**Subcircuit instances** get the voltage and the current of each pin (`v:<pin>`,
`i:<pin>`, the current flowing into the pin), measured through zero-volt sources
in the deck. Their power is Σ V·I over the pins: what the IC dissipates, whatever
it delivers to its loads. It gives ICs their self-heating. A `p` field in the BOM
overrides it. Instances in `.include` files are not measured.

## Derating check

Every stress that has a matching rating is compared with it, independently of
FIDES:

| Ratio | Stress | Rating |
|---|---|---|
| V/Vmax | the voltage in the table above | `vmax` |
| V/Vpmax | the same voltage | `vpmax` |
| P/Pmax | P | `pmax` |
| I/Imax | \|I\| | `imax` |
| Vgs/Vgsmax | \|V_GS\| of a MOSFET or JFET | `vgsmax` |
| Vcb/Vcbmax | \|V_CB\| of a bipolar transistor | `vcbmax` |
| Veb/Vebmax | reverse emitter-base voltage | `vebmax` |

Above the **derating limit** is a **warning**: 80 % of the rating, unless
[derating rules](#derating-rules) say otherwise. Above 100 % is an
**overstress**, whatever the rules. A reverse voltage on a polarized capacitor
(tags `alu`, `elco`, `tant`, `tantalium`) is also an overstress. Both show in the
derating table, in the summary and on stderr, warnings with their limit:
`P/Pmax = 71% (0.07053 W of 0.1 W): warning (limit 50 %)`. Components without
ratings are listed under the table.

### Derating rules

Derating standards set lower limits by component family: thick film resistors
at half their power rating, tantalum capacitors at half their voltage, and so
on. `--derating FILE` gives them as a CSV file:

    class, tags,  rating, limit, note
    *,     ,      *,      80,    everything else
    R,     thick, pmax,   50,    thick film resistor power
    C,     tant,  vmax,   50,    tantalum capacitor voltage
    Q,     ,      vgsmax, 75,    gate voltage

- `class`: the FIDES class, or `*` (or empty) for any.
- `tags`: tags the component must all have, separated by spaces; empty for any.
- `rating`: `vmax`, `vpmax`, `pmax`, `imax`, `vgsmax`, `vcbmax` or `vebmax`, or
  `*` for any.
- `limit`: the derating limit, in percent of the rating.
- Other columns, such as a note, are ignored; lines starting with `#` are
  comments.

Each ratio of a component takes the **most specific** rule that matches its
class, its tags and the rating: the one with the most tags, then one for its
class over one for any class, then one for the rating over one for any rating,
then the first in the file. Where no rule matches, the limit is 80 %. The report
lists the rules with their file and line. `testdata/derating.csv` is an example,
with limits of the kind derating standards give; check them against the standard
of your project.

The ratings are taken as given: this check is at the nominal temperature. It is
repeated at the phase temperatures, with derated power ratings (see
[Mission phase temperatures](#mission-phase-temperatures)).

## Mission phase temperatures

A circuit's operating point changes with temperature: bias currents, leakage
currents, on-resistances. With a mission profile, fitcalc therefore simulates the
circuit at the ambient temperature of each operating phase, besides the nominal
simulation. Phases at the same temperature share one simulation, and a phase at
the nominal temperature reuses the nominal one. The deck sets `option temp`, which
overrides a `.temp` card of the netlist.

- **FIT.** FIDES sums over the phases, each weighted by its share of the mission,
  and fitcalc calculates each phase's contribution with the stresses at that
  phase's temperature. Phases that are not operating do not depend on the stresses
  in FIDES; they use the nominal ones. The report gives each phase's share of the
  total FIT, and each component's FIT with the nominal stresses in every phase for
  comparison. A component whose FIT differs from it by more than 10 % gets a
  warning: its stresses depend on the temperature.
- **Derating.** At each phase temperature, the stresses are checked against the
  ratings at that ambient temperature. Above the rated temperature `trated`
  (default 70 °C for resistors, as in most resistor datasheets, and 25 °C for other
  classes, as for the total power of semiconductors), the power rating falls
  linearly to 0 at `tmax`. An ambient temperature at or above `tmax` is an
  overstress. Voltage and current ratings are not derated, and components without
  `tmax` keep their power rating. The report shows each component's largest ratios
  over the temperatures, and where they occur.
- `--hot TEMP` adds a temperature to the derating check only: a design temperature
  beyond the mission, or a derating check without a mission.

For the sample circuit the phase temperatures change the total FIT by −0.4 %,
mostly through M1, whose drain current falls with temperature (−6.2 %). Circuits
with power transistors, whose on-resistance rises with temperature, or with
leakage-dominated nodes change more.

This needs simulation models with temperature coefficients. ngspice's resistors
have none unless `tc1`/`tc2` are given, and many vendor models ignore temperature:
for them the stresses are the same at every temperature. The simulations run at
the ambient temperature: the self-heating of semiconductors is added by FIDES as
P·Rth, not simulated, unless a [thermal network](#thermal-network) is given. If the simulation at a temperature fails (for example, no
convergence), the report has a warning, and its phases use the nominal stresses.

## DC sweep

A single operating point shows the stresses at one supply voltage, one load, one
input. `--sweep "SOURCE START STOP STEP"` runs a DC sweep of a voltage or current
source of the netlist instead, as ngspice's `dc` command:

    fitcalc --sweep "V1 10.8 13.2 0.2" -p bom.csv -p db.csv -m mission.csv circuit.net

Values take SPICE suffixes (`100m`, `1.5k`), and a sweep can go down with a
negative step; it has at most 1000 points. The sweep runs at every simulated
temperature, the nominal one and those of the mission phases, and:

- the **FIT** and the stress tables use each stress **averaged** over the sweep:
  the operating condition is taken as spread evenly over its range;
- the **derating** check takes the **largest** ratio of each kind over the sweep,
  and its warnings say where:
  `RC: P/Pmax = 86% (0.0855 W of 0.1 W) with V1 = 13.2 V: warning (limit 80 %)`;
- the report's **DC sweep** section gives each component's range of V and I, and
  its mean and largest P, with the point where P is largest.

For the sample circuit, a sweep of the supply from 10.8 V to 13.2 V hardly
changes the FIT (75 FIT instead of 74.9), but RC takes 85.5 mW at 13.2 V: 86 %
of its rating at 27 °C, and 103 % of its rating derated for the 85 °C phase, an
overstress that the operating point at 12 V does not show.

## Transient analysis

Ripple, switching and pulses do not show at an operating point.
`--tran "TSTEP TSTOP [TSTART [TMAX]]"` runs a transient analysis instead, as
ngspice's `tran` command, with the sources of the netlist (`SIN`, `PULSE`, `PWL`):

    fitcalc --tran "10u 5m 1m" -p bom.csv -p db.csv -m mission.csv circuit.net

Times take SPICE suffixes, with or without `s` (`10u`, `10us`). The results are
recorded from `TSTART` to `TSTOP`: a `TSTART` after the start-up of the circuit
leaves it out, and the recorded time should be a whole number of periods. `TMAX`
bounds ngspice's time step; there are at most 100000 steps from `TSTART` to
`TSTOP`. The transient runs at every simulated temperature, and each stress is
reduced over the recorded time, integrated with the stress linear between the
time points:

- the **FIT** and the stress tables use each stress at its **effective value**:
  the **mean** power, and the **RMS** value of the voltages and currents, with the
  sign of their mean. For a circuit that does not change, these are the values of
  its operating point;
- the **derating** check compares the **peak** voltages (the maximum and the
  minimum) with the voltage ratings, the **RMS** currents with the current ratings
  and the **mean** power with the power rating, since continuous current and power
  ratings are thermal. The warnings say which it took:
  `RC: at 85 °C (full-op): P/Pmax = 86% (0.0705 W of 0.08235 W) with the mean power: warning (limit 80 %)`;
- capacitors also get their current (`@c1[i]`), whose RMS value is checked against
  `imax`, a ripple current rating;
- the report's **Transient** section gives each component's peak V, I and P with
  the time where they are, the RMS V and I, and the mean P; compare the peaks with
  pulse ratings, which fitcalc does not check.

For the sample circuit with a 1 kHz, 1 V ripple on its 12 V supply
(`V1 vcc 0 SIN(12 1 1k)`, `--tran "10u 3m 1m"`), the FIT is 75 instead of 74.9;
RC peaks at 82.9 mW with a mean of 70.8 mW.

## Thermal network

Without a thermal network, every component sits at the ambient of the mission
phase, and FIDES adds only its own heating: P·Rth for diodes, transistors and ICs,
its own model for resistors. On a real board the components heat each other and
the air of the enclosure. `--thermal FILE` describes that as a network of thermal
resistances:

    # K/W
    from,  to,    rth
    Q1,    board, 250
    X1,    board, 100
    R3,    board, 200
    board, amb,   25

Each row is a thermal resistance in K/W (SPICE suffixes allowed) between two nodes:
component references, passive nodes such as `board`, `heatsink` or `air`, and
`amb`, the ambient of the phase. Names are case-insensitive, lines starting with
`#` are comments, and every node needs a path to `amb`. A component's own
resistance to its first node is its junction-to-board (or body-to-board)
resistance. A node that looks like a reference but is not a component gets a
warning: it is taken as a passive node, without power.

fitcalc solves the network for the power of the components (`p`: simulated, or
from the parts files) at every simulated temperature, and:

- **simulates again** with every element of the network at its temperature
  (ngspice's `dtemp` on R, C, L, D, Q, M and J instances of the netlist file),
  until no temperature changes by more than 0.01 K; after 20 simulations it stops
  with a warning about a possible thermal runaway. Temperature coefficients and
  semiconductor models then see their own temperature, and the stresses follow;
- gives **FIDES** each component's **local ambient**, the phase ambient and the
  heating by the other components (`ta`), in every operating phase, and for
  diodes, transistors and ICs their **own rise** in the network (`t`, the power
  times the network's resistance from the node to the ambient) instead of the
  package Rth. A `t` in the parts files still wins;
- checks the **derating** at the local ambient: the power ratings are derated for
  it, and a component whose local ambient, plus its own rise, reaches `tmax` is an
  overstress;
- adds a **Thermal network** section to the report, with the resistances and
  each node's power, own rise, rise from the others, and its temperature at every
  simulated temperature.

A network is steady-state: the power of a sweep or transient is its mean, and
elements of subcircuits and included files keep the simulation temperature. For
the sample circuit on a board 25 K/W above the ambient (`testdata/thermal.csv`),
the board is 6.9 K above the ambient, X1 10.5 K and R3 32.4 K; the total FIT goes
from 74.9 to 94.7, mostly X1's (38.7 to 54.1), and RC reaches 92 % of its power
rating derated for 85 °C.

## Monte Carlo

The nominal analysis takes every value as it is in the netlist. Real parts
spread within their tolerances, and supplies vary. `--mc N` repeats the whole
analysis N times, each run a board with its own values:

    fitcalc --mc 200 --vary "V1=10%" -p bom.csv -p db.csv -m mission.csv circuit.net

- **What varies**: resistors, capacitors and inductors with a `tolerance` (in %)
  in the parts files, and the elements named in `--vary` (any element with a
  numeric value, such as a supply `V1`; it also overrides a tolerance of the
  parts files). Elements of included files and values written as expressions do
  not vary.
- **How**: each value is drawn from a normal distribution around its nominal
  value with σ = tolerance / 3, truncated at the tolerance. The draws depend only
  on `--seed` and the number of the run, so the same seed gives the same runs.
- **Each run** is the whole analysis: the simulations at the nominal and at the
  phase temperatures, with the DC sweep, transient or thermal network if given,
  the FIT per phase and the derating checks. The simulations of the runs use all
  processor cores; a run whose simulation fails is left out, with a warning.

The nominal analysis stays the report's; the **Monte Carlo** section adds the
varied elements, the spread of the total FIT (mean, σ, 5 % and 95 %), and for each
component its FIT over the runs, its largest stress ratios over the runs and the
temperatures, and the share of the runs above a derating limit and in overstress.
A component that goes above a limit in some runs gets a warning:

    RC: Monte Carlo: above a derating limit in 85 % of the runs, overstressed in 2.5 % (largest P/Pmax = 101 %)

For the sample circuit, 40 runs with V1 at ±10 % and RC and R3 at ±5 % take about
a second; the total FIT goes from 73.7 to 76.9 (5 % to 95 %), and RC, at 85 % of
its derated rating nominally, is overstressed in 1 run of 40.

## Reports

### Markdown (default)

Sections, from the sample circuit:

1. **Header**: circuit title, netlist, date, the fitcalc and fides versions, and
   the ngspice version.
2. **Inputs**: every file read (project, netlist, includes, parts files, mission,
   derating rules, thermal network) with its
   SHA-256, so the report can be tied to exactly these inputs.
3. **Analysis**: the analysis, the simulation temperature and the assumptions, and
   the legend of the source marks.
4. **Summary**: total FIT and MTBF, the total with the nominal stresses in every
   phase, component counts, components without FIT, derating counts (nominal and
   at the phase temperatures), warning counts, the largest contributors with their
   share, and, if the parts files use `block`, the FIT per block.

       - **Total:** 74.9 FIT (MTBF 13.4 × 10⁶ h, 1524 years, assuming constant failure rates)
       - **With the nominal stresses in every phase:** 75.2 FIT; the stresses at the phase temperatures change the total by -0.4 %
       - **Components:** 18: 17 in the netlist, 1 only in the parts files; 1 simulation-only element left out

       | Ref | Class | FIT | % | Cumulative % |
       |---|---|---|---|---|
       | X1 | U | 38.7 | 51.7 | 51.7 |
       | L1 | L | 8.59 | 11.5 | 63.1 |

5. **Components**: value, V, I, P, T and FIT of each component. Each value is
   marked with its source: ˢ SPICE result, ᴰ derived, ᴹ parts files, ˀ default.
   Components without FIT show `—` and the reason under Warnings.

       | Ref | Class | Value | V | I | P | T | FIT |
       |---|---|---|---|---|---|---|---|
       | R1 | R | 10 kΩ | 10.9 Vᴰ | 1.09 mAˢ | 11.9 mWᴰ |  | 1.17 |
       | D1 | D |  | 0 Vᴰ | 11.3 mAˢ | 8.1 mWᴰ | 2.73 °Cᴰ | 0.244 |
       | M1 | Q |  | 10.4 Vˢ | 1.57 mAˢ | 16.3 mWᴰ | 7.23 °Cᴰ | 4.39 |

6. **DC sweep** (with `--sweep`): each component's range of V and I, and its mean
   and largest P, with the point where P is largest. **Transient** (with
   `--tran`): each component's peak V, I and P with their times, its RMS V and I,
   and its mean P.
7. **Derating**: the ratios and the result (`ok`, warning, overstress), with
   columns for V/Vpmax, Vgs/Vgsmax, Vcb/Vcbmax and Veb/Vebmax when a component has
   them. With `--derating`, the **derating rules** follow.
8. **Temperatures**: the phases, the temperature of the stresses each one uses and
   its share of the FIT; and per component, its FIT, its FIT with the nominal
   stresses and the change, and the largest stress ratios over the phase
   temperatures (relative to the derated power ratings), with the temperature
   where they occur. With `--thermal`, the **thermal network** follows: its
   resistances, and each node's power, rises and temperatures. With `--mc`, the
   **Monte Carlo** section follows: the varied elements, the spread of the FIT,
   and each component's largest ratios and share of runs above a limit.

       | Phase | On | Tamb | Duration | Stresses at | FIT | % |
       |---|---|---|---|---|---|---|
       | off-night | no | 14 °C | 7532 h | 27 °C (nominal) | 0.698 | 0.932 |
       | full-op | yes | 85 °C | 201 h | 85 °C | 62.5 | 83.5 |

       | Ref | FIT | Nominal FIT | Change | V/Vmax | P/Pmax | I/Imax | Result |
       |---|---|---|---|---|---|---|---|
       | RC | 1.17 | 1.17 | 0 % | 15.8 % at 32 °C | *85.3 %* at 85 °C |  | **warning** |
       | M1 | 4.39 | 4.68 | -6.2 % | 21.4 % at 85 °C | 7.33 % at 85 °C | 0.768 % at 32 °C | ok |

9. **FIDES metadata**: class, tags, package, part (manufacturer and part number),
   ratings, the files and rows they came from, and notes such as "tag mos from the
   SPICE element type" or "type D1N4148 from the part number 1N4148W".
10. **Not simulated or not rated**: simulation-only elements, parts that are only
    in the BOM, and parts not mounted (DNP).
11. **Mission profile**: the phases as FIDES used them.
12. **Warnings**: general warnings, then per component.

### CSV (`-f csv`)

One row per component, with plain numbers in SI units (V, A, W, °C) for a
spreadsheet or a script:

    ref,element,class,tags,package,value,v,v_source,i,i_source,p,p_source,t,t_source,fit,fit_error,v_ratio,p_ratio,i_ratio,derating,warnings,fit_nominal,v_ratio_max,v_ratio_max_temp,p_ratio_max,p_ratio_max_temp,i_ratio_max,i_ratio_max_temp,derating_max,manufacturer,mpn,vp_ratio,vp_ratio_max,vp_ratio_max_temp,vgs_ratio,vgs_ratio_max,vgs_ratio_max_temp,vcb_ratio,vcb_ratio_max,vcb_ratio_max_temp,veb_ratio,veb_ratio_max,veb_ratio_max_temp
    R1,R1,R,thick,0603,10000,10.9091,derived,0.00109091,SPICE,0.0119008,derived,,,1.16537,,0.1455,0.119,,ok,,1.16537,0.1455,32,0.1445,85,,,ok,,,,,,,,,,,,,,

| Column | Content |
|---|---|
| `ref` | reference |
| `element` | netlist element (`XU1` for `U1`), empty for BOM-only parts |
| `class`, `tags`, `package`, `value` | as used for FIDES |
| `v`, `i`, `p`, `t` | stresses |
| `v_source`, … | `SPICE`, `derived`, `metadata` or `default` |
| `fit` | FIT, with the stresses of each phase at its temperature; empty if it could not be calculated |
| `fit_error` | why there is no FIT |
| `v_ratio`, `p_ratio`, `i_ratio` | derating ratios (1 = 100 %) |
| `derating` | `ok`, `warning` or `overstress` |
| `warnings` | the component's warnings |
| `fit_nominal` | FIT with the nominal stresses in every phase; empty unless `fit` is per phase |
| `v_ratio_max`, `p_ratio_max`, `i_ratio_max` | the largest ratio over the phase temperatures (and `--hot`), relative to the derated power rating |
| `v_ratio_max_temp`, … | the temperature of that ratio, °C |
| `derating_max` | the worst derating result over those temperatures |
| `manufacturer`, `mpn` | from the BOM |
| `vp_ratio`, `vgs_ratio`, `vcb_ratio`, `veb_ratio` | the ratios V/Vpmax, Vgs/Vgsmax, Vcb/Vcbmax and Veb/Vebmax, each followed by its `_max` and `_max_temp` over the phase temperatures |
| `v_min`, `v_max`, `i_min`, `i_max`, `p_min`, `p_max` | with `--sweep` or `--tran`: the range of V, I and P over the sweep (`v`, `i`, `p` are their means) or the transient (`v`, `i` are their RMS values, `p` its mean) |
| `v_peak`, `v_peak_time`, `v_rms`, `i_peak`, `i_peak_time`, `i_rms`, `p_peak`, `p_peak_time` | with `--tran`: the value of the largest magnitude of V, I and P with its time in s, and the RMS values of V and I |
| `fit_mc_mean`, `fit_mc_p5`, `fit_mc_p95`, `mc_warning_share`, `mc_overstress_share` | with `--mc`: the mean, 5 % and 95 % FIT over the runs, and the share of the runs (0 to 1) with a derating warning or worse, and with an overstress |
| `thermal_rise`, `thermal_coupled` | with `--thermal`: the component's rise above the ambient in the network at the nominal temperature, and the part of it from the other components, K |

The CSV report has no header, inputs or mission sections. Use the Markdown report
for the audit trail.

## Messages and troubleshooting

| Message | Meaning and fix |
|---|---|
| `analysis and output cards commented out: …` | The netlist's own analyses were disabled, because fitcalc runs its own `.op`. Harmless. |
| `no parts files (-p): no FIDES metadata for any component` | Only stresses are reported. Add a BOM and types. |
| `no mission profile (-m): stresses and derating only, no FIT` | Add `-m mission.csv` for FIT values. |
| `X1: no class: set it in the parts files` | Subcircuits have no default class. Add the reference to the BOM with `class` U (or the right class). |
| `R1: no FIT: Tmax (max temperature of component) not set` | The FIDES model needs `tmax`. Add it to the part or its type. |
| `X1: self-heating not included: no power` | No P for an IC or subcircuit: its instance is in an `.include` file, so its pin currents are not measured. Give `p` in the BOM if it dissipates noticeably. |
| `R3: P/Pmax = 51% (0.1273 W of 0.25 W): warning (limit 50 %)` | The ratio is above the derating limit of its rule (80 % without `--derating`). |
| `derating.csv:12: unknown rating "pmaks"` | A derating rule names a rating fitcalc doesn't know. |
| `D1: self-heating not included: no thermal resistance (set rth in the parts files)` | No `rth`/`rtha`, and the package has no FIDES default (or there is no package). |
| `J9: not in the netlist: no SPICE stress` | A BOM part that is not simulated. Its FIT uses metadata only. |
| `not in the parts files: no FIDES metadata` | A netlist component that is not in the BOM. |
| `value 10k in the netlist, 12k in bom.csv (R5)` | The netlist and the BOM disagree beyond `tolerance`. |
| `class Q from …, but netlist element M1 is a MOSFET (class Q)` | The parts files and the netlist disagree on the class. The parts files win. |
| `v from the parts files overrides the simulated value; …` | A manual stress replaced a simulated one. |
| `fides: …` | A message from the FIDES library, such as an unknown package. |
| `parts: part numbers without a type in the parts files: LM358 (U1)` | These parts get no type from their part number (nor from their footprint). Add a type for them, or their part numbers to a type's `mpns`. |
| `parts: bom.csv: R4: quantity 2 for 1 references` | The BOM's `Qty` does not match its references. |
| `U5: DNP in the BOM, but netlist element XU5 is simulated; …` | The simulated circuit has a part the board does not. Exclude it from the simulation, or check the DNP. |
| `RC: at 85 °C (full-op): P/Pmax = 85% (0.07025 W of 0.08235 W): warning (limit 80 %)` | A derating finding at a phase temperature, against the rating derated for that temperature. Check `trated` and `tmax`, or use a part with more margin. |
| `M1: FIT +25.3 % with the stresses at the phase temperatures (…)` | The component's stresses depend on the temperature. Its FIT already uses the stresses of each phase; check that its SPICE model has the right temperature behaviour. |
| `simulation at 85 °C (full-op) not done, the nominal stresses apply: …` | The simulation at that temperature failed, or ran at another temperature. Rerun with `-k DIR` and read `DIR/85C/ngspice.log`. |
| `ngspice executable not found (use --ngspice or $NGSPICE)` | Install ngspice or give its path. |
| exit status 2, with ngspice log lines | The simulation failed. Rerun with `-k DIR` and read `DIR/ngspice.log` and `DIR/fitcalc.cir`. |

For a closer look at one component, `-v` prints each component's class, tags,
stresses with their source, and FIT.

## Limitations

- DC operating points, DC sweeps of one source, or transient analyses, at the
  nominal temperature and at the phase temperatures. No AC analysis. The points of
  a sweep weigh the same in the FIT; a transient stands for the whole of each
  operating phase, and its effective values (RMS, mean power) for the FIDES
  stress laws, which are not linear. Pulse ratings are not checked.
- The simulations run at the ambient temperature. Without `--thermal`,
  semiconductor self-heating is added by FIDES (P·Rth), not simulated, and
  components do not heat each other. The thermal network is steady-state (no
  thermal time constants), and the temperature cycling of the phases (`tdelta`)
  does not grow with the self-heating.
- Only power ratings are derated with temperature, linearly. Voltage derating
  (tantalum capacitors, for example) is not modelled.
- Subcircuits are single components, whose power is what they take at their
  pins. Pin currents are measured only for instances in the netlist file itself,
  not in `.include` files.
- The derating check has no junction temperature limit yet: that comes with
  thermal coupling (M7).
- Derating thresholds are fixed at 80 % and 100 %.
- FIDES 2022 only. Some families (fuses, relays, switches, film capacitors,
  ASICs, GaN and GaAs parts) have no model in the fides library and are reported
  without FIT. See the [fides readme](https://github.com/rveen/fides).
- FIT values assume a constant failure rate, as FIDES does.

## Validation

fitcalc's results for the probe circuit (`testdata/probe.net`, one element of each
kind) match an independent FIDES 2022 calculation from the guide to within 10⁻⁴.
The fides library's 99 model and table-row cases match to within 10⁻⁹. See
[testdata/validation](testdata/validation/README.md).

    go test ./...

Tests that need ngspice are skipped when it isn't installed.

## License

MIT, see [LICENSE](LICENSE).
