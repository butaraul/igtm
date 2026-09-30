package engine

import (
	"strings"

	"github.com/butaraul/lgtm/internal/diff"
)

// Clock costs in working minutes.
const (
	acceptMinutes    = 1
	rejectMinutes    = 3 // writing the reason
	testsMinutes     = 7
	explainMinutes   = 3
	summariseMinutes = 15
	compactMinutes   = 5
)

// Token and context costs. Every agent call re-reads its whole context, so
// the token cost of a call is the context it carries plus what it writes.
const (
	tokensPerLine    = 14   // per diff line written
	genOverhead      = 350  // message and tool chatter per generation
	genContext       = 900  // file reads that stay in context per generation
	testsTokens      = 200  // the command the agent runs
	testsContextBase = 150  // plus 12 per line of output
	explainBase      = 120  // plus the explanation itself
	rejectContext    = 150  // plus the note
	summaryTokens    = 1500 // writing the summary
	summaryFloor     = 0.12 // fraction of the window a summary occupies
)

// Flaw planting.
const (
	defaultChance  = 0.4  // seeded variants without a chance of their own
	decayBoost     = 0.3  // extra chance at zero context health
	maxChance      = 0.95 // never certain
	wrongReasonFix = 0.3  // chance a mislabelled rejection still gets the flaw fixed
	vagueReasonFix = 0.5  // same, for "something else"
	maxRejections  = 3    // per unit of work before the agent drops it
)

func diffLines(d *diff.Diff) (all, changed int) {
	for _, f := range d.Files {
		for _, h := range f.Hunks {
			all += len(h.Lines)
			a, r := h.Stats()
			changed += a + r
		}
	}
	return all, changed
}

func generateMinutes(d *diff.Diff) int {
	_, changed := diffLines(d)
	return 6 + changed/10
}

func generateOutput(d *diff.Diff) int {
	all, _ := diffLines(d)
	return genOverhead + all*tokensPerLine
}

func inspectMinutes(h *diff.Hunk) int {
	return 1 + (len(h.Lines)+7)/8
}

func countLines(s string) int {
	return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1
}

// Cost is the clock and token price of an action on the current candidate.
// Tokens depend on the context at the time, so they are an estimate for
// actions that generate new work.
type Cost struct {
	Minutes int
	Tokens  int
}

// CostOf prices an action for display. For ActInspect it prices the given
// hunk of the current candidate.
func (g *Game) CostOf(a Action) Cost {
	switch a.Kind {
	case ActAccept:
		return Cost{Minutes: acceptMinutes}
	case ActInspect:
		if g.cur == nil {
			return Cost{}
		}
		if _, h := g.cur.Diff.Hunk(a.Hunk); h != nil {
			return Cost{Minutes: inspectMinutes(h)}
		}
	case ActTests:
		return Cost{Minutes: testsMinutes, Tokens: g.ctx + testsTokens}
	case ActExplain:
		return Cost{Minutes: explainMinutes, Tokens: g.ctx + explainBase}
	case ActSummarise:
		return Cost{Minutes: summariseMinutes, Tokens: g.ctx + summaryTokens}
	case ActReject:
		if g.cur == nil {
			return Cost{}
		}
		return Cost{
			Minutes: rejectMinutes + generateMinutes(g.cur.Diff),
			Tokens:  g.ctx + rejectContext + generateOutput(g.cur.Diff),
		}
	}
	return Cost{}
}
