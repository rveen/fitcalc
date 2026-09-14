# FIDES 2022 validation

fitcalc's FIT values are checked against an independent calculation written
from the FIDES Guide 2022 Edition A (July 2023), not from the Go `fides`
library.

There are two sets of references:

- **Probe circuit** (whole pipeline, here). `fides_ref.py` implements the
  models of the probe circuit's components (`testdata/probe.net`,
  `probe.bom.csv`, `parts.db.csv`). It takes the stresses (power, voltage,
  junction temperature rise) that fitcalc derives from ngspice, so only the
  reliability model is compared. `reference.csv` holds its results, and
  `rel/validation_test.go` (`TestFIDESReference`) runs fitcalc on the probe
  circuit and requires every FIT value to be within 10⁻⁴ of the reference.
- **One case per model and table row** (library). 99 cases covering every
  model and table row the library implements. They are part of
  github.com/rveen/fides, in its `testdata/validation`, together with
  `fides_ref.py`, the coverage table, the deviations found and fixed, and
  what is not supported.

Regenerate and compare, with rveen/fides checked out next to fitcalc:

    python3 ../fides/testdata/validation/fides_ref.py > testdata/validation/reference.csv
    go test ./rel

    fitcalc -f csv -o probe.csv -p testdata/probe.bom.csv -p testdata/parts.db.csv \
        -m testdata/mission.csv testdata/probe.net
    python3 ../fides/testdata/validation/fides_ref.py probe.csv

The probe circuit results:

| Ref | Model | Guide FIT | fitcalc FIT |
|---|---|---|---|
| R1, R3 | fixed resistor, SMD, thick film | 1.16537, 1.16554 | same (≤ 2·10⁻⁶) |
| C1 | ceramic capacitor, type II X7R, category 1 | 4.74231 | same |
| L1 | high-current wirewound inductor | 8.59437 | same |
| D1, D2 | signal diode, SOD123, forward / reverse-biased | 0.245599, 0.236343 | same |
| Q1, M1, J1 | bipolar, MOS, JFET < 5 W, SOT23 | 2.95545, 4.68488, 3.32767 | same |
| X1 | analog IC, SO8, with the self-heating of its measured 36 mW | 38.6972 | same |

X1's pin currents are measured since M3, and its 36 mW with the FIDES default
Rth of an SO8 package (400·8^−0.58·1.15 = 138 K/W) give a junction temperature
rise of 4.95756 K. The `fides_ref.py` of github.com/rveen/fides v0.1.0 still has
X1 without it; its value here is the same model with that rise:

    python3 -c "import sys; sys.path.insert(0, '../fides/testdata/validation'); import fides_ref as f; print(f.ic_so(f.mission(), 8, 0.086, 4.95756, 1.3))"
| J9 | PCB connector, through-hole, 2 contacts | 1.20373 | same |
