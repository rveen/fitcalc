package spice

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var tempRE = regexp.MustCompile(`Doing analysis at TEMP = ([-+0-9.eE]+)`)

// simTemp returns the analysis temperature that ngspice reports in its
// output, in °C, or NaN.
func simTemp(log string) float64 {
	m := tempRE.FindStringSubmatch(log)
	if m == nil {
		return math.NaN()
	}
	t, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return math.NaN()
	}
	return t
}

// ngspiceLog is what fitcalc takes from the output of an ngspice run.
type ngspiceLog struct {
	errors      []string
	warnings    []string // distinct, with the number of repetitions
	unavailable []string // vectors that could not be saved
}

var unavailableRE = regexp.MustCompile(`vector (\S+) is not available`)

// scanLog classifies the lines of ngspice's output. In batch mode ngspice
// exits with status 0 after most simulation failures (verified with ngspice
// 47), so the output is where they show.
func scanLog(s string) ngspiceLog {

	var l ngspiceLog
	count := map[string]int{}
	var warnings []string

	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		lt := strings.ToLower(t)
		switch {
		case strings.HasPrefix(lt, "warning"):
			if m := unavailableRE.FindStringSubmatch(t); m != nil {
				l.unavailable = append(l.unavailable, strings.ToLower(m[1]))
			}
			if count[t] == 0 {
				warnings = append(warnings, t)
			}
			count[t]++
		case strings.HasPrefix(lt, "error"),
			strings.Contains(lt, "dc solution failed"),
			strings.Contains(lt, "simulation(s) aborted"),
			strings.Contains(lt, "simulation interrupted"):
			l.errors = append(l.errors, t)
		}
	}

	for _, w := range warnings {
		if n := count[w]; n > 1 {
			w = fmt.Sprintf("%s (%d times)", w, n)
		}
		l.warnings = append(l.warnings, "ngspice: "+w)
	}
	return l
}

// zeroLength returns the device vectors that a raw file lists with zero
// length ("dims=0" in the type column). ngspice 47 writes a parameter that
// the device does not have this way, with the value 0 and no warning.
func zeroLength(raw []byte) []string {
	var v []string
	in := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "Variables:":
			in = true
		case line == "Binary:" || line == "Values:":
			return v
		case in:
			f := strings.Split(strings.TrimLeft(line, "\t "), "\t")
			if len(f) >= 3 && strings.Contains(f[2], "dims=0") {
				v = append(v, deviceVector(f[1]))
			}
		}
	}
	return v
}

// deviceVector returns a raw file variable name in lower case, without the
// i(…) or v(…) that ngspice puts around device vectors: i(@r1[i]) is
// @r1[i].
func deviceVector(name string) string {
	name = strings.ToLower(name)
	if (strings.HasPrefix(name, "i(@") || strings.HasPrefix(name, "v(@")) && strings.HasSuffix(name, ")") {
		return name[2 : len(name)-1]
	}
	return name
}

// rawHeader returns the header fields of a raw file (Title, Plotname, Flags,
// …). The header is text in both the ASCII and the binary format.
func rawHeader(raw []byte) map[string]string {
	h := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "Binary:" || line == "Values:" || line == "Variables:" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			h[k] = strings.TrimSpace(v)
		}
	}
	return h
}
