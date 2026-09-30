package store

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/butaraul/lgtm/internal/engine"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &Store{ConfigDir: filepath.Join(dir, "config"), StateDir: filepath.Join(dir, "state")}

	st, err := s.Settings()
	if err != nil || st != DefaultSettings() {
		t.Fatalf("fresh settings = %+v, %v", st, err)
	}
	want := Settings{Motion: false, Color: "16"}
	if err := s.SaveSettings(want); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Settings(); got != want {
		t.Errorf("settings = %+v, want %+v", got, want)
	}

	run := Run{Scenario: "landing", Seed: 42, Difficulty: "hard", Actions: []engine.Action{
		{Kind: engine.ActStart}, {Kind: engine.ActInspect, Hunk: 2}, {Kind: engine.ActReject, Reason: "other", Note: "hmm"},
	}}
	if err := s.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	got, err := s.Run()
	if err != nil || !reflect.DeepEqual(*got, run) {
		t.Errorf("run = %+v, %v", got, err)
	}

	p, _ := s.Progress()
	p.Runs = 3
	p.Best["landing"] = Best{Score: 91, Grade: "A"}
	if err := s.SaveProgress(p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Progress(); got.Runs != 3 || got.Best["landing"].Grade != "A" {
		t.Errorf("progress = %+v", got)
	}

	gone, err := s.Reset()
	if err != nil || len(gone) != 2 {
		t.Errorf("reset removed %v, %v", gone, err)
	}
	if r, _ := s.Run(); r != nil {
		t.Error("run survived reset")
	}
	if got, _ := s.Settings(); got != want {
		t.Error("reset should keep settings")
	}
}

func TestZeroStoreWritesNothing(t *testing.T) {
	var s *Store
	if err := s.SaveRun(Run{Scenario: "x"}); err != nil {
		t.Fatal(err)
	}
	if r, err := s.Run(); r != nil || err != nil {
		t.Errorf("nil store run = %v, %v", r, err)
	}
}

func TestOpenHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	t.Setenv("XDG_STATE_HOME", "/tmp/state")
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if s.ConfigDir != filepath.Join("/tmp/cfg", "lgtm") || s.StateDir != filepath.Join("/tmp/state", "lgtm") {
		t.Errorf("dirs = %+v", s)
	}
}
