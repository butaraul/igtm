package tui

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/butaraul/lgtm/internal/engine"
	"github.com/butaraul/lgtm/internal/scenario"
	"github.com/butaraul/lgtm/internal/store"
)

var update = flag.Bool("update", false, "rewrite golden files")

func plainStyles() *styles {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.Ascii)
	return newStyles(r)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from golden file; run go test ./internal/tui -update and review the diff.\n--- got ---\n%s", name, got)
	}
}

func scenarios(t *testing.T) []*scenario.Scenario {
	t.Helper()
	list, err := scenario.All()
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestDiffRendererGolden(t *testing.T) {
	st := plainStyles()
	s := scenario.Find(scenarios(t), "landing")
	var turn *scenario.Turn
	for _, x := range s.Turns {
		if x.ID == "hardening" {
			turn = x
		}
	}
	d := turn.Variant("swallowed-error").Parsed()
	n := d.NumHunks()
	open, read := make([]bool, n), make([]bool, n)
	open[0], read[0] = true, true
	rows := diffRows(st, d, open, read, 60, nil)
	got := viewRows(st, rows, 3, 0, len(rows), len(rows))
	golden(t, "diff_hardening", got+"\n")

	// Collapsed, narrow: only file and hunk headers, truncated to fit.
	rows = diffRows(st, d, make([]bool, n), make([]bool, n), 38, nil)
	golden(t, "diff_collapsed", viewRows(st, rows, 0, 0, len(rows), len(rows))+"\n")
	for _, r := range rows {
		if w := lipgloss.Width(r.text); r.kind != rowBlank && w != 38 {
			t.Errorf("row width %d, want 38: %q", w, r.text)
		}
	}
}

func TestWrapSegs(t *testing.T) {
	st := plainStyles()
	rows := wrapSegs([]seg{{"abcdef", st.base}, {"ghij", st.dim}}, 4)
	var got []string
	for _, r := range rows {
		s := ""
		for _, x := range r {
			s += x.text
		}
		got = append(got, s)
	}
	if strings.Join(got, "|") != "abcd|efgh|ij" {
		t.Errorf("wrap = %v", got)
	}
}

// scripted plays the landing scenario: reads every hunk, rejects the known
// secret, accepts everything else, and ships.
func scripted(t *testing.T, seed uint64) *engine.Game {
	t.Helper()
	g, err := engine.New(scenario.Find(scenarios(t), "landing"), engine.Config{Seed: seed, Difficulty: engine.Normal})
	if err != nil {
		t.Fatal(err)
	}
	do := func(a engine.Action) {
		t.Helper()
		if err := g.Apply(a); err != nil {
			t.Fatalf("%s: %v", a.Kind, err)
		}
	}
	do(engine.Action{Kind: engine.ActStart})
	for g.Phase() == engine.PhaseReview {
		c := g.Current()
		if c.Seq%2 == 1 {
			for i := range c.Diff.NumHunks() {
				if !g.Inspected(i) {
					do(engine.Action{Kind: engine.ActInspect, Hunk: i})
				}
			}
			if !c.TestsRun {
				do(engine.Action{Kind: engine.ActTests})
			}
		}
		if strings.Contains(c.Message, "falls back to the dev key") {
			do(engine.Action{Kind: engine.ActReject, Reason: string(scenario.HardcodedSecret), Note: "live key in source"})
			continue
		}
		do(engine.Action{Kind: engine.ActAccept})
	}
	do(engine.Action{Kind: engine.ActShip})
	return g
}

func TestPostMortemGolden(t *testing.T) {
	st := plainStyles()
	g := scripted(t, 4242)
	golden(t, "postmortem_landing", strings.Join(postMortemLines(st, g, 90), "\n")+"\n")
	golden(t, "results_landing", strings.Join(resultsLines(st, g, 90), "\n")+"\n")
	golden(t, "production_landing", strings.Join(productionLines(st, g, 90), "\n")+"\n")
}

// TestDrive pushes keys through the whole app to catch panics and layout
// overflow at the minimum size and a wide one.
func TestDrive(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 40}} {
		for _, s := range scenarios(t) {
			m := New(Options{
				Scenarios: scenarios(t),
				Store:     &store.Store{},
				Settings:  store.Settings{Motion: false, Color: "none"},
				Renderer:  plainStyles().r,
				Scenario:  s.ID,
				Seed:      7,
				SeedSet:   true,
			})
			var model tea.Model = m
			send := func(msg tea.Msg) {
				model, _ = model.Update(msg)
				view := model.View()
				lines := strings.Split(view, "\n")
				if len(lines) > size[1] {
					t.Fatalf("%s %dx%d: view has %d lines", s.ID, size[0], size[1], len(lines))
				}
				for _, l := range lines {
					if w := lipgloss.Width(l); w > size[0] {
						t.Fatalf("%s %dx%d: line width %d: %q", s.ID, size[0], size[1], w, l)
					}
				}
			}
			key := func(k string) {
				switch k {
				case "enter":
					send(tea.KeyMsg{Type: tea.KeyEnter})
				case "tab":
					send(tea.KeyMsg{Type: tea.KeyTab})
				case "esc":
					send(tea.KeyMsg{Type: tea.KeyEsc})
				default:
					send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
				}
			}
			send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			key("enter") // brief -> review
			for i := 0; m.game.Phase() != engine.PhaseOver && i < 400; i++ {
				switch {
				case m.game.Phase() == engine.PhaseShip:
					key("enter")
					key("y")
				case i%7 == 3:
					key("r")
					key("9")
					key("x")
					key("enter")
				default:
					for _, k := range []string{"j", "n", "enter", "?", "?", "tab", "tab", "e", "tab", "a"} {
						key(k)
					}
				}
			}
			if m.game.Phase() != engine.PhaseOver {
				t.Fatalf("%s: run did not finish", s.ID)
			}
			for _, k := range []string{"enter", "p", "j", "G", "esc", "enter"} {
				key(k)
			}
			if m.screen != scrMenu {
				t.Errorf("%s: ended on screen %d, want menu", s.ID, m.screen)
			}
		}
	}
}

func TestTooSmall(t *testing.T) {
	m := New(Options{Scenarios: scenarios(t), Store: &store.Store{}, Settings: store.DefaultSettings(), Renderer: plainStyles().r})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if v := m.View(); !strings.Contains(v, "at least 80x24") || !strings.Contains(v, "60x20") {
		t.Errorf("small view = %q", v)
	}
}

func TestDailyRunIsStable(t *testing.T) {
	list := scenarios(t)
	a, sa := DailyRun("2026-09-30", list)
	b, sb := DailyRun("2026-09-30", list)
	if a != b || sa != sb {
		t.Error("daily run differs for the same date")
	}
	if _, sc := DailyRun("2026-10-01", list); sc == sa {
		t.Error("consecutive days share a seed")
	}
}
