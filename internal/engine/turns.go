package engine

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/butaraul/lgtm/internal/scenario"
)

func (g *Game) start() error {
	if g.phase != PhaseBrief {
		return ErrInvalid
	}
	g.phase = PhaseReview
	g.logText(EvBrief, strings.TrimSpace(g.scn.Brief))
	g.next()
	return nil
}

// next presents the next unit of work, or moves to the ship phase when
// there is none or the agent cannot afford it.
func (g *Game) next() {
	g.cur = nil
	switch {
	case len(g.queue) == 0:
		g.phase = PhaseShip
		g.logf(EvSystem, "Nothing left in the queue. Ship it or hold it.")
		return
	case !g.tokensLeft():
		g.phase = PhaseShip
		g.logf(EvSystem, "Token budget spent. The agent has stopped.")
		return
	}
	t := g.queue[0]
	g.queue = g.queue[1:]
	g.turnNo++
	g.rejects = 0
	v := g.pick(t)
	msg := t.Message
	if v != nil && v.Message != "" {
		msg = v.Message
	}
	g.present(t, v, msg, false)
}

// pick decides which version of a turn the agent submits: a decay variant if
// the agent has forgotten a constraint, else a guaranteed flaw, else a
// seeded one, else the clean candidate.
func (g *Game) pick(t *scenario.Turn) *scenario.Variant {
	h := g.health()
	for _, v := range t.Variants {
		if v.Plant == scenario.PlantDecay && (h < v.Below || g.forgot) {
			g.forgot = false
			return v
		}
	}
	for _, v := range t.Variants {
		if v.Plant == scenario.PlantAlways {
			return v
		}
	}
	var seeded []*scenario.Variant
	for _, v := range t.Variants {
		if v.Plant == scenario.PlantSeeded {
			seeded = append(seeded, v)
		}
	}
	if len(seeded) == 0 {
		return nil
	}
	v := seeded[int(g.roll(t.ID, "pick")*float64(len(seeded)))]
	if g.roll(t.ID, "plant") < g.plantChance(v, h) {
		return v
	}
	return nil
}

func (g *Game) plantChance(v *scenario.Variant, health float64) float64 {
	c := v.Chance
	if c == 0 {
		c = defaultChance
	}
	c *= difficultyMods[g.cfg.Difficulty].chance
	if health < 0.5 {
		c += decayBoost * (0.5 - health) / 0.5
	}
	return min(c, maxChance)
}

// present has the agent generate a candidate and charges for it.
func (g *Game) present(t *scenario.Turn, v *scenario.Variant, msg string, resubmitted bool) {
	if g.ctx >= g.window {
		g.spent += g.ctx + summaryTokens
		g.ctx = int(float64(g.window) * summaryFloor)
		g.clock += compactMinutes
		g.forgot = true
		g.logf(EvSystem, "Context full. The agent compacted it on its own. Not everything survived.")
	}
	c := &Candidate{
		Seq:         len(g.cands) + 1,
		TurnNo:      g.turnNo,
		Title:       t.Title,
		Message:     strings.TrimSpace(msg),
		Diff:        t.Parsed(),
		Health:      g.health(),
		Resubmitted: resubmitted,
		turn:        t,
		variant:     v,
		key:         t.ID,
		tests:       t.Tests,
		explain:     t.Explain,
	}
	if v != nil {
		c.Diff = v.Parsed()
		c.key = t.ID + "/" + v.ID
		if v.Tests != nil {
			c.tests = *v.Tests
		}
		if v.Explain != nil {
			c.explain = *v.Explain
		} else {
			// The clean explanation describes code that is not there.
			c.explain.Truthful = false
		}
	}
	if _, ok := g.inspected[c.key]; !ok {
		g.inspected[c.key] = make([]bool, c.Diff.NumHunks())
	}
	g.clock += generateMinutes(c.Diff)
	out := generateOutput(c.Diff)
	g.spent += g.ctx + out
	g.ctx += genContext + out
	c.Clock = g.clock
	g.cands = append(g.cands, c)
	g.cur = c
	g.phase = PhaseReview
	g.logText(EvAgent, c.Message)
}

func (g *Game) decide(accepted bool, reason, note string) {
	c := g.cur
	d := Decision{
		Seq:      c.Seq,
		Accepted: accepted,
		Flawed:   c.variant != nil,
		Reason:   reason,
		Note:     note,
		Clock:    g.clock,
	}
	ins := g.inspected[c.key]
	for _, r := range ins {
		if r {
			d.Read++
		}
	}
	if c.variant != nil {
		d.FlawRead = ins[c.variant.Loc().Hunk]
	}
	g.decisions = append(g.decisions, d)
}

func (g *Game) accept() error {
	if g.phase != PhaseReview {
		return ErrInvalid
	}
	c := g.cur
	g.clock += acceptMinutes
	g.decide(true, "", "")
	if unread := c.Diff.NumHunks() - g.decisions[len(g.decisions)-1].Read; unread > 0 {
		g.logf(EvYou, "Accepted. %d of %d hunks unread.", unread, c.Diff.NumHunks())
	} else {
		g.logf(EvYou, "Accepted.")
	}
	g.accepted[c.turn.ID] = c
	g.resolve(c.turn)
	g.next()
	return nil
}

func (g *Game) reject(reason, note string) error {
	if g.phase != PhaseReview {
		return ErrInvalid
	}
	if reason != ReasonOther && !scenario.FlawType(reason).Valid() {
		return fmt.Errorf("%w: unknown reason %q", ErrInvalid, reason)
	}
	c := g.cur
	note = strings.TrimSpace(note)
	g.clock += rejectMinutes
	g.ctx += rejectContext + len(note)/4
	g.decide(false, reason, note)
	label := reasonLabel(reason)
	if note != "" {
		g.logf(EvYou, "Rejected: %s. %s", label, note)
	} else {
		g.logf(EvYou, "Rejected: %s.", label)
	}
	g.rejects++

	if g.rejects >= maxRejections || !g.tokensLeft() {
		g.dropped[c.turn.ID] = true
		g.logf(EvAgent, "Dropping this change. %s is not in the build.", c.Title)
		g.resolve(c.turn)
		g.next()
		return nil
	}

	v := c.variant
	if v == nil {
		g.present(c.turn, nil, fmt.Sprintf("Checked for %s. Found nothing to change. Resubmitting as-is.", strings.ToLower(label)), true)
		return nil
	}
	fixed := reason == string(v.Type)
	if !fixed {
		chance := wrongReasonFix
		if reason == ReasonOther {
			chance = vagueReasonFix
		}
		fixed = g.roll(c.turn.ID, v.ID, strconv.Itoa(c.Seq), "reject") < chance
	}
	if !fixed {
		g.present(c.turn, v, fmt.Sprintf("Checked for %s. Found none. Resubmitting unchanged.", strings.ToLower(label)), true)
		return nil
	}
	msg := v.Fix
	if reason != string(v.Type) {
		msg = "Looked again and found something else. " + v.Fix
	}
	g.present(c.turn, c.turn.Variant(v.Then), msg, false)
	return nil
}

func reasonLabel(reason string) string {
	if reason == ReasonOther {
		return "Something else"
	}
	return scenario.FlawType(reason).Label()
}

func (g *Game) inspect(i int) error {
	if g.phase != PhaseReview {
		return ErrInvalid
	}
	_, h := g.cur.Diff.Hunk(i)
	if h == nil {
		return fmt.Errorf("%w: no hunk %d", ErrInvalid, i)
	}
	ins := g.inspected[g.cur.key]
	if ins[i] {
		return ErrNoop
	}
	ins[i] = true
	g.clock += inspectMinutes(h)
	return nil
}

func (g *Game) runTests() error {
	if g.phase != PhaseReview || !g.tokensLeft() {
		return ErrInvalid
	}
	c := g.cur
	if c.TestsRun {
		return ErrNoop
	}
	c.TestsRun = true
	g.clock += testsMinutes
	g.spent += g.ctx + testsTokens
	g.ctx += testsContextBase + 12*countLines(c.tests.Output)
	g.log = append(g.log, Event{Kind: EvTests, Clock: g.clock, Seq: c.Seq, Pass: c.tests.Pass, Text: strings.TrimRight(c.tests.Output, "\n")})
	return nil
}

func (g *Game) explainChange() error {
	if g.phase != PhaseReview || !g.tokensLeft() {
		return ErrInvalid
	}
	c := g.cur
	if c.Explained {
		return ErrNoop
	}
	c.Explained = true
	text := strings.TrimSpace(c.explain.Text)
	g.clock += explainMinutes
	g.spent += g.ctx + explainBase + len(text)/4
	g.ctx += explainBase + len(text)/4
	g.logText(EvExplain, text)
	return nil
}

func (g *Game) summarise() error {
	if g.phase != PhaseReview || !g.tokensLeft() {
		return ErrInvalid
	}
	floor := int(float64(g.window) * summaryFloor)
	if g.ctx <= floor {
		return ErrNoop
	}
	g.clock += summariseMinutes
	g.spent += g.ctx + summaryTokens
	g.ctx = floor
	g.forgot = false
	g.logf(EvSystem, "Summarised and restarted. Context at %d%%.", int(g.health()*100+0.5))
	return nil
}

func (g *Game) ship() error {
	if g.phase != PhaseReview && g.phase != PhaseShip {
		return ErrInvalid
	}
	g.logf(EvYou, "Shipped.")
	g.finish(EndShipped)
	return nil
}

func (g *Game) hold() error {
	if g.phase != PhaseReview && g.phase != PhaseShip {
		return ErrInvalid
	}
	g.logf(EvYou, "Held. Nothing shipped.")
	g.finish(EndHeld)
	return nil
}

// resolve fires any client changes waiting on this turn.
func (g *Game) resolve(t *scenario.Turn) {
	for _, ch := range g.scn.Changes {
		if ch.After == t.ID && !g.fired[ch.ID] {
			g.fire(ch)
		}
	}
}

func (g *Game) fire(ch *scenario.Change) {
	g.fired[ch.ID] = true
	g.logText(EvClient, strings.TrimSpace(ch.Message))
	g.reqs = slices.DeleteFunc(g.reqs, func(r scenario.Requirement) bool {
		return slices.Contains(ch.Removes, r.ID)
	})
	g.reqs = append(g.reqs, ch.Adds...)
	g.queue = slices.DeleteFunc(g.queue, func(t *scenario.Turn) bool { return slices.Contains(ch.Cancels, t.ID) })
	g.plan = slices.DeleteFunc(g.plan, func(t *scenario.Turn) bool { return slices.Contains(ch.Cancels, t.ID) })

	var insert []*scenario.Turn
	if ch.Rework != nil && slices.ContainsFunc(ch.Conflicts, func(id string) bool { return g.accepted[id] != nil }) {
		insert = append(insert, ch.Rework)
	}
	insert = append(insert, ch.Turns...)
	g.queue = append(insert, g.queue...)
	g.plan = append(g.plan, insert...)
}

// claimed reports whether every planned turn implementing the requirement
// has been accepted.
func (g *Game) claimed(id string) bool {
	found := false
	for _, t := range g.plan {
		if !slices.Contains(t.Implements, id) {
			continue
		}
		if g.accepted[t.ID] == nil {
			return false
		}
		found = true
	}
	return found
}

// delivered is claimed, minus requirements an accepted flaw silently
// dropped.
func (g *Game) delivered(id string) bool {
	if !g.claimed(id) {
		return false
	}
	for _, t := range g.plan {
		if c := g.accepted[t.ID]; c != nil && c.variant != nil && slices.Contains(c.variant.Drops, id) {
			return false
		}
	}
	return true
}
