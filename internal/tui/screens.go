package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"

	"github.com/butaraul/lgtm/internal/engine"
)

// wrap reflows text: single newlines become spaces, blank lines separate
// paragraphs.
func wrap(s string, w int) []string {
	var out []string
	paras := strings.Split(strings.TrimSpace(s), "\n\n")
	for i, p := range paras {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, wrapWords(strings.Join(strings.Fields(p), " "), w)...)
	}
	return out
}

// wrapKeep keeps line breaks and hard-wraps long lines, for output such as
// test runs.
func wrapKeep(s string, w int) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.ReplaceAll(l, "\t", "    ")
		for runewidth.StringWidth(l) > w {
			head := runewidth.Truncate(l, w, "")
			out = append(out, head)
			l = l[len(head):]
		}
		out = append(out, l)
	}
	return out
}

func wrapWords(s string, w int) []string {
	if s == "" {
		return []string{""}
	}
	var out []string
	cur := ""
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case runewidth.StringWidth(cur)+1+runewidth.StringWidth(word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
		for runewidth.StringWidth(cur) > w {
			head := runewidth.Truncate(cur, w, "")
			out = append(out, head)
			cur = cur[len(head):]
		}
	}
	return append(out, cur)
}

// page centres a column of content with a status line at the bottom.
func (m *Model) page(lines []string, hints string) string {
	w := m.contentWidth()
	left := max(0, (m.w-w)/2)
	h := m.h - 1
	var b strings.Builder
	for i := 0; i < h; i++ {
		if i < len(lines) {
			b.WriteString(fit(strings.Repeat(" ", left)+lines[i], m.w))
		} else {
			b.WriteString(strings.Repeat(" ", m.w))
		}
		b.WriteByte('\n')
	}
	b.WriteString(m.statusLine(hints))
	return b.String()
}

type menuItem struct {
	label, detail string
	run           func(m *Model) tea.Cmd
}

func (m *Model) menuItems() []menuItem {
	var items []menuItem
	for _, s := range m.opt.Scenarios {
		s := s
		best := "—"
		if b, ok := m.progress.Best[s.ID]; ok {
			best = fmt.Sprintf("best %s %d", b.Grade, b.Score)
		}
		items = append(items, menuItem{
			label:  s.Title,
			detail: strings.Repeat("■", s.Difficulty) + strings.Repeat("□", 4-s.Difficulty) + "  " + best,
			run:    func(m *Model) tea.Cmd { m.newRun(s, m.seed(), ""); return nil },
		})
	}
	today := time.Now().UTC().Format("2006-01-02")
	ds, _ := DailyRun(today, m.opt.Scenarios)
	detail := ds.ID
	if b, ok := m.progress.Daily[today]; ok {
		detail += fmt.Sprintf("  done %s %d", b.Grade, b.Score)
	}
	items = append(items, menuItem{
		label:  "Daily challenge " + today,
		detail: detail,
		run: func(m *Model) tea.Cmd {
			s, seed := DailyRun(today, m.opt.Scenarios)
			m.newRun(s, seed, today)
			return nil
		},
	})
	if r := m.saved; r != nil {
		items = append(items, menuItem{
			label:  "Resume " + r.Scenario,
			detail: fmt.Sprintf("seed %d  %d actions", r.Seed, len(r.Actions)),
			run:    func(m *Model) tea.Cmd { m.resume(r); return nil },
		})
	}
	items = append(items,
		menuItem{label: "Settings", run: func(m *Model) tea.Cmd { m.screen = scrSettings; return nil }},
		menuItem{label: "Quit", run: func(*Model) tea.Cmd { return tea.Quit }},
	)
	return items
}

func (m *Model) menuKey(k string) tea.Cmd {
	items := m.menuItems()
	switch k {
	case "j", "down":
		m.menuIdx = (m.menuIdx + 1) % len(items)
	case "k", "up":
		m.menuIdx = (m.menuIdx + len(items) - 1) % len(items)
	case "h", "left":
		m.difficulty = cycleDifficulty(m.difficulty, -1)
	case "l", "right":
		m.difficulty = cycleDifficulty(m.difficulty, 1)
	case "enter", " ":
		return items[m.menuIdx].run(m)
	case "q":
		return tea.Quit
	}
	return nil
}

var difficulties = []engine.Difficulty{engine.Easy, engine.Normal, engine.Hard}

func cycleDifficulty(d engine.Difficulty, step int) engine.Difficulty {
	for i, x := range difficulties {
		if x == d {
			return difficulties[(i+step+len(difficulties))%len(difficulties)]
		}
	}
	return engine.Normal
}

func (m *Model) menuView() string {
	st := m.st
	w := m.contentWidth()
	lines := []string{
		"",
		st.title.Render("lgtm"),
		st.dim.Render("An agent writes the code. You decide what ships."),
		"",
	}
	items := m.menuItems()
	m.menuIdx = min(m.menuIdx, len(items)-1)
	for i, it := range items {
		if i == len(m.opt.Scenarios) {
			lines = append(lines, "")
		}
		label := truncRight(it.label, w-26)
		detail := st.dim.Render(it.detail)
		if i == m.menuIdx {
			lines = append(lines, st.cursor.Render("▌ ")+fit(st.accent.Render(label), w-26)+detail)
		} else {
			lines = append(lines, "  "+fit(st.base.Render(label), w-26)+detail)
		}
	}
	lines = append(lines, "", st.dim.Render("difficulty ")+st.dim.Render("◂ ")+st.accent.Render(string(m.difficulty))+st.dim.Render(" ▸"))
	if m.progress.Runs > 0 {
		lines = append(lines, st.faint.Render(fmt.Sprintf("%d runs finished", m.progress.Runs)))
	}
	lines = append(lines, "", "")
	for _, l := range wrap("Each contract is one client and one agent. The agent submits diffs. You accept, reject, or read first. Reading costs time. Not reading costs more, later.", w) {
		lines = append(lines, st.dim.Render(l))
	}
	return m.page(lines, "j/k move  h/l difficulty  enter select  ? help  q quit")
}

func (m *Model) settingsKey(k string) tea.Cmd {
	switch k {
	case "j", "down", "k", "up":
		m.setIdx = 1 - m.setIdx
	case "enter", " ", "h", "l", "left", "right":
		s := &m.opt.Settings
		if m.setIdx == 0 {
			s.Motion = !s.Motion
		} else {
			next := map[string]string{"auto": "16", "16": "none", "none": "auto"}
			s.Color = next[s.Color]
			if s.Color == "" {
				s.Color = "auto"
			}
			m.applyColor()
		}
		if err := m.opt.Store.SaveSettings(*s); err != nil {
			m.flash = "Could not save settings: " + err.Error()
		}
	case "esc", "q":
		m.screen = scrMenu
	}
	return nil
}

// applyColor sets the colour profile from settings. Auto keeps whatever the
// terminal reported at startup.
func (m *Model) applyColor() {
	switch m.opt.Settings.Color {
	case "16":
		m.st.r.SetColorProfile(termenv.ANSI)
	case "none":
		m.st.r.SetColorProfile(termenv.Ascii)
	default:
		m.st.r.SetColorProfile(termenv.EnvColorProfile())
	}
	m.rv.dirty = true
	m.rv.hl = map[int][][]seg{}
}

func (m *Model) settingsView() string {
	st := m.st
	motion := "on"
	if !m.opt.Settings.Motion {
		motion = "off"
	}
	rows := [][2]string{
		{"Motion", motion},
		{"Colour", m.opt.Settings.Color},
	}
	lines := []string{"", st.title.Render("Settings"), ""}
	for i, r := range rows {
		prefix := "  "
		if i == m.setIdx {
			prefix = st.cursor.Render("▌ ")
		}
		lines = append(lines, prefix+fit(st.base.Render(r[0]), 12)+st.accent.Render(r[1]))
	}
	lines = append(lines, "",
		st.dim.Render("Motion streams diffs in and eases the gauges."),
		st.dim.Render("Colour: auto detects the terminal; 16 forces basic colours."),
		st.dim.Render("--no-color on the command line overrides this for one run."),
	)
	return m.page(lines, "j/k move  enter change  esc back")
}

func (m *Model) briefView() string {
	st := m.st
	g := m.game
	s := g.Scenario()
	w := m.contentWidth()
	lines := []string{
		"",
		st.title.Render(s.Title) + st.dim.Render(fmt.Sprintf("   difficulty %d of 4", s.Difficulty)),
		"",
		st.section.Render("CLIENT"),
		st.bright.Render(s.Client.Name) + st.dim.Render(", "+s.Client.Role),
	}
	for _, l := range wrap(s.Client.About, w) {
		lines = append(lines, st.dim.Render(l))
	}
	lines = append(lines, "", st.section.Render("BRIEF"))
	for _, l := range wrap(s.Brief, w-2) {
		lines = append(lines, st.base.Render("  "+l))
	}
	lines = append(lines, "", st.section.Render("REQUIREMENTS, AS YOU READ THEM"))
	for _, r := range g.Requirements() {
		lines = append(lines, st.base.Render("○ "+r.Text))
	}
	res := g.Resources()
	lines = append(lines, "", st.section.Render("BUDGET"),
		st.base.Render(fmt.Sprintf("Clock %s, deadline %s. Tokens %s. Context window %s.",
			engine.FormatDuration(res.ClockBudget), engine.FormatClock(res.ClockBudget), kTokens(res.TokenBudget), kTokens(s.Budget.Context))),
		"",
		st.dim.Render(fmt.Sprintf("seed %d  %s", m.run.Seed, m.run.Difficulty)),
	)
	return m.page(clampLines(lines, m.h-1), "enter start  esc back  ? help")
}

func (m *Model) productionView() string {
	lines := []string{""}
	lines = append(lines, m.prod[:m.shown]...)
	hint := "any key skips"
	if m.shown >= len(m.prod) {
		hint = "enter scorecard  q quit"
	}
	// Keep the latest lines on screen.
	if over := len(lines) - (m.h - 1); over > 0 {
		lines = lines[over:]
	}
	return m.page(lines, hint)
}

func (m *Model) resultsView() string {
	return m.page(clampLines(resultsLines(m.st, m.game, m.contentWidth()), m.h-1), "p post-mortem  r replay seed  enter menu  q quit")
}

func (m *Model) postmortemView() string {
	h := m.h - 1
	end := min(len(m.pm), m.pmOff+h)
	lines := m.pm[m.pmOff:end]
	pct := 100
	if len(m.pm) > h {
		pct = 100 * end / len(m.pm)
	}
	return m.page(lines, fmt.Sprintf("j/k scroll  space page  esc scorecard  q quit  %d%%", pct))
}

func (m *Model) helpView() string {
	st := m.st
	keys := [][2]string{
		{"j k / arrows", "move the cursor; scroll the log"},
		{"n p", "next or previous hunk"},
		{"enter / space", "open or close a hunk. Opening an unread hunk costs time"},
		{"", "on a file header: every hunk in the file"},
		{"g G", "top, bottom"},
		{"tab", "switch between the diff and the log"},
		{"a", "accept the candidate"},
		{"r", "reject it, with a reason and an optional note"},
		{"t", "run the tests. Costs time and tokens. Passing is not proof"},
		{"e", "ask the agent to explain. It is sometimes wrong, and sure"},
		{"s", "summarise and restart the agent's context"},
		{"D", "ship now, with whatever has been accepted"},
		{"q", "quit; a run in progress is saved"},
	}
	lines := []string{"", st.title.Render("Keys"), ""}
	for _, k := range keys {
		lines = append(lines, st.key.Render(fit(k[0], 16))+st.base.Render(k[1]))
	}
	w := m.contentWidth()
	lines = append(lines, "", st.title.Render("How it works"), "")
	for _, p := range []string{
		"Hunks start closed. ○ is unread, ● is read. Accepting unread code is allowed.",
		"A correct rejection reason gets the flaw fixed. A wrong one may not. Rejecting clean work costs time and gets the same work back.",
		"The agent's context fills as the session goes on. Below about 40% it starts forgetting earlier constraints. Summarising restores it, for a price.",
		"When you ship, production decides.",
	} {
		for _, l := range wrap(p, w) {
			lines = append(lines, st.dim.Render(l))
		}
	}
	return m.page(clampLines(lines, m.h-1), "any key closes")
}
