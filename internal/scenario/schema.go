// Package scenario defines the contract schema, loads the embedded scenarios
// and validates them.
package scenario

import "github.com/butaraul/lgtm/internal/diff"

// Scenario is one contract: a client, a brief, and the work the agent will
// submit for review.
type Scenario struct {
	ID           string        `yaml:"id"`
	Title        string        `yaml:"title"`
	Difficulty   int           `yaml:"difficulty"`
	Summary      string        `yaml:"summary"`
	Client       Client        `yaml:"client"`
	Brief        string        `yaml:"brief"`
	Requirements []Requirement `yaml:"requirements"`
	Budget       Budget        `yaml:"budget"`
	Turns        []*Turn       `yaml:"turns"`
	Changes      []*Change     `yaml:"changes"`
}

// Client is the persona writing the brief and the change requests.
type Client struct {
	Name  string `yaml:"name"`
	Role  string `yaml:"role"`
	About string `yaml:"about"`
}

// Requirement is something the client asked for. Weight defaults to 1.
type Requirement struct {
	ID     string `yaml:"id"`
	Text   string `yaml:"text"`
	Weight int    `yaml:"weight"`
}

// Budget sets the run's resources: clock in working minutes, tokens the
// agent may spend, and the size of its context window in tokens.
type Budget struct {
	Clock   int `yaml:"clock"`
	Tokens  int `yaml:"tokens"`
	Context int `yaml:"context"`
}

// Turn is one unit of work: the clean candidate plus the flawed variants the
// agent might submit instead.
type Turn struct {
	ID         string     `yaml:"id"`
	Title      string     `yaml:"title"`
	Implements []string   `yaml:"implements"`
	Message    string     `yaml:"message"`
	Diff       string     `yaml:"diff"`
	Tests      Tests      `yaml:"tests"`
	Explain    Explain    `yaml:"explain"`
	Variants   []*Variant `yaml:"variants"`

	parsed *diff.Diff
}

// Parsed is the clean diff, parsed. It is set by validation.
func (t *Turn) Parsed() *diff.Diff { return t.parsed }

// Variant returns the variant with the given id, or nil.
func (t *Turn) Variant(id string) *Variant {
	for _, v := range t.Variants {
		if v.ID == id {
			return v
		}
	}
	return nil
}

// Tests is what the player sees when they run the test suite.
type Tests struct {
	Pass   bool   `yaml:"pass"`
	Output string `yaml:"output"`
}

// Explain is the agent's answer when asked to explain its change. Truthful
// is hidden from the player until the post-mortem.
type Explain struct {
	Text     string `yaml:"text"`
	Truthful bool   `yaml:"truthful"`
}

// Plant controls when a flawed variant is submitted in place of the clean
// candidate.
type Plant string

// Plant modes.
const (
	// PlantAlways submits the variant every run.
	PlantAlways Plant = "always"
	// PlantSeeded submits the variant with a seeded chance that rises as
	// the agent's context degrades.
	PlantSeeded Plant = "seeded"
	// PlantDecay submits the variant only when context health is below the
	// variant's threshold: the agent has forgotten a constraint.
	PlantDecay Plant = "decay"
	// PlantRevision is only reachable as another variant's "then": the
	// agent's fix for one flaw that introduces another.
	PlantRevision Plant = "revision"
)

// Variant is a flawed version of a turn's candidate, expressed as edits to
// the clean diff.
type Variant struct {
	ID       string   `yaml:"id"`
	Type     FlawType `yaml:"type"`
	Plant    Plant    `yaml:"plant"`
	Chance   float64  `yaml:"chance"`
	Below    float64  `yaml:"below"`
	Patch    []Edit   `yaml:"patch"`
	Line     string   `yaml:"line"`
	Message  string   `yaml:"message"`
	Tests    *Tests   `yaml:"tests"`
	Explain  *Explain `yaml:"explain"`
	Fix      string   `yaml:"fix"`
	Then     string   `yaml:"then"`
	Drops    []string `yaml:"drops"`
	Spotted  string   `yaml:"spotted"`
	Incident Incident `yaml:"incident"`

	parsed *diff.Diff
	loc    diff.Loc
}

// Parsed is the variant's diff with its patch applied. It is set by
// validation.
func (v *Variant) Parsed() *diff.Diff { return v.parsed }

// Loc is where the flaw sits in the variant's diff.
func (v *Variant) Loc() diff.Loc { return v.loc }

// Edit replaces exactly one occurrence of Find in the clean diff.
type Edit struct {
	Find    string `yaml:"find"`
	Replace string `yaml:"replace"`
}

// Incident is what happens in production when the flaw ships. Severity and
// Day default by flaw type when omitted.
type Incident struct {
	Title    string   `yaml:"title"`
	Detail   string   `yaml:"detail"`
	Cost     string   `yaml:"cost"`
	Severity Severity `yaml:"severity"`
	Day      *int     `yaml:"day"`
}

// Change is a client message mid-build. It can add and remove
// requirements, insert new turns, cancel queued ones, and add a rework turn
// when it conflicts with work that was already accepted.
type Change struct {
	ID        string        `yaml:"id"`
	After     string        `yaml:"after"`
	Kind      ChangeKind    `yaml:"kind"`
	Message   string        `yaml:"message"`
	Adds      []Requirement `yaml:"adds"`
	Removes   []string      `yaml:"removes"`
	Turns     []*Turn       `yaml:"turns"`
	Cancels   []string      `yaml:"cancels"`
	Conflicts []string      `yaml:"conflicts"`
	Rework    *Turn         `yaml:"rework"`
}

// ChangeKind labels a change for the log.
type ChangeKind string

// Change kinds.
const (
	ScopeCreep ChangeKind = "scope_creep"
	Pivot      ChangeKind = "pivot"
)

// Severity of a production incident.
type Severity string

// Severities, in ascending order.
const (
	Low      Severity = "low"
	Medium   Severity = "medium"
	High     Severity = "high"
	Critical Severity = "critical"
)

// Rank orders severities from 1 (low) to 4 (critical); 0 is unknown.
func (s Severity) Rank() int {
	switch s {
	case Low:
		return 1
	case Medium:
		return 2
	case High:
		return 3
	case Critical:
		return 4
	}
	return 0
}
