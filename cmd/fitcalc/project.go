package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// projectFile is the name of the project file in a project directory.
const projectFile = "fitcalc.toml"

// project is a project file: the settings of the long flags, with paths
// relative to the file.
type project struct {
	Netlist  string   `toml:"netlist"`
	Parts    []string `toml:"parts"`
	Mission  string   `toml:"mission"`
	Derating string   `toml:"derating"`
	Thermal  string   `toml:"thermal"`
	Sweep    string   `toml:"sweep"`
	Tran     string   `toml:"tran"`
	Hot      *float64 `toml:"hot"`
	Ngspice  string   `toml:"ngspice"`
	Keep     string   `toml:"keep"`
	Output   string   `toml:"output"`
	Format   string   `toml:"format"`
	MC       int      `toml:"mc"`
	Seed     *uint64  `toml:"seed"`
	Vary     string   `toml:"vary"`
}

// loadProject reads a project file. Unknown keys are errors.
func loadProject(file string) (*project, error) {
	p := &project{}
	md, err := toml.DecodeFile(file, p)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		var keys []string
		for _, k := range u {
			keys = append(keys, k.String())
		}
		return nil, fmt.Errorf("%s: unknown keys: %s", file, strings.Join(keys, ", "))
	}
	if p.Netlist == "" {
		return nil, fmt.Errorf("%s: no netlist", file)
	}
	return p, nil
}

// projectOf returns the project file that a command line argument names: arg
// itself if it is a .toml file, arg/fitcalc.toml if it is a directory, and
// "" for a netlist. Without arguments, it is ./fitcalc.toml if there is one.
func projectOf(args []string) (string, error) {
	if len(args) == 0 {
		if _, err := os.Stat(projectFile); err == nil {
			return projectFile, nil
		}
		return "", nil
	}
	arg := args[0]
	if strings.EqualFold(filepath.Ext(arg), ".toml") {
		return arg, nil
	}
	fi, err := os.Stat(arg)
	if err != nil || !fi.IsDir() {
		return "", nil
	}
	file := filepath.Join(arg, projectFile)
	if _, err := os.Stat(file); errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s is a directory without %s", arg, projectFile)
	}
	return file, nil
}

// apply sets the settings of the project read from file that the command
// line does not set: set holds the long names of the flags it sets. Paths
// are made relative to the directory of file; output "-" is stdout.
func (p *project) apply(cfg *config, file string, set map[string]bool) {

	dir := filepath.Dir(file)
	path := func(s string) string {
		if s == "" || s == "-" || filepath.IsAbs(s) {
			return s
		}
		return filepath.Join(dir, s)
	}
	str := func(flag string, dst *string, v string, isPath bool) {
		if set[flag] || v == "" {
			return
		}
		if isPath {
			v = path(v)
		}
		*dst = v
		cfg.fromProject[flag] = true
	}

	cfg.project, cfg.fromProject = file, map[string]bool{}
	cfg.netlist = path(p.Netlist)
	if !set["parts"] && len(p.Parts) > 0 {
		cfg.parts = nil
		for _, f := range p.Parts {
			cfg.parts = append(cfg.parts, path(f))
		}
	}
	str("mission", &cfg.mission, p.Mission, true)
	str("derating", &cfg.derating, p.Derating, true)
	str("thermal", &cfg.thermal, p.Thermal, true)
	str("sweep", &cfg.sweep, p.Sweep, false)
	str("tran", &cfg.tran, p.Tran, false)
	if p.Hot != nil {
		str("hot", &cfg.hot, strconv.FormatFloat(*p.Hot, 'g', -1, 64), false)
	}
	str("ngspice", &cfg.ngspice, p.Ngspice, false)
	str("keep", &cfg.keep, p.Keep, true)
	str("output", &cfg.output, p.Output, true)
	str("format", &cfg.format, p.Format, false)
	str("vary", &cfg.vary, p.Vary, false)
	if !set["mc"] && p.MC != 0 {
		cfg.mc, cfg.fromProject["mc"] = p.MC, true
	}
	if !set["seed"] && p.Seed != nil {
		cfg.seed, cfg.fromProject["seed"] = *p.Seed, true
	}
}

// origin names where a setting comes from, for error messages: "--sweep", or
// "sweep in dir/fitcalc.toml".
func (cfg *config) origin(flag string) string {
	if cfg.fromProject[flag] {
		return flag + " in " + cfg.project
	}
	return "--" + flag
}
