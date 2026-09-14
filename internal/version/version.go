// Package version reports the version of fitcalc and of its build.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Fitcalc returns the fitcalc version: the module version the go command
// stamped into the binary (a tag or a pseudo-version), or, if there is none,
// "devel-" plus the VCS revision.
func Fitcalc() string {

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}

	var rev, modified string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev == "" {
		return "devel"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if modified == "true" {
		rev += "-dirty"
	}
	return "devel-" + rev
}

// Fides returns the version of the github.com/rveen/fides module in the
// build, or "devel" if it comes from a workspace or a local replacement.
func Fides() string {

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, d := range bi.Deps {
		if d.Path != "github.com/rveen/fides" {
			continue
		}
		if d.Replace != nil {
			d = d.Replace
		}
		if d.Version == "" || d.Version == "(devel)" {
			return "devel"
		}
		return d.Version
	}
	return "unknown"
}

// String is the text printed by fitcalc --version.
func String() string {
	return fmt.Sprintf("fitcalc %s\nfides %s\n%s %s/%s", Fitcalc(), Fides(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
