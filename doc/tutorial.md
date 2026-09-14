# fitcalc tutorial

This tutorial takes a small circuit from its first netlist to a complete
reliability analysis: failure rates for a mission profile, derating checks,
the supply range, a PWM transient, the heat on the board and the spread of the
component values. Each step adds one thing, and every command and output shown
here comes from the example project in [`doc/tutorial`](tutorial), so you can
run them yourself.

The [README](../README.md) is the reference for everything shown here; the
sections below link to it where they only touch a subject.

## Contents

1. [Before you start](#1-before-you-start)
2. [The example circuit](#2-the-example-circuit)
3. [The mission profile](#3-the-mission-profile)
4. [The parts](#4-the-parts)
5. [The first analysis](#5-the-first-analysis)
6. [Fixing an overstress](#6-fixing-an-overstress)
7. [Derating rules](#7-derating-rules)
8. [A project file](#8-a-project-file)
9. [The supply range: a DC sweep](#9-the-supply-range-a-dc-sweep)
10. [A blinking LED: a transient analysis](#10-a-blinking-led-a-transient-analysis)
11. [Heat on the board: a thermal network](#11-heat-on-the-board-a-thermal-network)
12. [Tolerances: Monte Carlo](#12-tolerances-monte-carlo)
13. [CSV reports](#13-csv-reports)
14. [Everyday use](#14-everyday-use)

## 1. Before you start

You need fitcalc (see [Installation](../README.md#installation)) and ngspice in
your `PATH`. The outputs in this tutorial come from ngspice 43; other versions
may give slightly different last digits.

All commands run in the example directory:

```sh
cd doc/tutorial
```

| File | What it is |
|---|---|
| `sensor.net` | the circuit: a SPICE netlist |
| `models/opamp.lib` | an op-amp model, included by the netlist |
| `mission.csv` | the mission profile: where and how long the module works |
| `bom-v1.csv`, `bom.csv` | the bill of materials: a first draft, and the corrected one |
| `parts.csv` | the part types, with their ratings |
| `derating.csv` | derating rules (step 7) |
| `fitcalc.toml` | the project file (step 8) |
| `thermal.csv` | a thermal network of the board (step 11) |

A mission profile is part of every analysis in this tutorial. Without one,
fitcalc still checks the stresses against the ratings, but calculates no failure
rates; that is rarely what you want.

## 2. The example circuit

The circuit is a small module for a car: it takes the battery voltage, makes a
5 V rail for a microcontroller, amplifies a sensor signal and drives a status
LED.

```spice
Sensor interface: 12 V supply, 5 V rail, sensor amplifier and LED

* Battery and input protection: reverse-polarity diode and bulk capacitor
V1 bat 0 13.5
D1 bat vin DRECT
C1 vin 0 47u

* 5 V rail: zener reference and emitter follower
R1 vin z 2.2k
DZ1 0 z DZ56
Q1 vin z v5 QPASS
C2 v5 0 10u
* The microcontroller on the 5 V rail, as a current
IMCU v5 0 20m

* Sensor amplifier: a 0.5 V sensor signal, amplified 4.9 times
VS sens 0 0.5
R4 sens inp 10k
C3 inp 0 10n
XU1 inp fb v5 0 out opamp
R5 fb 0 10k
R6 out fb 39k

* Status LED, switched by a MOSFET from a logic output: on at the operating
* point (DC 5), blinking at 1 kHz with a duty cycle of 50 % in a transient
VG g 0 DC 5 PULSE(0 5 0 1u 1u 0.5m 1m)
R7 vin led 1k
D2 led k DLED
M1 k g 0 0 NMOS1

.include "models/opamp.lib"
.model DRECT D(IS=1n N=1.8 RS=0.05)
...
.end
```

What fitcalc makes of it:

- **Components** are the elements that are parts on the board: R, C, L, D, Q, M,
  J and subcircuit instances such as `XU1`. They get stresses from the
  simulation and, with parts files, a failure rate.
- **Sources** (`V1`, `IMCU`, `VS`, `VG`) are simulation-only: the battery, the
  microcontroller's current, the sensor and the logic output are outside this
  circuit. The report lists them under *Not simulated or not rated*.
- **Subcircuit instances** are one component each: `XU1`, the op-amp, is `U1`
  in the BOM. fitcalc measures the current into each of its pins, so the report
  has its power, 4 mW here.
- The netlist has no analysis card: fitcalc runs its own analyses. If a netlist
  has `.op`, `.tran` or `.control` blocks, fitcalc comments them out and says so
  in a warning. Your netlist file is never changed.

Nothing needs to be simulated by hand before fitcalc: it runs ngspice itself.

## 3. The mission profile

The failure rate of a part depends on how it is used: how hot it is, how often
it heats up and cools down, how much it is shaken, for how long. A FIDES mission
profile describes that as phases. `mission.csv` is the automotive example of the
FIDES guide, one year of a car:

| Phase | Duration | Operating | Ambient | What it is |
|---|---|---|---|---|
| off-day | 720 h | no | 14 °C | parked in the day |
| off-night | 7532 h | no | 14 °C | parked at night |
| start-night | 117 h | yes | 32 °C | short trips, cold start |
| start-day | 58 h | yes | 32 °C | short trips |
| full-op | 201 h | yes | 85 °C | long trips, hot engine compartment |
| motorway | 131 h | yes | 60 °C | motorway driving |

The file has more columns: temperature cycling (`tdelta`, `ncycles`, `tcycle`,
`tmax`), humidity (`rh`), vibration (`grms`), pollution, protection (`ip`) and the
application factor (`pi_app`). See [Mission profile](../README.md#mission-profile)
for all of them.

Two things follow for fitcalc:

- The FIT is the sum of each phase's contribution, weighted by its duration.
  Phases that are not operating still count: the parts age in a parked car
  too, from temperature cycles and humidity.
- fitcalc simulates the circuit at the ambient temperature of each operating
  phase (32, 60 and 85 °C), besides the nominal 27 °C, so that each phase uses
  its own stresses, and checks the derating at each of these temperatures.

## 4. The parts

The netlist says how the circuit works; it does not say which parts are used.
fitcalc takes that from parts files: CSV files with one row per reference or per
part type.

`bom-v1.csv` is the first draft of the bill of materials:

```csv
name, type,       value, block
J1,   TB2,        ,      supply
D1,   S1M,        ,      supply
C1,   C_ALU47U35, 47u,   supply
R1,   R0603,      2.2k,  supply
...
R7,   R0603,      1k,    led
D2,   LED0603,    ,      led
M1,   2N7002,     ,      led
```

Each row names a type from `parts.csv`, which holds what the datasheets say:

```csv
name,       class, tags,      package, npins, tolerance, tmax, vmax, pmax, imax, vgsmax, vcbmax, vebmax, rth, description
R0603,      R,     thick,     0603,    ,      1,         155,  75,   0.1,  ,     ,       ,       ,       ,    thick film resistor 0603
C_ALU47U35, C,     alu,       ,        ,      20,        105,  35,   ,     ,     ,       ,       ,       ,    aluminium electrolytic 47 µF 35 V
BCP56,      Q,     ,          SOT223,  ,      ,          150,  80,   1.5,  1,    ,       100,    5,      ,    NPN medium power transistor
LED0603,    D,     led,       CHIP,    ,      ,          100,  5,    ,     0.02, ,       ,       ,       500, red chip LED 0603
...
```

- **class** and **tags** select the FIDES model: `R thick` is a thick film
  resistor, `C alu` an aluminium electrolytic, `D led` an LED, `Q mos` a MOSFET.
  Without a class, fitcalc takes it from the element (an `R` is a resistor, a
  `Q` a bipolar transistor).
- **package** matters for semiconductors and ICs: it gives FIDES the package's
  failure rates and the default thermal resistance for the self-heating.
- **Ratings** (`vmax`, `pmax`, `imax`, `vgsmax`, …) are what the derating checks
  compare the stresses with; `tmax` is needed by most FIDES models.
- **tolerance** (in %) is used by the Monte Carlo analysis (step 12); **rth**
  gives a thermal resistance where the package has no FIDES default.
- **block** groups the components in the report.
- `J1`, the supply connector, is in the BOM but not in the netlist: it gets a
  FIT from its type alone.

The BOM is the first parts file (`-p`); the others hold the types. See
[Parts files](../README.md#parts-files) for all fields, and
[KiCad BOM](../README.md#kicad-bom) for using a BOM exported from KiCad as it is.

## 5. The first analysis

```sh
fitcalc -p bom-v1.csv -p parts.csv -m mission.csv sensor.net
```

fitcalc writes the report to `sensor.md`, next to the netlist, and prints
warnings and a summary on stderr:

```text
level=WARN msg="R7: no FIT: Actual power (0.109446 W) exceeds its Pmax (0.100000 W) R=1000"
level=WARN msg="R7: P/Pmax = 109% (0.1094 W of 0.1 W): overstress"
level=WARN msg="R7: at 85 °C (full-op): P/Pmax = 130% (0.107 W of 0.08235 W): overstress"
level=WARN msg="J1: not in the netlist: no SPICE stress"
fitcalc: 15 components, 105 FIT in total (1 without FIT), 1 overstressed, 0 derating warnings; at 32, 60 and 85 °C: 1 overstressed, 0 derating warnings; report: sensor.md
```

Read the warnings first:

- **R7 is overstressed.** The LED resistor drops 10.5 V at 10.5 mA: 109 mW in an
  0603 resistor rated 0.1 W. At 85 °C it is worse: above 70 °C the power rating
  of a resistor falls linearly to 0 at its `tmax` of 155 °C, to 82 mW at 85 °C,
  and R7 takes 130 % of that.
- **R7 has no FIT.** FIDES does not calculate a failure rate for a part used
  beyond its rating, so the total of 105 FIT leaves R7 out.
- **J1 has no stress**: it is only in the BOM. That is expected for a connector.

The report has, in this order: the inputs with their SHA-256 (which files
exactly gave this result), the analysis and its assumptions, a summary with the
largest contributors and the FIT per block, the components with their stresses
and FIT, the derating checks, the phase temperatures, the FIDES metadata of each
component with the files it comes from, and all warnings.

## 6. Fixing an overstress

R7 needs a larger package. `bom.csv` is the BOM with R7 as a 1206 (0.25 W):

```sh
fitcalc -p bom.csv -p parts.csv -m mission.csv sensor.net
```

```text
level=WARN msg="J1: not in the netlist: no SPICE stress"
fitcalc: 15 components, 106 FIT in total; at 32, 60 and 85 °C: 0 overstressed, 0 derating warnings; report: sensor.md
```

No overstress, and every component has a FIT. The summary of `sensor.md`:

```text
- Total: 106 FIT (MTBF 9.45 × 10⁶ h, 1078 years, assuming constant failure rates)
- With the nominal stresses in every phase: 106 FIT; the stresses at the phase temperatures change the total by 0 %
- Components: 15: 14 in the netlist, 1 only in the parts files; 4 simulation-only elements left out
- Without FIT: 0
- Derating: 0 overstressed, 0 with warnings
- Derating at 32, 60 and 85 °C: 0 overstressed, 0 with warnings
```

| Block | FIT | % |
|---|---|---|
| supply | 35.9 | 33.9 |
| sensor | 35.8 | 33.8 |
| led | 34.1 | 32.3 |

| Ref | Class | FIT | % | Cumulative % |
|---|---|---|---|---|
| D2 | D | 29.9 | 28.3 | 28.3 |
| U1 | U | 29.4 | 27.8 | 56 |
| C2 | C | 9.4 | 8.88 | 64.9 |
| Q1 | Q | 8.74 | 8.25 | 73.2 |
| C1 | C | 8.31 | 7.85 | 81 |

The LED and the op-amp make more than half of the total: that is where a more
reliable design would start (a better LED type, an op-amp in another package, or
fewer parts in the path). The resistors, at about 1.2 FIT each, hardly matter.

The components table shows each stress with the mark of where it comes from:
ˢ from the simulation, ᴰ derived from it, ᴹ from the parts files:

| Ref | Class | Value | V | I | P | T | FIT |
|---|---|---|---|---|---|---|---|
| D1 | D |  | 0 Vᴰ | 34.4 mAˢ | 27.9 mWᴰ | 3.06 °Cᴰ | 4.37 |
| Q1 | Q |  | 7.84 Vᴰ | 20.7 mAˢ | 162 mWᴰ | 13.6 °Cᴰ | 8.74 |
| U1 | U |  | 4.85 Vᴰ | 850 µAˢ | 4 mWᴰ | 0.551 °Cᴰ | 29.4 |
| R7 | R | 1 kΩ | 10.5 Vᴰ | 10.5 mAˢ | 109 mWᴰ |  | 1.17 |
| D2 | D |  | 0 Vᴰ | 10.5 mAˢ | 23 mWᴰ | 11.5 °Cᴰ | 29.9 |

V is the working voltage FIDES uses: the reverse voltage of a diode (0 V for
the forward-biased D1 and D2), V_CE of a transistor, the supply of an IC. T is
the temperature rise of a semiconductor from its own power: P times the thermal
resistance of its package (the SOT223 of Q1), or `rth` from the parts files (the
LED).

The *Temperatures* section shows that simulating each phase at its own
temperature changes nothing here (the total changes by 0 %): the stresses of
this circuit hardly depend on the temperature. A circuit with more
temperature-dependent parts would show it, and fitcalc warns about a component
whose FIT changes by more than 10 %.

## 7. Derating rules

Without rules, fitcalc warns above 80 % of any rating. Projects usually follow a
derating standard with other limits per part kind. `derating.csv`:

```csv
class, tags,  rating, limit, note
*,     ,      *,      80,    everything else
R,     ,      pmax,   60,    resistor power
R,     thick, pmax,   50,    thick film resistor power
C,     cer,   vmax,   60,    ceramic capacitor voltage
C,     alu,   vmax,   80,    aluminium electrolytic voltage
D,     ,      imax,   75,    diode forward current
D,     led,   imax,   60,    LED current
Q,     ,      pmax,   50,    transistor power
Q,     ,      vgsmax, 75,    gate voltage
```

Each ratio takes the most specific rule: a thick film resistor's power takes the
50 % rule, not the 60 % one for all resistors.

```sh
fitcalc -p bom.csv -p parts.csv -m mission.csv -d derating.csv sensor.net
```

```text
level=WARN msg="R7: at 85 °C (full-op): P/Pmax = 52% (0.107 W of 0.2059 W): warning (limit 50 %)"
level=WARN msg="J1: not in the netlist: no SPICE stress"
fitcalc: 15 components, 106 FIT in total; at 32, 60 and 85 °C: 0 overstressed, 1 derating warnings; report: sensor.md
```

R7 is fine at room temperature (44 % of 0.25 W), but in the full-op phase at
85 °C its rating is derated to 0.2059 W, and 0.107 W is 52 % of that: above the
50 % limit. The report gets a *Derating rules* section that lists the rules with
the file and line each comes from. A warning is not an overstress; whether it
needs a change depends on the standard of the project. Here it is worth
remembering for the next steps.

## 8. A project file

The command lines are getting long. A project file keeps the settings with the
circuit; `fitcalc.toml`:

```toml
netlist  = "sensor.net"
parts    = ["bom.csv", "parts.csv"] # the BOM first
mission  = "mission.csv"
derating = "derating.csv"
```

Paths are relative to the project file. In the project directory, the argument
is the directory itself:

```sh
fitcalc .
```

From anywhere else, `fitcalc doc/tutorial`, and without an argument fitcalc
reads the `fitcalc.toml` of the current directory. The result is the one of
step 7, and the report names the project file and lists it among the inputs:

```text
- Project: `fitcalc.toml`
- Netlist: `sensor.net`
```

Flags on the command line add to the project or override it, as the next steps
do. Every long flag can also be a key of the file; see
[Project file](../README.md#project-file).

## 9. The supply range: a DC sweep

The analysis so far used one battery voltage, 13.5 V. A car battery goes from
about 9 V (cranking) to 16 V (charging). A DC sweep of `V1` covers that range:

```sh
fitcalc --sweep "V1 9 16 0.5" .
```

```text
level=WARN msg="R1: at 85 °C (full-op): P/Pmax = 52% (0.04246 W of 0.08235 W) with V1 = 16 V: warning (limit 50 %)"
level=WARN msg="R7: P/Pmax = 67% (0.1671 W of 0.25 W) with V1 = 16 V: warning (limit 50 %)"
level=WARN msg="R7: at 85 °C (full-op): P/Pmax = 80% (0.164 W of 0.2059 W) with V1 = 16 V: warning (limit 50 %)"
level=WARN msg="D2: I/Imax = 65% (0.01294 A of 0.02 A) with V1 = 16 V: warning (limit 60 %)"
level=WARN msg="D2: at 32 °C (start-night, start-day): I/Imax = 65% (0.01293 A of 0.02 A) with V1 = 16 V: warning (limit 60 %)"
level=WARN msg="J1: not in the netlist: no SPICE stress"
fitcalc: 15 components, 105 FIT in total, 0 overstressed, 2 derating warnings; at 32, 60 and 85 °C: 0 overstressed, 3 derating warnings; report: sensor.md
```

With a sweep, fitcalc uses each stress **averaged** over the sweep for the FIT
(the voltage spread evenly over its range), and the **largest** ratio of each
kind for the derating, with the point where it occurs. The FIT hardly changes
(105 instead of 106), but at 16 V three components go above their limits: R7 at
80 % of its derated rating, R1 (the zener resistor) at 52 %, and the LED at 65 %
of its current rating. The *DC sweep* section of the report shows the range of
every component:

| Ref | V | I | P mean | P max | At |
|---|---|---|---|---|---|
| R1 | 2.64 V … 9.59 V | 1.2 mA … 4.36 mA | 19.1 mW | 41.8 mW | V1 = 16 V |
| Q1 | 3.37 V … 10.3 V | 20.7 mA … 22.3 mA | 142 mW | 214 mW | V1 = 16 V |
| R7 | 6.03 V … 12.9 V | 6.03 mA … 12.9 mA | 94.4 mW | 167 mW | V1 = 16 V |
| D2 | 0 V | 6.03 mA … 12.9 mA | 20.8 mW | 28.7 mW | V1 = 16 V |

The LED current follows the battery, because R7 sets it from the battery
voltage. A constant-current LED driver, or a larger R7 with a brighter LED,
would take care of R7 and D2 at once. See [DC sweep](../README.md#dc-sweep).

## 10. A blinking LED: a transient analysis

The status LED does not stay on: the microcontroller blinks it at 1 kHz with a
duty cycle of 50 %. The operating point sees the LED on (`VG` is `DC 5`); a
transient analysis sees the `PULSE` of `VG`:

```sh
fitcalc --tran "10u 5m 1m" .
```

`10u 5m 1m` simulates from 0 to 5 ms in steps of 10 µs and records from 1 ms,
which leaves out the start-up.

```text
level=WARN msg="J1: not in the netlist: no SPICE stress"
fitcalc: 15 components, 106 FIT in total; at 32, 60 and 85 °C: 0 overstressed, 0 derating warnings; report: sensor.md
```

The derating warning of R7 is gone. Over a transient, fitcalc compares the
**peak** voltages with the voltage ratings, but the **RMS** currents and the
**mean** power with the current and power ratings, which are thermal: R7 takes
110 mW when the LED is on, but 55 mW on average. The FIT uses the RMS values and
the mean power. The *Transient* section:

| Ref | V peak | V RMS | I peak | I RMS | P peak | P mean |
|---|---|---|---|---|---|---|
| D1 | 0 V at 1 ms | 0 V | 34.4 mA at 2.501 ms | 29.5 mA | 27.8 mW at 2.501 ms | 23.4 mW |
| C1 | 12.7 V at 1 ms | 12.7 V | 10.4 mA at 2.502 ms | 2.92 mA |  |  |
| R7 | 10.5 V at 1.002 ms | 7.41 V | 10.5 mA at 1.002 ms | 7.41 mA | 110 mW at 1.002 ms | 54.9 mW |
| D2 | 0 V at 1 ms | 0 V | 10.8 mA at 1.001 ms | 7.41 mA | 23.8 mW at 1.001 ms | 11.5 mW |
| M1 | 12.6 V at 3.502 ms | 8.21 V | 10.5 mA at 1.002 ms | 7.41 mA | 554 µW at 1.001 ms | 158 µW |

It also shows what an operating point cannot: C1, the bulk capacitor, carries a
ripple current of 2.92 mA RMS as the LED switches, with 10.4 mA peaks, and M1
sees 12.6 V when it is off. A capacitor with an `imax` in the parts files gets
its RMS current checked against it, as a ripple current rating. See
[Transient analysis](../README.md#transient-analysis).

## 11. Heat on the board: a thermal network

So far every part sat at the ambient temperature of the phase, plus its own
self-heating. On a real board the warm parts heat each other. A thermal network
describes that as thermal resistances; `thermal.csv`:

```csv
from,  to,    rth
Q1,    board, 60
D1,    board, 80
R7,    board, 150
DZ1,   board, 300
U1,    board, 100
D2,    board, 400
R1,    board, 300
board, amb,   20
```

Each warm part connects to the board, and the board to `amb`, the air in the
housing at the ambient of the phase. The numbers are thermal resistances in
K/W, from datasheets, thermal simulation or measurement.

```sh
fitcalc --thermal thermal.csv .
```

```text
level=WARN msg="R7: at 85 °C (full-op): P/Pmax = 56% (0.1062 W of 0.1908 W): warning (limit 50 %)"
level=WARN msg="D2: no FIT: phase full-op: LED D2: junction temperature 102°C exceeds Tmax 100°C"
level=WARN msg="D2: at 85 °C (full-op): temperature 102.3 °C (local ambient temperature (85 °C and 6.76 K from the other components) and a rise of 10.5 K) at or above tmax 100 °C: overstress"
level=WARN msg="J1: not in the netlist: no SPICE stress"
fitcalc: 15 components, 96.2 FIT in total (1 without FIT); at 32, 60 and 85 °C: 1 overstressed, 1 derating warnings; report: sensor.md
```

The board is 7.3 K warmer than the air, and that is enough to push the LED over
the edge: in the full-op phase its junction reaches 102.3 °C, above its `tmax` of
100 °C. FIDES gives it no FIT, and the total of 96.2 FIT is **not** an
improvement: it leaves the LED out. The *Thermal network* section shows every
node:

| Node | Class | P | Own rise | From the others | At 27 °C | At 32 °C | At 60 °C | At 85 °C |
|---|---|---|---|---|---|---|---|---|
| Q1 | Q | 162 mW | 13 K | 4.08 K | 44.05 °C | 49.04 °C | 77 °C | 102 °C |
| BOARD |  |  | 0 K | 7.32 K | 34.32 °C | 39.31 °C | 67.29 °C | 92.26 °C |
| R7 | R | 109 mW | 18.5 K | 5.15 K | 50.61 °C | 55.57 °C | 83.37 °C | 108.2 °C |
| U1 | U | 4.03 mW | 0.483 K | 7.24 K | 34.72 °C | 39.72 °C | 67.69 °C | 92.67 °C |
| D2 | D | 23.5 mW | 9.85 K | 6.85 K | 43.7 °C | 48.76 °C | 77.04 °C | 102.3 °C |

fitcalc also simulates each part at its temperature in the network, and repeats
the simulation until the temperatures agree with the power (three simulations
per temperature here). The fix is the LED again: one rated for 125 °C, less
current, or keeping it away from Q1 and R7. See
[Thermal network](../README.md#thermal-network).

## 12. Tolerances: Monte Carlo

Every value so far was the nominal one. Real resistors and capacitors spread
within their tolerances, and the battery voltage varies. A Monte Carlo analysis
repeats the whole analysis for many boards, each with its own values:

```sh
fitcalc --mc 100 --vary "V1=15%" --seed 1 .
```

The resistors and capacitors vary by the `tolerance` of their types in
`parts.csv`; `--vary` adds the battery, ±15 % around 13.5 V. Each value is drawn
from a normal distribution with σ = tolerance / 3, and `--seed` makes the runs
repeatable.

```text
level=WARN msg="R7: at 85 °C (full-op): P/Pmax = 52% (0.107 W of 0.2059 W): warning (limit 50 %)"
level=WARN msg="R7: Monte Carlo: above a derating limit in 64 % of the runs (largest P/Pmax = 69.9 %)"
level=WARN msg="D2: Monte Carlo: above a derating limit in 2 % of the runs (largest I/Imax = 60.6 %)"
level=WARN msg="J1: not in the netlist: no SPICE stress"
fitcalc: 15 components, 106 FIT in total; at 32, 60 and 85 °C: 0 overstressed, 1 derating warnings; Monte Carlo: 100 runs, total FIT 105 to 108 (5 % to 95 %); report: sensor.md
```

The 100 runs, each at four temperatures, take about a second. The FIT spreads
little, from 105 to 108 FIT, but R7 goes above its derating limit on 64 boards
of 100, and the LED on 2. The *Monte Carlo* section:

| Ref | FIT | FIT mean | FIT 5 % | FIT 95 % | V/Vmax max | P/Pmax max | I/Imax max | … | Above a limit | Overstress |
|---|---|---|---|---|---|---|---|---|---|---|
| C1 | 8.31 | 8.34 | 7.95 | 8.88 | 41.3 % |  |  | … | 0 % | 0 % |
| Q1 | 8.74 | 8.78 | 8.18 | 9.58 | 11.9 % | 25.2 % | 2.07 % | … | 0 % | 0 % |
| R7 | 1.17 | 1.17 | 1.17 | 1.17 | 6.06 % | 69.9 % |  | … | **64 %** | 0 % |
| D2 | 29.9 | 29.9 | 29.9 | 29.9 | 0 % |  | 60.6 % | … | **2 %** | 0 % |

The nominal analysis stays the report's; the Monte Carlo section adds the
spread. See [Monte Carlo](../README.md#monte-carlo).

## 13. CSV reports

The Markdown report is for reading and for the audit trail. For a spreadsheet,
a script or a comparison between two design versions, write CSV:

```sh
fitcalc -f csv .
```

This writes `sensor.csv`: one row per component with its stresses and their
sources, the FIT, the stress ratios and derating results (nominal and at the
phase temperatures), and the columns of the sweep, transient, thermal network
and Monte Carlo analyses when they run. The header starts with:

```text
ref,element,class,tags,package,value,v,v_source,i,i_source,p,p_source,t,t_source,fit,fit_error,v_ratio,p_ratio,i_ratio,derating,warnings,...
```

See [CSV](../README.md#csv--f-csv) for all columns.

## 14. Everyday use

- **Keep the ngspice files** with `-k work`: the generated deck (`fitcalc.cir`),
  the ngspice log and the raw results, and those of the other temperatures in
  `work/32C`, `work/85C`, …. That is where to look when a simulation fails.
- **See everything** with `-v`: the configuration, the parsed netlist, and each
  component's class, tags, stresses with their source, and FIT.
- **A design temperature** beyond the mission: `--hot 105` also checks the
  derating at 105 °C.
- **Exit status**: 0 when the report was written (warnings or not), 1 for an
  input error, 2 when ngspice failed; usable in scripts and CI.
- **Settings for good**: every flag used in this tutorial can go into
  `fitcalc.toml`, for example

  ```toml
  netlist  = "sensor.net"
  parts    = ["bom.csv", "parts.csv"]
  mission  = "mission.csv"
  derating = "derating.csv"
  thermal  = "thermal.csv"
  sweep    = "V1 9 16 0.5"
  hot      = 105
  ```

  so that `fitcalc .` repeats the whole analysis, and the report records the
  project file with its SHA-256.

For everything else, the [README](../README.md) is the reference:
[the netlist](../README.md#the-netlist),
[parts files](../README.md#parts-files),
[stresses](../README.md#stresses),
[derating](../README.md#derating-check),
[reports](../README.md#reports) and
[messages and troubleshooting](../README.md#messages-and-troubleshooting).
