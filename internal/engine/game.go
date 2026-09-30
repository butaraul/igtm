// Package engine is the game: turns, resources, context decay, flaw
// planting, rejection handling, production outcomes and scoring. It does no
// I/O and has no clock or randomness of its own; a run is fully determined by
// its scenario, seed, difficulty and the actions applied to it.
package engine

import (
	"errors"
	"fmt"

	"github.com/butaraul/lgtm/internal/diff"
	"github.com/butaraul/lgtm/internal/scenario"
)

// Difficulty scales the budget and the chance of seeded flaws.
type Difficulty string

// Difficulties.
const (
	Easy   Difficulty = "easy"
	Normal Difficulty = "normal"
	Hard   Difficulty = "hard"
)

// ParseDifficulty reads a difficulty name.
func ParseDifficulty(s string) (Difficulty, error) {
	switch d := Difficulty(s); d {
	case Easy, Normal, Hard:
		return d, nil
	}
	return "", fmt.Errorf("unknown difficulty %q (want easy, normal or hard)", s)
}

type modifiers struct {
	budget float64 // multiplies clock and token budgets
	chance float64 // multiplies seeded flaw chances
}

var difficultyMods = map[Difficulty]modifiers{
	Easy:   {budget: 1.3, chance: 0.6},
	Normal: {budget: 1, chance: 1},
	Hard:   {budget: 0.85, chance: 1.35},
}

// Phase is where the run is.
type Phase uint8

// Phases.
const (
	// PhaseBrief shows the client's brief before work starts.
	PhaseBrief Phase = iota
	// PhaseReview has a candidate waiting for a decision.
	PhaseReview
	// PhaseShip has no more work coming; ship or hold.
	PhaseShip
	// PhaseOver is after ship, hold or deadline. Outcome and Report are
	// final.
	PhaseOver
)

// ActionKind names a player action.
type ActionKind string

// Actions.
const (
	ActStart     ActionKind = "start"
	ActAccept    ActionKind = "accept"
	ActReject    ActionKind = "reject"
	ActInspect   ActionKind = "inspect"
	ActTests     ActionKind = "tests"
	ActExplain   ActionKind = "explain"
	ActSummarise ActionKind = "summarise"
	ActShip      ActionKind = "ship"
	ActHold      ActionKind = "hold"
)

// ReasonOther is the rejection reason for "something is wrong but I can't
// name it".
const ReasonOther = "other"

// Action is one player input. Hunk applies to ActInspect; Reason and Note to
// ActReject. Actions serialise to JSON for saves.
type Action struct {
	Kind   ActionKind `json:"kind"`
	Hunk   int        `json:"hunk,omitempty"`
	Reason string     `json:"reason,omitempty"`
	Note   string     `json:"note,omitempty"`
}

// Errors returned by Apply. Neither changes the game or is recorded.
var (
	// ErrInvalid means the action is not allowed right now.
	ErrInvalid = errors.New("not allowed now")
	// ErrNoop means the action would have no effect, such as running the
	// tests twice on the same candidate.
	ErrNoop = errors.New("no effect")
)

// EndReason is how a run ended.
type EndReason string

// End reasons.
const (
	EndShipped  EndReason = "shipped"
	EndHeld     EndReason = "held"
	EndDeadline EndReason = "deadline"
)

// Config fixes everything about a run except the player's actions.
type Config struct {
	Seed       uint64
	Difficulty Difficulty
}

// Candidate is one submission from the agent awaiting review.
type Candidate struct {
	Seq         int // presentation order, from 1
	TurnNo      int // which unit of work, from 1
	Title       string
	Message     string
	Diff        *diff.Diff
	Clock       int     // minute it arrived
	Health      float64 // context health when it was generated
	Resubmitted bool    // same work again after a rejection
	TestsRun    bool
	Explained   bool

	turn    *scenario.Turn
	variant *scenario.Variant
	key     string // identity of the content, shared by resubmissions
	tests   scenario.Tests
	explain scenario.Explain
}

// Decision is an accept or reject of one candidate.
type Decision struct {
	Seq      int
	Accepted bool
	Flawed   bool
	Reason   string // rejection reason: a flaw type or ReasonOther
	Note     string
	Clock    int
	Read     int  // hunks inspected at the time of the decision
	FlawRead bool // the hunk holding the flaw had been inspected
}

// Game is one run.
type Game struct {
	scn   *scenario.Scenario
	cfg   Config
	phase Phase

	clockBudget, clock int
	tokenBudget, spent int
	window, ctx        int
	forgot             bool // a lossy auto-compaction happened

	queue    []*scenario.Turn
	plan     []*scenario.Turn // every turn queued so far, minus cancelled
	fired    map[string]bool  // change ids
	reqs     []scenario.Requirement
	accepted map[string]*Candidate // turn id -> accepted candidate
	dropped  map[string]bool       // turn ids abandoned after rejections

	cur       *Candidate
	turnNo    int
	rejects   int
	cands     []*Candidate
	decisions []Decision
	inspected map[string][]bool
	log       []Event
	actions   []Action

	end     EndReason
	outcome *Outcome
}

// New starts a run in the brief phase.
func New(scn *scenario.Scenario, cfg Config) (*Game, error) {
	if scn == nil {
		return nil, errors.New("no scenario")
	}
	if cfg.Difficulty == "" {
		cfg.Difficulty = Normal
	}
	m, ok := difficultyMods[cfg.Difficulty]
	if !ok {
		return nil, fmt.Errorf("unknown difficulty %q", cfg.Difficulty)
	}
	g := &Game{
		scn:         scn,
		cfg:         cfg,
		clockBudget: int(float64(scn.Budget.Clock) * m.budget),
		tokenBudget: int(float64(scn.Budget.Tokens) * m.budget),
		window:      scn.Budget.Context,
		fired:       map[string]bool{},
		accepted:    map[string]*Candidate{},
		dropped:     map[string]bool{},
		inspected:   map[string][]bool{},
	}
	g.queue = append(g.queue, scn.Turns...)
	g.plan = append(g.plan, scn.Turns...)
	g.reqs = append(g.reqs, scn.Requirements...)
	return g, nil
}

// Replay rebuilds a run from its actions. It fails if any action is not
// valid at the point it is applied.
func Replay(scn *scenario.Scenario, cfg Config, actions []Action) (*Game, error) {
	g, err := New(scn, cfg)
	if err != nil {
		return nil, err
	}
	for i, a := range actions {
		if err := g.Apply(a); err != nil {
			return nil, fmt.Errorf("action %d (%s): %w", i+1, a.Kind, err)
		}
	}
	return g, nil
}

// Apply performs one action. On error nothing changes.
func (g *Game) Apply(a Action) error {
	var err error
	switch a.Kind {
	case ActStart:
		err = g.start()
	case ActAccept:
		err = g.accept()
	case ActReject:
		err = g.reject(a.Reason, a.Note)
	case ActInspect:
		err = g.inspect(a.Hunk)
	case ActTests:
		err = g.runTests()
	case ActExplain:
		err = g.explainChange()
	case ActSummarise:
		err = g.summarise()
	case ActShip:
		err = g.ship()
	case ActHold:
		err = g.hold()
	default:
		err = fmt.Errorf("%w: unknown action %q", ErrInvalid, a.Kind)
	}
	if err != nil {
		return err
	}
	g.actions = append(g.actions, a)
	if g.phase != PhaseOver && g.clock >= g.clockBudget {
		g.logf(EvSystem, "Deadline. The client shipped what had been accepted.")
		g.finish(EndDeadline)
	}
	return nil
}

// Scenario is the contract being played.
func (g *Game) Scenario() *scenario.Scenario { return g.scn }

// Config is the run's seed and difficulty.
func (g *Game) Config() Config { return g.cfg }

// Phase is the current phase.
func (g *Game) Phase() Phase { return g.phase }

// Current is the candidate under review, or nil.
func (g *Game) Current() *Candidate { return g.cur }

// Actions is every action applied so far, for saving.
func (g *Game) Actions() []Action { return append([]Action(nil), g.actions...) }

// Log is the run's event log.
func (g *Game) Log() []Event { return g.log }

// Inspected reports whether hunk i of the current candidate has been read.
func (g *Game) Inspected(i int) bool {
	if g.cur == nil {
		return false
	}
	ins := g.inspected[g.cur.key]
	return i >= 0 && i < len(ins) && ins[i]
}

// Progress is the number of the current unit of work and the number known
// so far, including queued work.
func (g *Game) Progress() (current, total int) {
	total = g.turnNo + len(g.queue)
	return g.turnNo, total
}

// Resources is a snapshot of the run's budgets.
type Resources struct {
	Health      float64 // context health, 0 to 1
	TokensSpent int
	TokenBudget int
	ClockUsed   int // minutes
	ClockBudget int
}

// Resources reports the budgets.
func (g *Game) Resources() Resources {
	return Resources{
		Health:      g.health(),
		TokensSpent: g.spent,
		TokenBudget: g.tokenBudget,
		ClockUsed:   g.clock,
		ClockBudget: g.clockBudget,
	}
}

// ReqStatus is a requirement and whether the agent has claimed it done.
type ReqStatus struct {
	scenario.Requirement
	Claimed bool
}

// Requirements lists the active requirements. Claimed means every unit of
// work implementing it was accepted, which is what the agent would report;
// it says nothing about whether the code does it.
func (g *Game) Requirements() []ReqStatus {
	out := make([]ReqStatus, len(g.reqs))
	for i, r := range g.reqs {
		out[i] = ReqStatus{Requirement: r, Claimed: g.claimed(r.ID)}
	}
	return out
}

// Decisions lists every accept and reject in order.
func (g *Game) Decisions() []Decision { return g.decisions }

func (g *Game) health() float64 {
	h := 1 - float64(g.ctx)/float64(g.window)
	switch {
	case h < 0:
		return 0
	case h > 1:
		return 1
	}
	return h
}

func (g *Game) tokensLeft() bool { return g.spent < g.tokenBudget }

func (g *Game) finish(r EndReason) {
	g.phase = PhaseOver
	g.end = r
	g.cur = nil
	g.outcome = g.resolveOutcome()
}
