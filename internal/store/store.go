// Package store keeps settings, progress and the run in progress as JSON
// files in the XDG config and state directories.
package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/butaraul/lgtm/internal/engine"
)

const app = "lgtm"

// Settings are user preferences.
type Settings struct {
	Motion bool   `json:"motion"`
	Color  string `json:"color"` // auto, 16, none
}

// DefaultSettings has motion on and colour detected from the terminal.
func DefaultSettings() Settings { return Settings{Motion: true, Color: "auto"} }

// Best is the best finished run on a scenario.
type Best struct {
	Score      int    `json:"score"`
	Grade      string `json:"grade"`
	Seed       uint64 `json:"seed"`
	Difficulty string `json:"difficulty"`
}

// Progress is the record of finished runs.
type Progress struct {
	Runs  int             `json:"runs"`
	Best  map[string]Best `json:"best"`
	Daily map[string]Best `json:"daily"` // keyed by date, YYYY-MM-DD
}

// Run is a run in progress: enough to replay it exactly.
type Run struct {
	Scenario   string          `json:"scenario"`
	Seed       uint64          `json:"seed"`
	Difficulty string          `json:"difficulty"`
	Daily      string          `json:"daily,omitempty"`
	Actions    []engine.Action `json:"actions"`
}

// Store reads and writes the files. A zero Store writes nothing, which is
// what tests use.
type Store struct {
	ConfigDir string
	StateDir  string
}

// Open locates the directories. XDG_CONFIG_HOME and XDG_STATE_HOME are
// honoured on every platform; otherwise ~/.config and ~/.local/state, or
// %AppData% and %LocalAppData% on Windows.
func Open() (*Store, error) {
	cfg, err := dir("XDG_CONFIG_HOME", ".config", os.UserConfigDir)
	if err != nil {
		return nil, err
	}
	state, err := dir("XDG_STATE_HOME", filepath.Join(".local", "state"), os.UserCacheDir)
	if err != nil {
		return nil, err
	}
	return &Store{ConfigDir: filepath.Join(cfg, app), StateDir: filepath.Join(state, app)}, nil
}

func dir(env, unixRel string, windows func() (string, error)) (string, error) {
	if v := os.Getenv(env); v != "" {
		return v, nil
	}
	if runtime.GOOS == "windows" {
		return windows()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, unixRel), nil
}

func (s *Store) path(dir, name string) string {
	if s == nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

func read(path string, v any) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func write(path string, v any) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Settings loads settings, falling back to defaults.
func (s *Store) Settings() (Settings, error) {
	st := DefaultSettings()
	err := read(s.path(s.configDir(), "settings.json"), &st)
	return st, err
}

// SaveSettings writes settings.
func (s *Store) SaveSettings(st Settings) error {
	return write(s.path(s.configDir(), "settings.json"), st)
}

// Progress loads the progress record.
func (s *Store) Progress() (Progress, error) {
	var p Progress
	err := read(s.path(s.stateDir(), "progress.json"), &p)
	if p.Best == nil {
		p.Best = map[string]Best{}
	}
	if p.Daily == nil {
		p.Daily = map[string]Best{}
	}
	return p, err
}

// SaveProgress writes the progress record.
func (s *Store) SaveProgress(p Progress) error {
	return write(s.path(s.stateDir(), "progress.json"), p)
}

// Run loads the run in progress, or nil.
func (s *Store) Run() (*Run, error) {
	var r Run
	if err := read(s.path(s.stateDir(), "run.json"), &r); err != nil || r.Scenario == "" {
		return nil, err
	}
	return &r, nil
}

// SaveRun writes the run in progress.
func (s *Store) SaveRun(r Run) error {
	return write(s.path(s.stateDir(), "run.json"), r)
}

// ClearRun removes the run in progress.
func (s *Store) ClearRun() error {
	return remove(s.path(s.stateDir(), "run.json"))
}

// Reset removes progress and the run in progress. Settings are kept. It
// returns the files it removed.
func (s *Store) Reset() ([]string, error) {
	var gone []string
	for _, name := range []string{"progress.json", "run.json"} {
		p := s.path(s.stateDir(), name)
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			if err := remove(p); err != nil {
				return gone, err
			}
			gone = append(gone, p)
		}
	}
	return gone, nil
}

func remove(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) configDir() string {
	if s == nil {
		return ""
	}
	return s.ConfigDir
}

func (s *Store) stateDir() string {
	if s == nil {
		return ""
	}
	return s.StateDir
}
