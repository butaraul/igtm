package tui

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/butaraul/lgtm/internal/engine"
	"github.com/butaraul/lgtm/internal/scenario"
	"github.com/butaraul/lgtm/internal/store"
)

// Minimum terminal size.
const (
	MinWidth  = 80
	MinHeight = 24
)

// Options configure the app. Scenario, Seed and Daily start a run straight
// away instead of showing the menu.
type Options struct {
	Scenarios  []*scenario.Scenario
	Store      *store.Store
	Settings   store.Settings
	Renderer   *lipgloss.Renderer
	Scenario   string
	Seed       uint64
	SeedSet    bool
	Difficulty engine.Difficulty
	Daily      string // YYYY-MM-DD when playing the daily challenge
}

// DailyRun picks the scenario and seed for a date. Everyone playing on the
// same UTC date gets the same run.
func DailyRun(date string, list []*scenario.Scenario) (*scenario.Scenario, uint64) {
	h := fnv.New64a()
	h.Write([]byte("lgtm-daily-" + date))
	x := h.Sum64()
	return list[x%uint64(len(list))], x%999_983 + 1
}

type screen uint8

const (
	scrMenu screen = iota
	scrSettings
	scrBrief
	scrReview
	scrProduction
	scrResults
	scrPostmortem
)

type tickMsg struct{}

const frame = 33 * time.Millisecond

func tick() tea.Cmd { return tea.Tick(frame, func(time.Time) tea.Msg { return tickMsg{} }) }

type confirm struct {
	prompt string
	yes    func(m *Model) tea.Cmd
}

// Model is the root Bubble Tea model.
type Model struct {
	opt      Options
	st       *styles
	w, h     int
	screen   screen
	help     bool
	confirm  *confirm
	flash    string
	ticking  bool
	progress store.Progress

	menuIdx    int
	setIdx     int
	difficulty engine.Difficulty
	saved      *store.Run

	game *engine.Game
	run  store.Run
	rv   review

	prod  []string
	shown int // production lines revealed
	delay int // ticks until the next production line
	pm    []string
	pmOff int
}

// New builds the model.
func New(opt Options) *Model {
	r := opt.Renderer
	if r == nil {
		r = lipgloss.DefaultRenderer()
	}
	m := &Model{opt: opt, st: newStyles(r), difficulty: opt.Difficulty}
	if m.difficulty == "" {
		m.difficulty = engine.Normal
	}
	var err error
	m.progress, err = opt.Store.Progress()
	m.saved, _ = opt.Store.Run()
	if err != nil {
		m.flash = "Progress file unreadable: " + err.Error()
	}
	switch {
	case opt.Daily != "":
		s, seed := DailyRun(opt.Daily, opt.Scenarios)
		m.newRun(s, seed, opt.Daily)
	case opt.Scenario != "":
		if s := scenario.Find(opt.Scenarios, opt.Scenario); s != nil {
			m.newRun(s, m.seed(), "")
		}
	}
	return m
}

func (m *Model) seed() uint64 {
	if m.opt.SeedSet {
		return m.opt.Seed
	}
	return rand.Uint64N(999_983) + 1
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) newRun(s *scenario.Scenario, seed uint64, daily string) {
	diff := m.difficulty
	if daily != "" {
		diff = engine.Normal
	}
	g, err := engine.New(s, engine.Config{Seed: seed, Difficulty: diff})
	if err != nil {
		m.flash = err.Error()
		return
	}
	m.game = g
	m.run = store.Run{Scenario: s.ID, Seed: seed, Difficulty: string(diff), Daily: daily}
	m.rv = review{}
	m.screen = scrBrief
}

func (m *Model) resume(r *store.Run) {
	s := scenario.Find(m.opt.Scenarios, r.Scenario)
	if s == nil {
		m.flash = "Saved run is for a scenario that no longer exists."
		return
	}
	g, err := engine.Replay(s, engine.Config{Seed: r.Seed, Difficulty: engine.Difficulty(r.Difficulty)}, r.Actions)
	if err != nil {
		m.flash = "Saved run no longer replays: " + err.Error()
		_ = m.opt.Store.ClearRun()
		m.saved = nil
		return
	}
	m.game = g
	m.run = *r
	m.rv = review{}
	m.enterGameScreen()
}

// enterGameScreen routes to the screen that matches the engine phase.
func (m *Model) enterGameScreen() {
	switch m.game.Phase() {
	case engine.PhaseBrief:
		m.screen = scrBrief
	case engine.PhaseOver:
		m.startProduction()
	default:
		m.screen = scrReview
		m.rv.sync(m)
	}
}

// apply sends an action to the engine, saves, and reacts to the result.
func (m *Model) apply(a engine.Action) error {
	err := m.game.Apply(a)
	if err != nil {
		return err
	}
	m.run.Actions = m.game.Actions()
	if m.game.Phase() == engine.PhaseOver {
		m.finishRun()
		return nil
	}
	if err := m.opt.Store.SaveRun(m.run); err != nil {
		m.flash = "Could not save: " + err.Error()
	}
	m.saved = &m.run
	return nil
}

func (m *Model) finishRun() {
	_ = m.opt.Store.ClearRun()
	m.saved = nil
	r := m.game.Report()
	p := m.progress
	p.Runs++
	b := store.Best{Score: r.Score, Grade: r.Grade, Seed: m.run.Seed, Difficulty: m.run.Difficulty}
	if old, ok := p.Best[m.run.Scenario]; !ok || r.Score > old.Score {
		p.Best[m.run.Scenario] = b
	}
	if m.run.Daily != "" {
		if old, ok := p.Daily[m.run.Daily]; !ok || r.Score > old.Score {
			p.Daily[m.run.Daily] = b
		}
	}
	m.progress = p
	if err := m.opt.Store.SaveProgress(p); err != nil {
		m.flash = "Could not save progress: " + err.Error()
	}
	m.startProduction()
}

func (m *Model) startProduction() {
	m.prod = productionLines(m.st, m.game, m.contentWidth())
	m.shown, m.delay = 0, 0
	if !m.opt.Settings.Motion {
		m.shown = len(m.prod)
	}
	m.screen = scrProduction
}

func (m *Model) contentWidth() int { return min(m.w, 100) - 4 }

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.rv.dirty = true
		if m.screen == scrPostmortem {
			m.pm = postMortemLines(m.st, m.game, m.contentWidth())
		}
		if m.screen == scrProduction {
			m.prod = productionLines(m.st, m.game, m.contentWidth())
		}
		return m, m.ensureTick()
	case tickMsg:
		m.ticking = false
		m.animate()
		return m, m.ensureTick()
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.w < MinWidth || m.h < MinHeight {
			if msg.String() == "q" {
				return m, tea.Quit
			}
			return m, nil
		}
		cmd := m.key(msg)
		return m, tea.Batch(cmd, m.ensureTick())
	}
	if m.rv.picker != nil && m.rv.picker.noting {
		var cmd tea.Cmd
		m.rv.picker.input, cmd = m.rv.picker.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	if m.help {
		m.help = false
		return nil
	}
	if c := m.confirm; c != nil {
		m.confirm = nil
		if k == "y" || k == "enter" {
			return c.yes(m)
		}
		m.flash = ""
		return nil
	}
	if m.rv.picker != nil {
		return m.pickerKey(msg)
	}
	if k == "?" {
		m.help = true
		return nil
	}
	m.flash = ""
	switch m.screen {
	case scrMenu:
		return m.menuKey(k)
	case scrSettings:
		return m.settingsKey(k)
	case scrBrief:
		switch k {
		case "enter", " ":
			if err := m.apply(engine.Action{Kind: engine.ActStart}); err != nil {
				m.flash = err.Error()
				return nil
			}
			m.screen = scrReview
			m.rv = review{}
			m.rv.sync(m)
		case "esc", "q":
			m.screen = scrMenu
		}
	case scrReview:
		return m.reviewKey(k)
	case scrProduction:
		if m.shown < len(m.prod) {
			m.shown = len(m.prod)
			return nil
		}
		if k == "enter" || k == " " || k == "esc" {
			m.screen = scrResults
		}
		if k == "q" {
			return tea.Quit
		}
	case scrResults:
		switch k {
		case "p":
			m.pm = postMortemLines(m.st, m.game, m.contentWidth())
			m.pmOff = 0
			m.screen = scrPostmortem
		case "r":
			m.newRun(m.game.Scenario(), m.run.Seed, m.run.Daily)
		case "enter", "esc":
			m.screen = scrMenu
		case "q":
			return tea.Quit
		}
	case scrPostmortem:
		page := m.h - 3
		switch k {
		case "j", "down":
			m.pmOff++
		case "k", "up":
			m.pmOff--
		case "ctrl+d", "pgdown", " ":
			m.pmOff += page / 2
		case "ctrl+u", "pgup":
			m.pmOff -= page / 2
		case "g", "home":
			m.pmOff = 0
		case "G", "end":
			m.pmOff = len(m.pm)
		case "esc", "enter", "p":
			m.screen = scrResults
		case "q":
			return tea.Quit
		}
		m.pmOff = max(0, min(m.pmOff, len(m.pm)-page))
	}
	return nil
}

// ensureTick keeps a single tick loop running while anything animates.
func (m *Model) ensureTick() tea.Cmd {
	if m.ticking || !m.opt.Settings.Motion || !m.animating() {
		return nil
	}
	m.ticking = true
	return tick()
}

func (m *Model) animating() bool {
	switch m.screen {
	case scrReview:
		return m.rv.animating(m)
	case scrProduction:
		return m.shown < len(m.prod)
	}
	return false
}

func (m *Model) animate() {
	switch m.screen {
	case scrReview:
		m.rv.animate(m)
	case scrProduction:
		if m.delay > 0 {
			m.delay--
			return
		}
		if m.shown < len(m.prod) {
			m.shown++
			m.delay = 9
			if m.shown < len(m.prod) && m.prod[m.shown-1] == "" {
				m.delay = 3
			}
		}
	}
}

// View implements tea.Model.
func (m *Model) View() string {
	if m.w == 0 {
		return ""
	}
	if m.w < MinWidth || m.h < MinHeight {
		msg := fmt.Sprintf("lgtm needs a terminal of at least %dx%d.\nThis one is %dx%d.\n\nResize, or press q to quit.", MinWidth, MinHeight, m.w, m.h)
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.st.base.Render(msg))
	}
	if m.help {
		return m.helpView()
	}
	var body string
	switch m.screen {
	case scrMenu:
		body = m.menuView()
	case scrSettings:
		body = m.settingsView()
	case scrBrief:
		body = m.briefView()
	case scrReview:
		body = m.reviewView()
	case scrProduction:
		body = m.productionView()
	case scrResults:
		body = m.resultsView()
	case scrPostmortem:
		body = m.postmortemView()
	}
	return body
}

// statusLine is the bottom row: a flash message, a confirm prompt, or key
// hints.
func (m *Model) statusLine(hints string) string {
	text := m.st.dim.Render(hints)
	switch {
	case m.confirm != nil:
		text = m.st.accent.Render(m.confirm.prompt) + m.st.dim.Render("  y confirm  any other key cancels")
	case m.flash != "":
		text = m.st.base.Render(m.flash)
	}
	return m.st.r.NewStyle().Width(m.w).MaxWidth(m.w).Render(" " + text)
}

// newInput is a text input for rejection notes.
func (m *Model) newInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = "optional: what did you see?"
	ti.CharLimit = 200
	ti.PromptStyle = m.st.accent
	ti.TextStyle = m.st.base
	ti.PlaceholderStyle = m.st.faint
	ti.Focus()
	return ti
}

func errText(err error) string {
	switch {
	case errors.Is(err, engine.ErrNoop):
		return "Already done for this candidate."
	case errors.Is(err, engine.ErrInvalid):
		return "Not now."
	}
	return err.Error()
}
