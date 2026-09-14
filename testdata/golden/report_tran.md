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

- Transient analysis (`.tran`), simulated with ngspice-47 at 27 °C. The stresses are assumed not to depend on the temperatures of the mission phases.
- The transient analysis from 0 to 3 ms in steps of 10 µs, recorded from 1 ms (201 time points) replaces the single operating point. The stresses in the tables and for the FIT are their RMS values over the recorded time, with the sign of their mean, and the mean power: for a circuit that does not change, those of its operating point. The derating checks compare the peak voltages, the RMS currents and the mean power with the ratings, as continuous current and power ratings are thermal. The section Transient gives the peaks.
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

## Transient

The stresses over the transient from 0 to 3 ms in steps of 10 µs, recorded from 1 ms (201 time points): the peak of V, I and P, the value of the largest magnitude with the time where it is, the RMS values of V and I and the mean of P.

| Ref | V peak | V RMS | I peak | I RMS | P peak | P mean |
|---|---|---|---|---|---|---|
| R1 | 12 V at 2.25 ms | 11 V | 1.2 mA at 2.25 ms | 1.1 mA | 13.1 mW at 2.25 ms | 11.9 mW |
| R3 | 12.4 V at 2.25 ms | 11.4 V | 12.4 mA at 2.25 ms | 11.4 mA | 140 mW at 2.25 ms | 127 mW |
| D2 | 13.2 V at 2.25 ms | 12.1 V | -13.2 pA at 1.5 ms | 12.1 pA | 158 pW at 2.25 ms | 144 pW |
| U1 | 13.2 V at 2.25 ms | 12.1 V |  |  |  |  |

## Derating

| Ref | V/Vmax | P/Pmax | I/Imax | Result |
|---|---|---|---|---|
| R1 | 14.5 % | 11.9 % |  | ok |
| R3 | 15 % | **127 %** |  | **overstress** |
| D2 | 12 % |  | < 0.1 % | ok |
| U1 | 33.3 % |  |  | ok |

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
- **R3**: P/Pmax = 127% (0.1273 W of 0.1 W): overstress
- **U1**: self-heating not included: no power
- **J9**: not in the netlist: no SPICE stress
