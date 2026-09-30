package tui

import (
	"fmt"
	"strings"

	"github.com/butaraul/lgtm/internal/diff"
	"github.com/butaraul/lgtm/internal/engine"
	"github.com/butaraul/lgtm/internal/scenario"
)

func severityText(st *styles, s scenario.Severity) string {
	label := strings.ToUpper(string(s))
	switch s {
	case scenario.Critical, scenario.High:
		return st.danger.Render(label)
	case scenario.Medium:
		return st.accent.Render(label)
	}
	return st.dim.Render(label)
}

// productionLines is the post-ship playback, one line revealed at a time.
func productionLines(st *styles, g *engine.Game, w int) []string {
	o := g.Outcome()
	res := g.Resources()
	const indent = "             "
	var out []string
	switch o.End {
	case engine.EndHeld:
		out = append(out, st.title.Render("HELD")+st.dim.Render("  "+engine.FormatClock(res.ClockUsed)), "",
			st.base.Render("Nothing shipped."), "",
			st.faint.Render("Day 2 10:00  ")+st.base.Render("The client asked for an update. There was nothing to show."), "",
			st.base.Render("Nothing broke."))
		return out
	case engine.EndDeadline:
		out = append(out, st.danger.Render("DEADLINE")+st.dim.Render("  the client shipped what had been accepted"))
	default:
		out = append(out, st.title.Render("SHIPPED")+st.dim.Render("  "+engine.FormatClock(res.ClockUsed)))
	}
	out = append(out, "", st.faint.Render(engine.FormatIncidentTime(0)+"  ")+st.base.Render("Deployed."), "")

	type ev struct {
		at    int
		lines []string
	}
	var evs []ev
	for _, in := range o.Incidents {
		lines := []string{st.faint.Render(engine.FormatIncidentTime(in.At)+"  ") + severityText(st, in.Severity) + "  " + st.bright.Render(in.Title)}
		for _, l := range wrap(in.Detail, w-len(indent)) {
			lines = append(lines, indent+st.base.Render(l))
		}
		for _, l := range wrap(in.Cost, w-len(indent)) {
			lines = append(lines, indent+st.dim.Render(l))
		}
		evs = append(evs, ev{in.At, lines})
	}
	for i, r := range o.Missing {
		at := (2+i)*1440 - engine.ShipHour*60 + 10*60
		text := fmt.Sprintf("The client asked about %q. It was never built.", r.Text)
		lines := []string{}
		for j, l := range wrap(text, w-len(indent)) {
			prefix := indent
			if j == 0 {
				prefix = st.faint.Render(engine.FormatIncidentTime(at) + "  ")
			}
			lines = append(lines, prefix+st.base.Render(l))
		}
		evs = append(evs, ev{at, lines})
	}
	// Stable insertion sort; the lists are short.
	for i := 1; i < len(evs); i++ {
		for j := i; j > 0 && evs[j].at < evs[j-1].at; j-- {
			evs[j], evs[j-1] = evs[j-1], evs[j]
		}
	}
	for _, e := range evs {
		out = append(out, e.lines...)
		out = append(out, "")
	}
	out = append(out, st.faint.Render("Day 30"), "")
	if len(o.Incidents) == 0 {
		out = append(out, st.bright.Render("Nothing happened."))
	} else {
		out = append(out, st.dim.Render(fmt.Sprintf("%d incident%s.", len(o.Incidents), plural(len(o.Incidents)))))
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// resultsLines is the scorecard.
func resultsLines(st *styles, g *engine.Game, w int) []string {
	r := g.Report()
	m := r.Matrix
	shipped := "yes"
	switch r.End {
	case engine.EndHeld:
		shipped = "no, held"
	case engine.EndDeadline:
		shipped = "by the deadline, not by you"
	}
	incidents := "none"
	if r.Incidents > 0 {
		incidents = fmt.Sprintf("%d, worst %s", r.Incidents, r.Worst)
	}
	catches := m.RejectedBad
	lines := []string{
		"",
		st.section.Render("GRADE  ") + st.title.Render(r.Grade) + st.bright.Render(fmt.Sprintf("  %d", r.Score)),
		"",
	}
	for _, l := range wrap(r.Verdict, w) {
		lines = append(lines, st.bright.Render(l))
	}
	cell := func(n int, bad bool) string {
		s := fmt.Sprintf("%6d", n)
		if bad && n > 0 {
			return st.danger.Render(s)
		}
		return st.base.Render(s)
	}
	lines = append(lines,
		"",
		st.section.Render("TRUST CALIBRATION  ")+st.title.Render(fmt.Sprint(r.Calibration)),
		st.dim.Render("                 clean  flawed"),
		st.dim.Render("    accepted  ")+cell(m.AcceptedGood, false)+"  "+cell(m.AcceptedBad, true),
		st.dim.Render("    rejected  ")+cell(m.RejectedGood, true)+"  "+cell(m.RejectedBad, false),
		"",
	)
	stat := func(k, v string) string { return st.dim.Render(fit(k, 22)) + st.base.Render(v) }
	lines = append(lines,
		stat("Shipped", shipped),
		stat("Client satisfaction", fmt.Sprint(r.Satisfaction)),
		stat("Production", fmt.Sprintf("%d  incidents: %s", r.Production, incidents)),
		stat("Requirements", fmt.Sprintf("%d of %d delivered", r.Delivered, r.Required)),
		stat("Review coverage", fmt.Sprintf("%d%% of hunks read", int(r.Coverage*100+0.5))),
		stat("Diagnosis", fmt.Sprintf("%d of %d catches named correctly", r.Diagnosed, catches)),
		stat("Budget left", fmt.Sprintf("%d%%", r.Efficiency)),
		"",
	)
	for _, l := range wrap("Score: 40% calibration, 35% production, 25% client satisfaction. Calibration is the mean of the share of flaws rejected and the share of clean work accepted. Speed is not scored.", w) {
		lines = append(lines, st.faint.Render(l))
	}
	return lines
}

// postMortemLines replays the run as a timeline.
func postMortemLines(st *styles, g *engine.Game, w int) []string {
	pm := g.PostMortem()
	r := g.Report()
	cfg := g.Config()
	const ind = "    "
	tw := w - len(ind)
	var out []string
	add := func(s string) { out = append(out, s) }
	para := func(style func(...string) string, text string) {
		for _, l := range wrap(text, tw) {
			add(ind + style(l))
		}
	}

	add("")
	add(st.title.Render("POST-MORTEM") + "  " + st.bright.Render(g.Scenario().Title))
	add(st.dim.Render(fmt.Sprintf("seed %d  %s  score %d  grade %s", cfg.Seed, cfg.Difficulty, r.Score, r.Grade)))
	add("")

	for _, it := range pm.Items {
		if it.Client != "" {
			add(st.faint.Render(engine.FormatClock(it.Clock)) + "  " + st.accent.Render("CLIENT"))
			para(st.dim.Render, it.Client)
			add("")
			continue
		}
		e := it.Entry
		head := st.dim.Render(fmt.Sprintf("#%-2d ", e.Seq)) + st.faint.Render(engine.FormatClock(e.Clock)) + "  " + st.bright.Render(truncRight(e.Title, w-22))
		add(head)
		read := fmt.Sprintf("%d/%d hunks read", e.Read, e.Hunks)
		switch e.Call {
		case engine.AcceptedGood:
			add(ind + st.base.Render("Accepted. Clean. ") + st.dim.Render(read))
		case engine.RejectedGood:
			add(ind + st.accent.Render("Rejected: "+e.Reason+". It was clean. ") + st.dim.Render(read))
			para(st.dim.Render, "The agent found nothing to change and resubmitted it. The time and tokens were spent on nothing.")
		case engine.AcceptedBad:
			add(ind + st.danger.Render("Accepted. Flawed: "+e.Flaw.Type.Label()+". ") + st.dim.Render(read))
		case engine.RejectedBad:
			verdict := "Correct."
			if !e.Diagnosed {
				verdict = "Right call, wrong reason. It was " + e.Flaw.Type.Label() + "."
			}
			add(ind + st.base.Render("Rejected: "+e.Reason+". ") + st.accent.Render(verdict))
		case engine.Undecided:
			add(ind + st.dim.Render("Still in review when the run ended. "+read))
		}
		if e.Note != "" {
			para(st.dim.Render, "Your note: "+e.Note)
		}
		if f := e.Flaw; f != nil && (e.Call == engine.AcceptedBad || e.Call == engine.RejectedBad) {
			out = append(out, flawBlock(st, e, f, ind, tw)...)
		}
		add("")
	}

	if len(pm.Lessons) > 0 {
		add(st.title.Render("WHAT TO LOOK FOR"))
		add("")
		for _, l := range pm.Lessons {
			add(st.bright.Render(l.Type.Label()) + st.dim.Render(fmt.Sprintf("  caught %d, missed %d", l.Caught, l.Missed)))
			para(st.base.Render, l.Type.LookFor())
			add("")
		}
	}
	return out
}

func flawBlock(st *styles, e *engine.Entry, f *engine.Flaw, ind string, tw int) []string {
	var out []string
	para := func(style func(...string) string, text string) {
		for _, l := range wrap(text, tw) {
			out = append(out, ind+style(l))
		}
	}
	where := fmt.Sprintf("%s  line %d  hunk %d of %d", f.File, f.Line, f.Hunk+1, e.Hunks)
	if f.Read {
		where += ", opened"
	} else {
		where += ", never opened"
	}
	out = append(out, ind+st.bright.Render(truncRight(where, tw)))
	out = append(out, ind+st.faint.Render(truncRight(f.Header, tw)))
	for i, l := range f.Excerpt {
		out = append(out, ind+excerptLine(st, l, i == f.Mark, tw))
	}
	if e.Call == engine.RejectedBad {
		if e.Diagnosed {
			para(st.base.Render, "You spotted it: "+f.Spotted)
		} else {
			para(st.base.Render, "What it was: "+f.Spotted)
		}
		return out
	}
	para(st.base.Render, fmt.Sprintf("Catchable at turn %d, candidate #%d, in hunk %d. %s", e.TurnNo, e.Seq, f.Hunk+1, f.Spotted))
	if f.Decay {
		para(st.dim.Render, fmt.Sprintf("Reintroduced after the agent's context fell to %d%%. It had forgotten an earlier constraint.", int(e.Health*100+0.5)))
	}
	switch {
	case e.TestsRun && e.TestsPass:
		para(st.dim.Render, "The tests ran and passed. They did not cover this.")
	case e.TestsRun:
		para(st.dim.Render, "The tests ran and failed. They were accepted anyway.")
	default:
		para(st.dim.Render, "The tests were not run.")
	}
	if e.Misled {
		para(st.dim.Render, "You asked the agent to explain. The explanation was wrong, and confident.")
	}
	if in := e.Incident; in != nil {
		out = append(out, ind+st.faint.Render(engine.FormatIncidentTime(in.At)+"  ")+severityText(st, in.Severity)+"  "+st.bright.Render(in.Title))
		para(st.dim.Render, in.Cost)
	}
	return out
}

func excerptLine(st *styles, l diff.Line, mark bool, w int) string {
	num, sign := l.New, " "
	switch l.Kind {
	case diff.Added:
		sign = "+"
	case diff.Removed:
		num, sign = l.Old, "-"
	}
	text := truncRight(strings.ReplaceAll(l.Text, "\t", "    "), w-9)
	if mark {
		return st.danger.Render(fmt.Sprintf("▶ %4d %s %s", num, sign, text))
	}
	return st.faint.Render("│ ") + st.dim.Render(fmt.Sprintf("%4d %s %s", num, sign, text))
}
