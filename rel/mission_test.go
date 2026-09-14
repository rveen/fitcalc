package rel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadMissionComments: comment lines, with commas, are left out; errors
// name the file.
func TestLoadMissionComments(t *testing.T) {
	src, err := os.ReadFile("../testdata/mission.csv")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "m.csv")
	text := "# A mission, with comments\n# durations in h, temperatures in °C\n" + string(src)
	if err := os.WriteFile(file, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadMission(file)
	if err != nil {
		t.Fatal(err)
	}
	want, err := LoadMission("../testdata/mission.csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Phases) != len(want.Phases) || m.Ttotal != want.Ttotal {
		t.Errorf("%d phases, %g h; want %d, %g h", len(m.Phases), m.Ttotal, len(want.Phases), want.Ttotal)
	}

	bad := filepath.Join(dir, "bad.csv")
	if err := os.WriteFile(bad, []byte("phase,duration\noff,1,2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMission(bad); err == nil || !strings.HasPrefix(err.Error(), bad+": ") {
		t.Errorf("error %v, want one naming %s", err, bad)
	}
}
