package engine

import (
	"testing"

	"github.com/butaraul/lgtm/internal/scenario"
)

// careful reads every hunk, runs the tests, summarises when context runs
// low, and rejects every flaw with the right reason. It stands in for a
// thorough player and checks every scenario can be finished that way.
func careful(g *Game) Action {
	switch g.Phase() {
	case PhaseBrief:
		return Action{Kind: ActStart}
	case PhaseShip:
		return Action{Kind: ActShip}
	}
	c := g.Current()
	if g.health() < 0.45 && g.ctx > int(float64(g.window)*summaryFloor) && g.tokensLeft() {
		return Action{Kind: ActSummarise}
	}
	for i := range c.Diff.NumHunks() {
		if !g.Inspected(i) {
			return Action{Kind: ActInspect, Hunk: i}
		}
	}
	if !c.TestsRun && g.spent+g.CostOf(Action{Kind: ActTests}).Tokens < g.tokenBudget/2 {
		return Action{Kind: ActTests}
	}
	if c.variant != nil {
		return Action{Kind: ActReject, Reason: string(c.variant.Type)}
	}
	return Action{Kind: ActAccept}
}

func rubberStamp(g *Game) Action {
	switch g.Phase() {
	case PhaseBrief:
		return Action{Kind: ActStart}
	case PhaseShip:
		return Action{Kind: ActShip}
	}
	return Action{Kind: ActAccept}
}

func TestScenarioBudgets(t *testing.T) {
	list, err := scenario.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		for _, d := range []Difficulty{Easy, Normal, Hard} {
			for seed := uint64(1); seed <= 20; seed++ {
				g, err := New(s, Config{Seed: seed, Difficulty: d})
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; g.Phase() != PhaseOver; i++ {
					if i > 1000 {
						t.Fatalf("%s: run did not end", s.ID)
					}
					if err := g.Apply(careful(g)); err != nil {
						t.Fatalf("%s seed %d: %v", s.ID, seed, err)
					}
				}
				r := g.Report()
				if d != Hard && r.End != EndShipped {
					t.Errorf("%s %s seed %d: careful play ended by %s", s.ID, d, seed, r.End)
				}
				if d == Normal && seed <= 3 {
					res := g.Resources()
					t.Logf("%-9s careful: clock %3d/%3d tokens %6d/%6d grade %s score %d", s.ID,
						res.ClockUsed, res.ClockBudget, res.TokensSpent, res.TokenBudget, r.Grade, r.Score)
				}
				if d == Normal && r.Grade != "A" {
					t.Errorf("%s seed %d: careful play graded %s (%+v)", s.ID, seed, r.Grade, r)
				}
			}
			g, _ := New(s, Config{Seed: 1, Difficulty: d})
			for g.Phase() != PhaseOver {
				if err := g.Apply(rubberStamp(g)); err != nil {
					t.Fatal(err)
				}
			}
			r := g.Report()
			if r.Grade == "A" || r.Grade == "B" {
				t.Errorf("%s %s: rubber stamp graded %s", s.ID, d, r.Grade)
			}
			if d == Normal {
				res := g.Resources()
				t.Logf("%-9s stamp:   clock %3d/%3d tokens %6d/%6d grade %s score %d incidents %d", s.ID,
					res.ClockUsed, res.ClockBudget, res.TokensSpent, res.TokenBudget, r.Grade, r.Score, r.Incidents)
			}
		}
	}
}
