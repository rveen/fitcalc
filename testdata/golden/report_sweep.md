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

- DC sweep (`.dc`), simulated with ngspice-47 at 27 °C. The stresses are assumed not to depend on the temperatures of the mission phases.
- The DC sweep of V1 from 10.8 V to 13.2 V in steps of 1.2 V (3 points) replaces the single operating point. The stresses in the tables and for the FIT are their means over the sweep; the derating checks take the largest ratio of each kind over it, and the warnings say where it is. The section DC sweep gives the range of the stresses.
- FIDES 2022 failure rates in FIT (failures in 10⁹ hours) for the mission profile below.
- V is the working voltage: the reverse voltage for diodes, V_CE or V_DS for transistors, the largest voltage between two pins for subcircuits.
- T is the temperature rise FIDES uses for diodes, transistors and ICs: P·Rth, with the FIDES default of the package on an FR4 board where the parts files give no rth.
- Derating: a warning above 80 %, an overstress above 100 % of the rating.
- Sources of values: ˢ SPICE results, ᴰ derived, ᴹ parts files, ˀ default.

## Summary

- **Total:** 42.5 FIT (MTBF 23.5 × 10⁶ h, 2685 years, assuming constant failure rates)
- **Components:** 5: 4 in the netlist, 1 only in the parts files; 1 simulation-only element left out
- **Without FIT:** 1 (R3)
- **Derating:** 1 overstressed (R3), 0 with warnings
- **Warnings:** 1 general, 3 components with warnings

### FIT per block

| Block | FIT | % |
|---|---|---|
| (none) | 40.7 | 95.6 |
| B2 | 1.49 | 3.51 |
| B1 | 0.367 | 0.862 |

### Largest contributors

| Ref | Class | FIT | % | Cumulative % |
|---|---|---|---|---|
| U1 | U | 39.9 | 93.8 | 93.8 |
| D2 | D | 1.49 | 3.51 | 97.3 |
| J9 | J | 0.788 | 1.85 | 99.1 |
| R1 | R | 0.367 | 0.862 | 100 |

## Components

| Ref | Class | Value | V | I | P | T | FIT |
|---|---|---|---|---|---|---|---|
| R1 | R | 10 kΩ | 10.9 Vᴰ | 1.09 mAˢ | 11.9 mWᴰ |  | 0.367 |
| R3 | R | 1 kΩ | 11.3 Vᴰ | 11.3 mAˢ | 127 mWᴰ |  | **none** |
| D2 | D |  | 12 Vᴰ | -12 pAˢ | 144 pWᴰ | 4.86e-08 °Cᴰ | 1.49 |
| U1 | U |  | 12 Vᴰ |  |  |  | 39.9 |
| J9 | J |  |  |  |  |  | 0.788 |

## DC sweep

The stresses over the sweep of V1 from 10.8 V to 13.2 V in steps of 1.2 V (3 points): the range of V and I, and the mean and the largest P, with the point where it is largest.

| Ref | V | I | P mean | P max | At |
|---|---|---|---|---|---|
| R1 | 9.82 V … 12 V | 982 µA … 1.2 mA | 11.9 mW | 13.1 mW | V1 = 13.2 V |
| R3 | 10.2 V … 12.4 V | 10.2 mA … 12.4 mA | 127 mW | 140 mW | V1 = 13.2 V |
| D2 | 10.8 V … 13.2 V | -13.2 pA … -10.8 pA | 144 pW | 158 pW | V1 = 13.2 V |
| U1 | 10.8 V … 13.2 V |  |  |  |  |

## Derating

| Ref | V/Vmax | P/Pmax | I/Imax | Result |
|---|---|---|---|---|
| R1 | 16 % | 13.1 % |  | ok |
| R3 | 16.5 % | **140 %** |  | **overstress** |
| D2 | 13.2 % |  | < 0.1 % | ok |
| U1 | 36.7 % |  |  | ok |

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
- **R3**: no FIT: Actual power (0.127288 W) exceeds its Pmax (0.100000 W) R=1000
- **R3**: P/Pmax = 140% (0.14 W of 0.1 W) with V1 = 13.2 V: overstress
- **U1**: self-heating not included: no power
- **J9**: not in the netlist: no SPICE stress
