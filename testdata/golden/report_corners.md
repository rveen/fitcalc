# Reliability analysis: Sample circuit

- **Netlist:** `testdata/sample.net`
- **Date:** 2026-09-11 10:00 UTC
- **fitcalc:** v1.0.0
- **FIDES 2022 Edition A:** github.com/rveen/fides v0.1.0
- **Simulator:** ngspice-47

## Inputs

| File | Role | SHA-256 |
|---|---|---|
| `testdata/sample.net` | netlist | `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa` |
| `testdata/probe.bom.csv` | parts (BOM) | `bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb` |
| `testdata/parts.db.csv` | parts | `cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc` |
| `testdata/mission.csv` | mission | `dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd` |

## Analysis

- DC operating point (`.op`), simulated with ngspice-47 at 27 °C for the nominal stresses in the tables, and again at 32 °C (start-night, start-day), 60 °C (motorway) and 85 °C (full-op).
- FIDES 2022 failure rates in FIT (failures in 10⁹ hours) for the mission profile below, with the stresses of each operating phase simulated at its ambient temperature. Phases that are not operating do not depend on the stresses.
- V is the working voltage: the reverse voltage for diodes, V_CE or V_DS for transistors, the largest voltage between two pins for subcircuits.
- T is the temperature rise FIDES uses for diodes, transistors and ICs: P·Rth, with the FIDES default of the package on an FR4 board where the parts files give no rth.
- Derating: a warning above 80 %, an overstress above 100 % of the rating.
- Derating at 32, 60 and 85 °C: with the stresses simulated at that ambient temperature, and the power ratings derated linearly from the rated temperature (`trated`; default 70 °C for resistors, 25 °C for other classes) to 0 at `tmax`.
- A component whose FIT differs by more than 10 % from its FIT with the nominal stresses in every phase gets a warning: its stresses depend on the temperature.
- Sources of values: ˢ SPICE results, ᴰ derived, ᴹ parts files, ˀ default.

## Summary

- **Total:** 42.7 FIT (MTBF 23.4 × 10⁶ h, 2675 years, assuming constant failure rates)
- **With the nominal stresses in every phase:** 42.5 FIT; the stresses at the phase temperatures change the total by +0.4 %
- **Components:** 5: 4 in the netlist, 1 only in the parts files; 1 simulation-only element left out
- **Without FIT:** 1 (R3)
- **Derating:** 1 overstressed (R3), 0 with warnings
- **Derating at 32, 60 and 85 °C:** 1 overstressed (R3), 0 with warnings
- **Warnings:** 1 general, 4 components with warnings

### FIT per block

| Block | FIT | % |
|---|---|---|
| (none) | 40.7 | 95.3 |
| B2 | 1.52 | 3.56 |
| B1 | 0.5 | 1.17 |

### Largest contributors

| Ref | Class | FIT | % | Cumulative % |
|---|---|---|---|---|
| U1 | U | 39.9 | 93.4 | 93.4 |
| D2 | D | 1.52 | 3.56 | 97 |
| J9 | J | 0.788 | 1.85 | 98.8 |
| R1 | R | 0.5 | 1.17 | 100 |

## Components

| Ref | Class | Value | V | I | P | T | FIT |
|---|---|---|---|---|---|---|---|
| R1 | R | 10 kΩ | 10.9 Vᴰ | 1.09 mAˢ | 11.9 mWᴰ |  | 0.5 |
| R3 | R | 1 kΩ | 11.3 Vᴰ | 11.3 mAˢ | 127 mWᴰ |  | **none** |
| D2 | D |  | 12 Vᴰ | -12 pAˢ | 144 pWᴰ | 4.86e-08 °Cᴰ | 1.52 |
| U1 | U |  | 12 Vᴰ |  |  |  | 39.9 |
| J9 | J |  |  |  |  |  | 0.788 |

## Derating

| Ref | V/Vmax | P/Pmax | I/Imax | Result |
|---|---|---|---|---|
| R1 | 14.5 % | 11.9 % |  | ok |
| R3 | 15 % | **127 %** |  | **overstress** |
| D2 | 12 % |  | < 0.1 % | ok |
| U1 | 33.3 % |  |  | ok |

## Temperatures

The stresses of each mission phase, and its share of the total FIT.

| Phase | On | Tamb | Duration | Stresses at | FIT | % |
|---|---|---|---|---|---|---|
| off-day | no | 14 °C | 720 h | 27 °C (nominal) | 4.27 | 10 |
| off-night | no | 14 °C | 7532 h | 27 °C (nominal) | 17.1 | 40 |
| start-night | yes | 32 °C | 117 h | 32 °C | 4.27 | 10 |
| start-day | yes | 32 °C | 58 h | 32 °C | 2.13 | 5 |
| full-op | yes | 85 °C | 201 h | 85 °C | 12.8 | 30 |
| motorway | yes | 60 °C | 131 h | 60 °C | 2.13 | 5 |

For each component, the largest stress ratios at 32, 60 and 85 °C, relative to the power ratings derated for the temperature, and its FIT compared with the FIT with the nominal stresses in every phase.

| Ref | FIT | Nominal FIT | Change | V/Vmax | P/Pmax | I/Imax | Result |
|---|---|---|---|---|---|---|---|
| R1 | 0.5 | 0.367 | **+36.4 %** | 14.5 % at 32 °C | 17.3 % at 85 °C |  | ok |
| R3 | **none** | **none** |  | 15 % at 32 °C | **185 %** at 85 °C |  | **overstress** |
| D2 | 1.52 | 1.49 | +1.7 % | 12 % at 32 °C |  | < 0.1 % at 32 °C | ok |
| U1 | 39.9 | 39.9 | 0 % | 33.3 % at 32 °C |  |  | ok |
| J9 | 0.788 | 0.788 | 0 % |  |  |  | ok |

## FIDES metadata

| Ref | Class | Tags | Package | Part | Vmax | Pmax | Imax | Tmax | From | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| R1 | R | thick | 0603 |  | 75 V | 0.1 W |  | 155 °C | parts.db.csv (R0603) |  |
| R3 | R | thick | 0603 |  | 75 V | 0.1 W |  | 155 °C | parts.db.csv (R0603) |  |
| D2 | D |  | SOD123 |  | 100 V |  | 0.2 A | 150 °C | parts.db.csv (D1N4148) |  |
| U1 | U | analog | SOIC8 |  | 36 V |  |  | 125 °C | probe.bom.csv (U1) | netlist element XU1 |
| J9 | J | tht |  |  | 300 V |  |  | 105 °C | parts.db.csv (TB2) |  |

## Not simulated or not rated

- `V1` (voltage source): simulation only, not a component.
- Only in the parts files, without SPICE stress: J9.

## Mission profile

8759 h in total.

| Phase | Duration | On | Tamb | Tdelta | Ncycles | Tcycle | RH | Grms | Tmax | Saline pol | Env pol | Appl pol | IP | Factor |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| off-day | 720.0 | false | 14.0 | 10.0 | 30 | 24.00 | 70 | 0.0 | 19.0 | 1 | 1.5 | 2 | true | 3.1 |
| off-night | 7532.0 | false | 14.0 | 10.0 | 335 | 22.50 | 70 | 0.0 | 19.0 | 1 | 1.5 | 2 | true | 3.1 |
| start-night | 117.0 | true | 32.0 | 22.0 | 670 | 0.20 | 50 | 2.0 | 32.0 | 1 | 1.5 | 2 | true | 4.8 |
| start-day | 58.0 | true | 32.0 | 18.0 | 1340 | 0.04 | 60 | 2.0 | 32.0 | 1 | 1.5 | 2 | true | 4.8 |
| full-op | 201.0 | true | 85.0 | 53.0 | 335 | 0.60 | 30 | 1.0 | 85.0 | 1 | 1.5 | 2 | true | 4.8 |
| motorway | 131.0 | true | 60.0 | 28.0 | 30 | 4.40 | 30 | 2.0 | 60.0 | 1 | 1.5 | 2 | true | 4.8 |

## Warnings

- testdata/sample.net: analysis and output cards commented out: .op (line 9)
- **R1**: FIT +36.4 % with the stresses at the phase temperatures (0.5 instead of 0.367 with the nominal stresses): the stresses depend on the temperature
- **R3**: no FIT: phase full-op: Actual power (0.152746 W) exceeds its Pmax (0.100000 W) R=1000
- **R3**: P/Pmax = 127% (0.1273 W of 0.1 W): overstress
- **R3**: at 85 °C (full-op): P/Pmax = 185% (0.1528 W of 0.08235 W): overstress
- **U1**: self-heating not included: no power
- **J9**: not in the netlist: no SPICE stress
