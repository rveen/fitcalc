# Reliability analysis: OP probe circuit: one element of each kind handled by fitcalc

- **Project:** `testdata/fitcalc.toml`
- **Netlist:** `testdata/probe.net`
- **Date:** 2026-09-14 14:01 UTC
- **fitcalc:** v0.0.0-20260914140056-ba933a5aa4ff
- **FIDES 2022 Edition A:** github.com/rveen/fides v0.1.1
- **Simulator:** ngspice-43

## Inputs

| File | Role | SHA-256 |
|---|---|---|
| `testdata/fitcalc.toml` | project | `37106177d30c44d3f0d8b4d8b71fe22a462515db1ab8d3c59f6b57b000845dd2` |
| `testdata/probe.net` | netlist | `6277f86b3a626bcc8dc077c3143df7cd4780aa838c805e332970f1c998519b0f` |
| `testdata/probe.bom.csv` | parts (BOM) | `6069b9974fe53b7f27f848881be69b8d6771d4ce0308b72cdc7bcdf8a17f96c2` |
| `testdata/parts.db.csv` | parts | `b4da14040cd19f302aaa11421c7c9efcce757c18f6f9289efde0c069ac27d193` |
| `testdata/mission.csv` | mission | `569a37f7760f285361d9306900509d65623bb5579e4db0f26779d83e0e39f0e6` |

## Analysis

- DC operating point (`.op`), simulated with ngspice-43 at 27 °C for the nominal stresses in the tables, and again at 32 °C (start-night, start-day), 60 °C (motorway) and 85 °C (full-op).
- FIDES 2022 failure rates in FIT (failures in 10⁹ hours) for the mission profile below, with the stresses of each operating phase simulated at its ambient temperature. Phases that are not operating do not depend on the stresses.
- V is the working voltage: the reverse voltage for diodes, V_CE or V_DS for transistors, the largest voltage between two pins for subcircuits.
- T is the temperature rise FIDES uses for diodes, transistors and ICs: P·Rth, with the FIDES default of the package on an FR4 board where the parts files give no rth.
- Derating: a warning above 80 %, an overstress above 100 % of the rating.
- Derating at 32, 60 and 85 °C: with the stresses simulated at that ambient temperature, and the power ratings derated linearly from the rated temperature (`trated`; default 70 °C for resistors, 25 °C for other classes) to 0 at `tmax`.
- A component whose FIT differs by more than 10 % from its FIT with the nominal stresses in every phase gets a warning: its stresses depend on the temperature.
- Sources of values: ˢ SPICE results, ᴰ derived, ᴹ parts files, ˀ default.

## Summary

- **Total:** 74.9 FIT (MTBF 13.4 × 10⁶ h, 1524 years, assuming constant failure rates)
- **With the nominal stresses in every phase:** 75.2 FIT; the stresses at the phase temperatures change the total by -0.4 %
- **Components:** 18: 17 in the netlist, 1 only in the parts files; 1 simulation-only element left out
- **Without FIT:** 0
- **Derating:** 0 overstressed, 0 with warnings
- **Derating at 32, 60 and 85 °C:** 0 overstressed, 1 with warnings (RC)
- **Warnings:** 1 general, 2 components with warnings

### Largest contributors

| Ref | Class | FIT | % | Cumulative % |
|---|---|---|---|---|
| X1 | U | 38.7 | 51.7 | 51.7 |
| L1 | L | 8.59 | 11.5 | 63.1 |
| C1 | C | 4.74 | 6.33 | 69.5 |
| M1 | Q | 4.39 | 5.87 | 75.3 |
| J1 | Q | 3.33 | 4.44 | 79.8 |
| Q1 | Q | 2.96 | 3.96 | 83.7 |
| J9 | J | 1.2 | 1.61 | 85.4 |
| RC | R | 1.17 | 1.56 | 86.9 |
| R3 | R | 1.17 | 1.56 | 88.5 |
| R1 | R | 1.17 | 1.56 | 90 |

## Components

| Ref | Class | Value | V | I | P | T | FIT |
|---|---|---|---|---|---|---|---|
| R1 | R | 10 kΩ | 10.9 Vᴰ | 1.09 mAˢ | 11.9 mWᴰ |  | 1.17 |
| R2 | R | 1 kΩ | 1.09 Vᴰ | 1.09 mAˢ | 1.19 mWᴰ |  | 1.17 |
| C1 | C | 100 nF | 1.09 Vᴰ |  |  |  | 4.74 |
| L1 | L | 10 µH | 0 Vᴰ | 11.3 mAˢ | 6.36 µWᴰ |  | 8.59 |
| R3 | R | 1 kΩ | 11.3 Vᴰ | 11.3 mAˢ | 127 mWᴰ |  | 1.17 |
| D1 | D |  | 0 Vᴰ | 11.3 mAˢ | 8.1 mWᴰ | 2.73 °Cᴰ | 0.244 |
| D2 | D |  | 12 Vᴰ | -12 pAˢ | 144 pWᴰ | 4.86e-08 °Cᴰ | 0.236 |
| RB | R | 100 kΩ | 11.2 Vᴰ | 112 µAˢ | 1.25 mWᴰ |  | 1.17 |
| Q1 | Q |  | 123 mVᴰ | 5.94 mAˢ | 824 µWᴰ | 0.365 °Cᴰ | 2.96 |
| RC | R | 2 kΩ | 11.9 Vᴰ | 5.94 mAˢ | 70.5 mWᴰ |  | 1.17 |
| M1 | Q |  | 10.4 Vˢ | 1.57 mAˢ | 16.3 mWᴰ | 7.23 °Cᴰ | 4.39 |
| RG | R | 1 MΩ | 9.23 Vᴰ | 9.23 µAˢ | 85.2 µWᴰ |  | 1.17 |
| RG2 | R | 300 kΩ | 2.77 Vᴰ | 9.23 µAˢ | 25.6 µWᴰ |  | 1.17 |
| RD | R | 1 kΩ | 1.57 Vᴰ | 1.57 mAˢ | 2.45 mWᴰ |  | 1.17 |
| J1 | Q |  | 10 Vᴰ | 400 µAˢ | 4 mWᴰ | 1.77 °Cᴰ | 3.33 |
| RJ | R | 5 kΩ | 2 Vᴰ | 400 µAˢ | 800 µWᴰ |  | 1.17 |
| X1 | U |  | 6 Vᴰ | 3 mAˢ | 36 mWᴰ | 4.96 °Cᴰ | 38.7 |
| J9 | J |  |  |  |  |  | 1.2 |

## Derating

| Ref | V/Vmax | P/Pmax | I/Imax | Result |
|---|---|---|---|---|
| R1 | 14.5 % | 11.9 % |  | ok |
| R2 | 1.45 % | 1.19 % |  | ok |
| C1 | 2.18 % |  |  | ok |
| L1 |  |  | 0.752 % | ok |
| R3 | 5.64 % | 50.9 % |  | ok |
| D1 | 0 % | 1.62 % | 5.64 % | ok |
| D2 | 12 % | < 0.1 % | < 0.1 % | ok |
| RB | 14.9 % | 1.25 % |  | ok |
| Q1 | 0.274 % | 0.33 % | 5.94 % | ok |
| RC | 15.8 % | 70.5 % |  | ok |
| M1 | 20.9 % | 4.54 % | 0.783 % | ok |
| RG | 12.3 % | < 0.1 % |  | ok |
| RG2 | 3.69 % | < 0.1 % |  | ok |
| RD | 2.09 % | 2.45 % |  | ok |
| J1 | 25 % | 1.14 % | 0.8 % | ok |
| RJ | 2.67 % | 0.8 % |  | ok |
| X1 | 16.7 % |  |  | ok |

## Temperatures

The stresses of each mission phase, and its share of the total FIT.

| Phase | On | Tamb | Duration | Stresses at | FIT | % |
|---|---|---|---|---|---|---|
| off-day | no | 14 °C | 720 h | 27 °C (nominal) | 0.0646 | 0.0862 |
| off-night | no | 14 °C | 7532 h | 27 °C (nominal) | 0.698 | 0.932 |
| start-night | yes | 32 °C | 117 h | 32 °C | 3.23 | 4.31 |
| start-day | yes | 32 °C | 58 h | 32 °C | 2.39 | 3.19 |
| full-op | yes | 85 °C | 201 h | 85 °C | 62.5 | 83.5 |
| motorway | yes | 60 °C | 131 h | 60 °C | 5.97 | 7.97 |

For each component, the largest stress ratios at 32, 60 and 85 °C, relative to the power ratings derated for the temperature, and its FIT compared with the FIT with the nominal stresses in every phase.

| Ref | FIT | Nominal FIT | Change | V/Vmax | P/Pmax | I/Imax | Result |
|---|---|---|---|---|---|---|---|
| R1 | 1.17 | 1.17 | 0 % | 14.5 % at 32 °C | 14.5 % at 85 °C |  | ok |
| R2 | 1.17 | 1.17 | 0 % | 1.45 % at 32 °C | 1.45 % at 85 °C |  | ok |
| C1 | 4.74 | 4.74 | 0 % | 2.18 % at 32 °C |  |  | ok |
| L1 | 8.59 | 8.59 | 0 % |  |  | 0.758 % at 85 °C | ok |
| R3 | 1.17 | 1.17 | 0 % | 5.69 % at 85 °C | 62.8 % at 85 °C |  | ok |
| D1 | 0.244 | 0.246 | -0.5 % | 0 % at 32 °C | 2.74 % at 85 °C | 5.69 % at 85 °C | ok |
| D2 | 0.236 | 0.236 | 0 % | 12 % at 32 °C | < 0.1 % at 85 °C | < 0.1 % at 85 °C | ok |
| RB | 1.17 | 1.17 | 0 % | 15 % at 85 °C | 1.54 % at 85 °C |  | ok |
| Q1 | 2.96 | 2.96 | +0.3 % | 0.326 % at 85 °C | 0.733 % at 85 °C | 5.94 % at 32 °C | ok |
| RC | 1.17 | 1.17 | 0 % | 15.8 % at 32 °C | *85.3 %* at 85 °C |  | **warning** |
| M1 | 4.39 | 4.68 | -6.2 % | 21.4 % at 85 °C | 7.33 % at 85 °C | 0.768 % at 32 °C | ok |
| RG | 1.17 | 1.17 | 0 % | 12.3 % at 32 °C | 0.103 % at 85 °C |  | ok |
| RG2 | 1.17 | 1.17 | 0 % | 3.69 % at 32 °C | < 0.1 % at 85 °C |  | ok |
| RD | 1.17 | 1.17 | 0 % | 2.05 % at 32 °C | 2.36 % at 32 °C |  | ok |
| J1 | 3.33 | 3.33 | 0 % | 25 % at 32 °C | 2.2 % at 85 °C | 0.8 % at 85 °C | ok |
| RJ | 1.17 | 1.17 | 0 % | 2.67 % at 85 °C | 0.971 % at 85 °C |  | ok |
| X1 | 38.7 | 38.7 | 0 % | 16.7 % at 32 °C |  |  | ok |
| J9 | 1.2 | 1.2 | 0 % |  |  |  | ok |

## FIDES metadata

| Ref | Class | Tags | Package | Part | Vmax | Pmax | Imax | Tmax | From | Notes |
|---|---|---|---|---|---|---|---|---|---|---|
| R1 | R | thick | 0603 |  | 75 V | 0.1 W |  | 155 °C | probe.bom.csv (R1), parts.db.csv (R0603) |  |
| R2 | R | thick | 0603 |  | 75 V | 0.1 W |  | 155 °C | probe.bom.csv (R2), parts.db.csv (R0603) |  |
| C1 | C | cer x7r | 0603 |  | 50 V |  |  | 125 °C | probe.bom.csv (C1), parts.db.csv (C0603X7R) |  |
| L1 | L | power |  |  |  |  | 1.5 A | 125 °C | probe.bom.csv (L1), parts.db.csv (LPWR) |  |
| R3 | R | thick | 1206 |  | 200 V | 0.25 W |  | 155 °C | probe.bom.csv (R3), parts.db.csv (R1206) |  |
| D1 | D |  | SOD123 |  | 100 V | 0.5 W | 0.2 A | 150 °C | probe.bom.csv (D1), parts.db.csv (D1N4148) |  |
| D2 | D |  | SOD123 |  | 100 V | 0.5 W | 0.2 A | 150 °C | probe.bom.csv (D2), parts.db.csv (D1N4148) |  |
| RB | R | thick | 0603 |  | 75 V | 0.1 W |  | 155 °C | probe.bom.csv (RB), parts.db.csv (R0603) |  |
| Q1 | Q |  | SOT23 |  | 45 V | 0.25 W | 0.1 A | 150 °C | probe.bom.csv (Q1), parts.db.csv (BC847) |  |
| RC | R | thick | 0603 |  | 75 V | 0.1 W |  | 155 °C | probe.bom.csv (RC), parts.db.csv (R0603) |  |
| M1 | Q | mos | SOT23 |  | 50 V | 0.36 W | 0.2 A | 150 °C | probe.bom.csv (M1), parts.db.csv (BSS138) | tag mos from the SPICE element type (MOSFET) |
| RG | R | thick | 0603 |  | 75 V | 0.1 W |  | 155 °C | probe.bom.csv (RG), parts.db.csv (R0603) |  |
| RG2 | R | thick | 0603 |  | 75 V | 0.1 W |  | 155 °C | probe.bom.csv (RG2), parts.db.csv (R0603) |  |
| RD | R | thick | 0603 |  | 75 V | 0.1 W |  | 155 °C | probe.bom.csv (RD), parts.db.csv (R0603) |  |
| J1 | Q | jfet | SOT23 |  | 40 V | 0.35 W | 0.05 A | 150 °C | probe.bom.csv (J1), parts.db.csv (J201) | tag jfet from the SPICE element type (JFET) |
| RJ | R | thick | 0603 |  | 75 V | 0.1 W |  | 155 °C | probe.bom.csv (RJ), parts.db.csv (R0603) |  |
| X1 | U | analog | SOIC8 |  | 36 V |  |  | 125 °C | probe.bom.csv (X1), parts.db.csv (DIVIC) |  |
| J9 | J | tht |  |  | 300 V |  | 10 A | 105 °C | probe.bom.csv (J9), parts.db.csv (TB2) |  |

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

- testdata/probe.net: analysis and output cards commented out: .op (line 29)
- **RC**: at 85 °C (full-op): P/Pmax = 85% (0.07025 W of 0.08235 W): warning (limit 80 %)
- **J9**: not in the netlist: no SPICE stress
