package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/butaraul/lgtm/internal/engine"
	"github.com/butaraul/lgtm/internal/scenario"
)

// review is the UI state for the review screen.
type review struct {
	seq      int // candidate the diff pane is showing
	open     []bool
	hl       map[int][][]seg
	rows     []row
	cursor   int
	offset   int
	revealed int
	msgShown int
	dirty    bool

	showLog bool
	logOff  int

	picker *picker
	gauges [3]float64 // context health, tokens used, clock used; as drawn
	primed bool
}

type picker struct {
	idx    int
	noting bool
	reason string
	input  textinput.Model
}

// reasons in picker order: the flaw types, then "something else".
func reasons() []string {
	out := make([]string, 0, len(scenario.FlawTypes)+1)
	for _, t := range scenario.FlawTypes {
		out = append(out, string(t))
	}
	return append(out, engine.ReasonOther)
}

func reasonText(r string) string {
	if r == engine.ReasonOther {
		return "Something else"
	}
	return scenario.FlawType(r).Label()
}

// layout sizes.
func (m *Model) sideWidth() int  { return max(32, min(44, m.w*36/100)) }
func (m *Model) leftWidth() int  { return m.w - m.sideWidth() - 1 }
func (m *Model) bodyHeight() int { return m.h - 2 }

// sync catches the review state up with the engine after an action.
func (rv *review) sync(m *Model) {
	c := m.game.Current()
	if c != nil && c.Seq != rv.seq {
		rv.seq = c.Seq
		rv.open = make([]bool, c.Diff.NumHunks())
		for i := range rv.open {
			// Resubmissions keep what was already read open.
			rv.open[i] = c.Resubmitted && m.game.Inspected(i)
		}
		rv.hl = map[int][][]seg{}
		rv.cursor, rv.offset = 0, 0
		rv.revealed, rv.msgShown = 0, 0
		if !m.opt.Settings.Motion {
			rv.revealed, rv.msgShown = math.MaxInt32, math.MaxInt32
		}
	}
	rv.dirty = true
	if !rv.primed || !m.opt.Settings.Motion {
		rv.gauges = gaugeTargets(m.game)
		rv.primed = true
	}
}

func gaugeTargets(g *engine.Game) [3]float64 {
	r := g.Resources()
	return [3]float64{
		r.Health,
		math.Min(1, float64(r.TokensSpent)/float64(r.TokenBudget)),
		math.Min(1, float64(r.ClockUsed)/float64(r.ClockBudget)),
	}
}

func (rv *review) rebuild(m *Model) {
	rv.dirty = false
	c := m.game.Current()
	if c == nil {
		rv.rows = nil
		return
	}
	read := make([]bool, c.Diff.NumHunks())
	for i := range read {
		read[i] = m.game.Inspected(i)
	}
	rv.rows = diffRows(m.st, c.Diff, rv.open, read, m.leftWidth()-1, rv.hl)
	rv.cursor = max(0, min(rv.cursor, len(rv.rows)-1))
	rv.scroll(m.bodyHeight())
}

func (rv *review) scroll(h int) {
	if rv.cursor < rv.offset {
		rv.offset = rv.cursor
	}
	if rv.cursor >= rv.offset+h {
		rv.offset = rv.cursor - h + 1
	}
	rv.offset = max(0, min(rv.offset, len(rv.rows)-1))
}

func (rv *review) animating(m *Model) bool {
	c := m.game.Current()
	if c != nil && (rv.revealed < len(rv.rows) || rv.msgShown < len(c.Message)) {
		return true
	}
	t := gaugeTargets(m.game)
	for i := range t {
		if math.Abs(t[i]-rv.gauges[i]) > 0.002 {
			return true
		}
	}
	return false
}

func (rv *review) animate(m *Model) {
	rv.revealed++
	rv.msgShown += 9
	t := gaugeTargets(m.game)
	for i := range t {
		rv.gauges[i] += (t[i] - rv.gauges[i]) * 0.2
		if math.Abs(t[i]-rv.gauges[i]) <= 0.002 {
			rv.gauges[i] = t[i]
		}
	}
}

func (rv *review) finishStream() {
	rv.revealed, rv.msgShown = math.MaxInt32, math.MaxInt32
}

func (m *Model) reviewKey(k string) tea.Cmd {
	rv := &m.rv
	rv.finishStream()
	if rv.dirty {
		rv.rebuild(m)
	}
	g := m.game
	if g.Phase() == engine.PhaseShip {
		switch k {
		case "enter", "y":
			m.confirm = &confirm{prompt: "Ship it?", yes: func(m *Model) tea.Cmd { m.act(engine.Action{Kind: engine.ActShip}); return nil }}
		case "h":
			m.confirm = &confirm{prompt: "Hold? Nothing ships and the client gets nothing.", yes: func(m *Model) tea.Cmd { m.act(engine.Action{Kind: engine.ActHold}); return nil }}
		case "tab":
			rv.showLog = !rv.showLog
		case "j", "down":
			rv.logOff++
		case "k", "up":
			rv.logOff--
		case "q":
			return m.confirmQuit()
		}
		return nil
	}

	page := m.bodyHeight()
	if rv.showLog {
		switch k {
		case "j", "down":
			rv.logOff++
			return nil
		case "k", "up":
			rv.logOff--
			return nil
		case "ctrl+d", "pgdown":
			rv.logOff += page / 2
			return nil
		case "ctrl+u", "pgup":
			rv.logOff -= page / 2
			return nil
		case "g", "home":
			rv.logOff = 0
			return nil
		case "G", "end":
			rv.logOff = math.MaxInt32
			return nil
		}
	} else {
		switch k {
		case "j", "down":
			rv.cursor++
		case "k", "up":
			rv.cursor--
		case "ctrl+d", "pgdown":
			rv.cursor += page / 2
		case "ctrl+u", "pgup":
			rv.cursor -= page / 2
		case "g", "home":
			rv.cursor = 0
		case "G", "end":
			rv.cursor = len(rv.rows) - 1
		case "n":
			for i := rv.cursor + 1; i < len(rv.rows); i++ {
				if rv.rows[i].kind == rowHunk {
					rv.cursor = i
					break
				}
			}
		case "p":
			for i := rv.cursor - 1; i >= 0; i-- {
				if rv.rows[i].kind == rowHunk {
					rv.cursor = i
					break
				}
			}
		case "enter", " ":
			m.toggle()
		}
		rv.cursor = max(0, min(rv.cursor, len(rv.rows)-1))
		rv.scroll(page)
	}

	switch k {
	case "tab":
		rv.showLog = !rv.showLog
		if rv.showLog {
			rv.logOff = math.MaxInt32
		}
	case "a":
		m.act(engine.Action{Kind: engine.ActAccept})
	case "r":
		rv.picker = &picker{}
	case "t":
		if m.act(engine.Action{Kind: engine.ActTests}) {
			m.showLatest()
			log := g.Log()
			if e := log[len(log)-1]; e.Kind == engine.EvTests && e.Pass {
				m.flash = "Tests passed. tab returns to the diff."
			} else {
				m.flash = "Tests failed. tab returns to the diff."
			}
		}
	case "e":
		if m.act(engine.Action{Kind: engine.ActExplain}) {
			m.showLatest()
			m.flash = "Explanation in the log. tab returns to the diff."
		}
	case "s":
		c := g.CostOf(engine.Action{Kind: engine.ActSummarise})
		m.confirm = &confirm{
			prompt: fmt.Sprintf("Summarise and restart the agent's context? %s, ~%s tokens.", engine.FormatDuration(c.Minutes), kTokens(c.Tokens)),
			yes: func(m *Model) tea.Cmd {
				if m.act(engine.Action{Kind: engine.ActSummarise}) {
					log := m.game.Log()
					m.flash = log[len(log)-1].Text
				}
				return nil
			},
		}
	case "D":
		m.confirm = &confirm{prompt: "Ship now? Unfinished work stays unfinished.", yes: func(m *Model) tea.Cmd {
			m.act(engine.Action{Kind: engine.ActShip})
			return nil
		}}
	case "q":
		return m.confirmQuit()
	}
	return nil
}

func (m *Model) confirmQuit() tea.Cmd {
	m.confirm = &confirm{prompt: "Quit? The run is saved and resumes from the menu.", yes: func(*Model) tea.Cmd { return tea.Quit }}
	return nil
}

// act applies an action and reports whether it succeeded.
func (m *Model) act(a engine.Action) bool {
	if err := m.apply(a); err != nil {
		m.flash = errText(err)
		return false
	}
	if m.screen != scrReview {
		return true
	}
	if a.Kind == engine.ActAccept || a.Kind == engine.ActReject {
		log := m.game.Log()
		for i := len(log) - 1; i >= 0; i-- {
			if log[i].Kind == engine.EvYou {
				m.flash = log[i].Text
				break
			}
		}
		m.rv.showLog = false
	}
	m.rv.sync(m)
	return true
}

func (m *Model) showLatest() {
	m.rv.showLog = true
	m.rv.logOff = math.MaxInt32
}

// toggle opens or closes the hunk under the cursor, or every hunk in the
// file under the cursor. Opening an unread hunk costs clock time.
func (m *Model) toggle() {
	rv := &m.rv
	if len(rv.rows) == 0 {
		return
	}
	r := rv.rows[rv.cursor]
	var hunks []int
	switch {
	case r.kind == rowFile:
		for _, x := range rv.rows {
			if x.kind == rowHunk && x.file == r.file {
				hunks = append(hunks, x.hunk)
			}
		}
	case r.hunk >= 0:
		hunks = []int{r.hunk}
	default:
		return
	}
	anyClosed := false
	for _, h := range hunks {
		anyClosed = anyClosed || !rv.open[h]
	}
	minutes := 0
	for _, h := range hunks {
		if !anyClosed {
			rv.open[h] = false
			continue
		}
		if !m.game.Inspected(h) {
			minutes += m.game.CostOf(engine.Action{Kind: engine.ActInspect, Hunk: h}).Minutes
			if !m.act(engine.Action{Kind: engine.ActInspect, Hunk: h}) {
				return
			}
			if m.screen != scrReview {
				return
			}
		}
		rv.open[h] = true
	}
	if minutes > 0 {
		m.flash = fmt.Sprintf("Read. %s.", engine.FormatDuration(minutes))
	}
	rv.rebuild(m)
	// Keep the cursor on the row that was toggled.
	for i, x := range rv.rows {
		if x.kind == r.kind && x.file == r.file && x.hunk == r.hunk {
			rv.cursor = i
			break
		}
	}
	rv.scroll(m.bodyHeight())
}

func (m *Model) pickerKey(msg tea.KeyMsg) tea.Cmd {
	p := m.rv.picker
	k := msg.String()
	rs := reasons()
	if p.noting {
		switch k {
		case "esc":
			p.noting = false
			return nil
		case "enter":
			m.rv.picker = nil
			m.act(engine.Action{Kind: engine.ActReject, Reason: p.reason, Note: p.input.Value()})
			return nil
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return cmd
	}
	switch k {
	case "esc", "q":
		m.rv.picker = nil
	case "j", "down":
		p.idx = (p.idx + 1) % len(rs)
	case "k", "up":
		p.idx = (p.idx + len(rs) - 1) % len(rs)
	case "enter":
		p.reason = rs[p.idx]
		p.noting = true
		p.input = m.newInput()
	default:
		if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			if i := int(k[0] - '1'); i < len(rs) {
				p.idx = i
				p.reason = rs[i]
				p.noting = true
				p.input = m.newInput()
			}
		}
	}
	return nil
}

func (m *Model) reviewView() string {
	rv := &m.rv
	if rv.dirty {
		rv.rebuild(m)
	}
	lw, sw, bh := m.leftWidth(), m.sideWidth(), m.bodyHeight()

	var left string
	switch {
	case m.game.Phase() == engine.PhaseShip && !rv.showLog:
		left = m.shipPane(lw, bh)
	case rv.showLog:
		left = m.logPane(lw, bh)
	default:
		left = viewRows(m.st, rv.rows, rv.cursor, rv.offset, bh, rv.revealed)
	}
	side := m.sidebar(sw, bh)

	leftLines := strings.Split(left, "\n")
	sideLines := strings.Split(side, "\n")
	sep := m.st.rule.Render("│")
	var b strings.Builder
	b.WriteString(m.topBar())
	for i := 0; i < bh; i++ {
		b.WriteByte('\n')
		b.WriteString(fit(line(leftLines, i), lw))
		b.WriteString(sep)
		b.WriteString(fit(line(sideLines, i), sw))
	}
	b.WriteByte('\n')
	hints := "j/k move  n/p hunk  enter read  tab log  a accept  r reject  ? help"
	switch {
	case m.game.Phase() == engine.PhaseShip:
		hints = "enter ship  h hold  tab log  ? help  q quit"
	case rv.showLog:
		hints = "j/k scroll  tab diff  a accept  r reject  t tests  e explain  ? help"
	}
	b.WriteString(m.statusLine(hints))
	return b.String()
}

func line(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// fit truncates or pads an ANSI string to exactly w cells.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "")
	if n := ansi.StringWidth(s); n < w {
		s += strings.Repeat(" ", w-n)
	}
	return s
}

func (m *Model) topBar() string {
	s := m.game.Scenario()
	cur, total := m.game.Progress()
	left := m.st.title.Render(" lgtm") + "  " + m.st.bright.Render(s.Title)
	if cur > 0 {
		left += m.st.dim.Render(fmt.Sprintf("  turn %d/%d", cur, total))
	}
	right := fmt.Sprintf("seed %d  %s", m.run.Seed, m.run.Difficulty)
	if m.run.Daily != "" {
		right = "daily " + m.run.Daily + "  " + right
	}
	pane := "diff"
	if m.rv.showLog {
		pane = "log"
	}
	right = m.st.dim.Render(right+"  ") + m.st.accent.Render(pane) + " "
	gap := m.w - ansi.StringWidth(left) - ansi.StringWidth(right)
	return fit(left+strings.Repeat(" ", max(1, gap))+right, m.w)
}

func kTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return fmt.Sprint(n)
}

// sidebar draws the right column: client, requirements, agent message,
// gauges and actions.
func (m *Model) sidebar(w, h int) string {
	st := m.st
	g := m.game
	s := g.Scenario()
	inner := w - 2
	pad := func(x string) string { return " " + x }
	rule := st.rule.Render(" " + strings.Repeat("─", inner))

	var top []string
	top = append(top, pad(st.section.Render("CLIENT")+"  "+st.bright.Render(truncRight(s.Client.Name, inner-8))))
	for _, l := range wrap(s.Client.Role, inner) {
		top = append(top, pad(st.dim.Render(l)))
		break
	}
	top = append(top, rule, pad(st.section.Render("REQUIREMENTS")))
	for _, r := range g.Requirements() {
		mark := st.faint.Render("○")
		text := st.dim.Render(truncRight(r.Text, inner-2))
		if r.Claimed {
			mark = st.base.Render("●")
			text = st.base.Render(truncRight(r.Text, inner-2))
		}
		top = append(top, pad(mark+" "+text))
	}
	top = append(top, rule)

	bottom := []string{rule}
	res := g.Resources()
	gw := max(6, inner-17)
	health := m.rv.gauges[0]
	hs := st.base
	switch {
	case health < 0.25:
		hs = st.danger
	case health < 0.5:
		hs = st.accent
	}
	bottom = append(bottom,
		pad(st.dim.Render("CONTEXT ")+bar(st, health, gw, hs)+hs.Render(fmt.Sprintf(" %3d%%", int(math.Round(res.Health*100))))),
	)
	ts := st.base
	if m.rv.gauges[1] > 0.8 {
		ts = st.danger
	}
	bottom = append(bottom,
		pad(st.dim.Render("TOKENS  ")+bar(st, 1-m.rv.gauges[1], gw, ts)+ts.Render(fmt.Sprintf(" %4s", kTokens(res.TokenBudget-res.TokensSpent)))),
	)
	cs := st.base
	left := res.ClockBudget - res.ClockUsed
	if left < 60 {
		cs = st.danger
	}
	bottom = append(bottom,
		pad(st.dim.Render("CLOCK   ")+bar(st, 1-m.rv.gauges[2], gw, cs)+cs.Render(fmt.Sprintf(" %4s", engine.FormatDuration(left)))),
		pad(st.faint.Render(engine.FormatClock(res.ClockUsed)+" · deadline "+engine.FormatClock(res.ClockBudget))),
		rule,
	)
	bottom = append(bottom, m.actionLines(inner)...)

	midH := h - len(top) - len(bottom)
	mid := m.middle(inner, midH)
	out := append(top, mid...)
	out = append(out, bottom...)
	if len(out) > h {
		out = out[len(out)-h:]
	}
	return strings.Join(out, "\n")
}

// middle is the flexible part of the sidebar: the agent's message, or the
// reject picker.
func (m *Model) middle(w, h int) []string {
	st := m.st
	var out []string
	pad := func(x string) string { return " " + x }
	if p := m.rv.picker; p != nil {
		if p.noting {
			out = append(out, pad(st.section.Render("REJECT")+"  "+st.accent.Render(reasonText(p.reason))))
			out = append(out, pad(st.dim.Render("Note for the agent:")))
			p.input.Width = w - 3
			out = append(out, pad(p.input.View()))
			out = append(out, pad(st.faint.Render("enter send  esc back")))
		} else {
			out = append(out, pad(st.section.Render("REJECT. WHY?")))
			for i, r := range reasons() {
				label := fmt.Sprintf("%d %s", i+1, reasonText(r))
				if i == p.idx {
					out = append(out, st.cursor.Render("▌")+st.accent.Render(truncRight(label, w)))
				} else {
					out = append(out, pad(st.base.Render(truncRight(label, w))))
				}
			}
			out = append(out, pad(st.faint.Render("1-9 or enter  esc cancel")))
		}
		return clampLines(out, h)
	}
	c := m.game.Current()
	if c == nil {
		out = append(out, pad(st.section.Render("AGENT")))
		out = append(out, pad(st.dim.Render("Idle. Nothing left to build.")))
		return clampLines(out, h)
	}
	head := st.section.Render("AGENT")
	if c.Resubmitted {
		head += st.dim.Render("  resubmitted")
	}
	out = append(out, pad(head))
	out = append(out, pad(st.bright.Render(truncRight(c.Title, w))))
	a, r := c.Diff.Stats()
	out = append(out, pad(st.dim.Render(fmt.Sprintf("%d files  ", len(c.Diff.Files)))+statStr(st, a, r)+st.dim.Render(fmt.Sprintf("  %d hunks", c.Diff.NumHunks()))))
	msg := c.Message
	if m.rv.msgShown < len(msg) {
		msg = msg[:m.rv.msgShown]
	}
	lines := wrap(msg, w)
	room := h - len(out)
	if len(lines) > room && room > 0 {
		lines = lines[:room]
		lines[room-1] = truncRight(lines[room-1], w-1) + "…"
	}
	for _, l := range lines {
		out = append(out, pad(st.base.Render(l)))
	}
	return clampLines(out, h)
}

func clampLines(lines []string, h int) []string {
	if h <= 0 {
		return nil
	}
	if len(lines) > h {
		return lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines
}

func (m *Model) actionLines(w int) []string {
	st := m.st
	g := m.game
	cost := func(k engine.ActionKind) string {
		c := g.CostOf(engine.Action{Kind: k})
		return st.faint.Render(engine.FormatDuration(c.Minutes))
	}
	item := func(key, label string, extra string) string {
		return st.key.Render(key) + " " + st.base.Render(label) + " " + extra
	}
	col := (w - 1) / 2
	two := func(a, b string) string { return " " + fit(a, col) + fit(b, w-col) }
	if g.Phase() == engine.PhaseShip {
		return []string{
			two(item("enter", "ship", ""), item("h", "hold", "")),
			two(item("tab", "log", ""), item("q", "quit", "")),
			"",
		}
	}
	return []string{
		two(item("a", "accept", ""), item("r", "reject", cost(engine.ActReject))),
		two(item("t", "tests", cost(engine.ActTests)), item("e", "explain", cost(engine.ActExplain))),
		two(item("s", "summarise", cost(engine.ActSummarise)), item("D", "ship now", "")),
	}
}

func bar(st *styles, frac float64, w int, fill interface{ Render(...string) string }) string {
	frac = math.Max(0, math.Min(1, frac))
	n := int(math.Round(frac * float64(w)))
	return fill.Render(strings.Repeat("━", n)) + st.faint.Render(strings.Repeat("─", w-n))
}

// logPane renders the event log, scrolled.
func (m *Model) logPane(w, h int) string {
	lines := logLines(m.st, m.game, w-2)
	maxOff := max(0, len(lines)-h)
	m.rv.logOff = max(0, min(m.rv.logOff, maxOff))
	end := min(len(lines), m.rv.logOff+h)
	out := make([]string, 0, h)
	for _, l := range lines[m.rv.logOff:end] {
		out = append(out, " "+l)
	}
	return strings.Join(clampLines(out, h), "\n")
}

func logLines(st *styles, g *engine.Game, w int) []string {
	var out []string
	client := g.Scenario().Client.Name
	for _, e := range g.Log() {
		var label string
		body := st.base
		switch e.Kind {
		case engine.EvBrief:
			label = st.accent.Render("BRIEF") + st.dim.Render("  "+client)
		case engine.EvClient:
			label = st.accent.Render("CLIENT") + st.dim.Render("  "+client)
		case engine.EvAgent:
			label = st.bright.Render("AGENT") + st.dim.Render(fmt.Sprintf("  #%d", e.Seq))
		case engine.EvTests:
			if e.Pass {
				label = st.bright.Render("TESTS") + st.dim.Render("  passed")
			} else {
				label = st.bright.Render("TESTS") + st.danger.Render("  failed")
			}
			body = st.dim
		case engine.EvExplain:
			label = st.bright.Render("EXPLAIN") + st.dim.Render(fmt.Sprintf("  #%d", e.Seq))
		case engine.EvYou:
			label = st.accent.Render("YOU")
		case engine.EvSystem:
			label = st.dim.Render("SYSTEM")
			body = st.dim
		}
		out = append(out, st.faint.Render(engine.FormatClock(e.Clock))+"  "+label)
		for _, l := range wrapKeep(e.Text, w-2) {
			out = append(out, "  "+body.Render(l))
		}
		out = append(out, "")
	}
	return out
}

// shipPane summarises the build when there is nothing left to review.
func (m *Model) shipPane(w, h int) string {
	st := m.st
	g := m.game
	out := []string{"", " " + st.title.Render("Build complete."), ""}
	for _, l := range wrap("The agent has nothing left to submit. Ship what was accepted, or hold and ship nothing.", w-4) {
		out = append(out, " "+st.base.Render(l))
	}
	out = append(out, "", " "+st.section.Render("REQUIREMENTS, AS CLAIMED"))
	for _, r := range g.Requirements() {
		if r.Claimed {
			out = append(out, " "+st.base.Render("● "+truncRight(r.Text, w-6)))
		} else {
			out = append(out, " "+st.dim.Render("○ "+truncRight(r.Text, w-6)+"  not built"))
		}
	}
	res := g.Resources()
	out = append(out, "", " "+st.dim.Render(fmt.Sprintf("Clock left %s. Tokens left %s.", engine.FormatDuration(res.ClockBudget-res.ClockUsed), kTokens(res.TokenBudget-res.TokensSpent))))
	return strings.Join(clampLines(out, h), "\n")
}
