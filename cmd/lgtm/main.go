// Command lgtm is a terminal game about supervising an AI coding agent.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/butaraul/lgtm/internal/engine"
	"github.com/butaraul/lgtm/internal/scenario"
	"github.com/butaraul/lgtm/internal/store"
	"github.com/butaraul/lgtm/internal/tui"
)

// version is set at release time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "validate" {
		return validate(args[1:], stdout, stderr)
	}

	fs := flag.NewFlagSet("lgtm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	scenarioID := fs.String("scenario", "", "start this scenario directly (see --list)")
	seed := fs.String("seed", "", "seed for the run; the same seed and inputs give the same run")
	difficulty := fs.String("difficulty", "normal", "easy, normal or hard")
	noColor := fs.Bool("no-color", false, "disable colour")
	reset := fs.Bool("reset", false, "delete saved progress and any run in progress, then exit")
	list := fs.Bool("list", false, "list scenarios and exit")
	daily := fs.Bool("daily", false, "play today's daily challenge: the same run for everyone on this UTC date")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "lgtm %s: an agent writes the code, you decide what ships.\n\n", version)
		fmt.Fprintln(stderr, "Usage:\n  lgtm [flags]\n  lgtm validate [file.yaml ...]\n\nFlags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "lgtm: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "lgtm", version)
		return 0
	}

	scenarios, err := scenario.All()
	if err != nil {
		fmt.Fprintln(stderr, "lgtm: embedded scenarios are invalid:", err)
		return 1
	}
	if *list {
		for _, s := range scenarios {
			fmt.Fprintf(stdout, "%-10s %d/4  %s\n           %s\n", s.ID, s.Difficulty, s.Title, s.Summary)
		}
		return 0
	}

	st, err := store.Open()
	if err != nil {
		fmt.Fprintln(stderr, "lgtm:", err)
		return 1
	}
	if *reset {
		gone, err := st.Reset()
		if err != nil {
			fmt.Fprintln(stderr, "lgtm:", err)
			return 1
		}
		if len(gone) == 0 {
			fmt.Fprintln(stdout, "Nothing to reset.")
		}
		for _, p := range gone {
			fmt.Fprintln(stdout, "Removed", p)
		}
		return 0
	}

	diff, err := engine.ParseDifficulty(*difficulty)
	if err != nil {
		fmt.Fprintln(stderr, "lgtm:", err)
		return 2
	}
	opt := tui.Options{Scenarios: scenarios, Store: st, Difficulty: diff, Scenario: *scenarioID}
	if *scenarioID != "" && scenario.Find(scenarios, *scenarioID) == nil {
		fmt.Fprintf(stderr, "lgtm: no scenario %q; try --list\n", *scenarioID)
		return 2
	}
	if *seed != "" {
		n, err := strconv.ParseUint(*seed, 10, 64)
		if err != nil {
			fmt.Fprintf(stderr, "lgtm: seed must be a non-negative integer, got %q\n", *seed)
			return 2
		}
		opt.Seed, opt.SeedSet = n, true
		if opt.Scenario == "" && !*daily {
			fmt.Fprintln(stderr, "lgtm: --seed needs --scenario")
			return 2
		}
	}
	if *daily {
		opt.Daily = time.Now().UTC().Format("2006-01-02")
	}

	opt.Settings, err = st.Settings()
	if err != nil {
		fmt.Fprintln(stderr, "lgtm: settings unreadable, using defaults:", err)
		opt.Settings = store.DefaultSettings()
	}
	r := lipgloss.DefaultRenderer()
	switch {
	case *noColor || os.Getenv("NO_COLOR") != "" || opt.Settings.Color == "none":
		r.SetColorProfile(termenv.Ascii)
	case opt.Settings.Color == "16":
		r.SetColorProfile(termenv.ANSI)
	}
	opt.Renderer = r

	p := tea.NewProgram(tui.New(opt), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(stderr, "lgtm:", err)
		return 1
	}
	return 0
}

// validate checks every embedded scenario, plus any files given.
func validate(files []string, stdout, stderr io.Writer) int {
	failed := false
	list, err := scenario.All()
	if err != nil {
		fmt.Fprintln(stderr, err)
		failed = true
	} else {
		for _, s := range list {
			fmt.Fprintf(stdout, "ok  %-10s %d turns, %d changes\n", s.ID, len(s.Turns), len(s.Changes))
		}
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(stderr, err)
			failed = true
			continue
		}
		s, err := scenario.Load(f, data)
		if err != nil {
			fmt.Fprintln(stderr, err)
			failed = true
			continue
		}
		fmt.Fprintf(stdout, "ok  %-10s %s\n", s.ID, f)
	}
	if failed {
		return 1
	}
	return 0
}
