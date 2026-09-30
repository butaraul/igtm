package engine

import (
	"github.com/butaraul/lgtm/internal/diff"
	"github.com/butaraul/lgtm/internal/scenario"
)

// Call classifies what the player did with a candidate.
type Call uint8

// Calls.
const (
	// Undecided: the run ended with the candidate still in review.
	Undecided Call = iota
	AcceptedGood
	AcceptedBad
	RejectedGood
	RejectedBad
)

// PostMortem replays a finished run as a timeline.
type PostMortem struct {
	Items   []Item
	Lessons []Lesson
}

// Item is one row of the timeline: a candidate, or a client message that
// arrived after the candidate before it.
type Item struct {
	Entry  *Entry
	Client string
	Clock  int
}

// Entry is one candidate and what became of it.
type Entry struct {
	Seq, TurnNo int
	Title       string
	Clock       int
	Health      float64
	Resubmitted bool
	Call        Call
	Reason      string // rejection reason label
	Note        string
	Diagnosed   bool // rejection named the actual flaw
	Hunks, Read int  // at the time of the decision
	TestsRun    bool
	TestsPass   bool
	Explained   bool
	Misled      bool // asked for an explanation and got a false one
	Flaw        *Flaw
	Incident    *Incident
}

// Flaw pins a planted flaw to its hunk and line.
type Flaw struct {
	Type    scenario.FlawType
	Decay   bool // planted because the agent's context had degraded
	Spotted string
	File    string
	Header  string // hunk header
	Hunk    int    // global hunk index, from 0
	Line    int    // line number in the new file
	Text    string // the flawed line
	Excerpt []diff.Line
	Mark    int  // index of the flawed line within Excerpt
	Read    bool // the hunk had been inspected at decision time
}

// Lesson is the "what to look for" note for a flaw type seen in the run.
type Lesson struct {
	Type   scenario.FlawType
	Caught int
	Missed int
}

const excerptContext = 3

// PostMortem builds the timeline. It is complete only once the run is over.
func (g *Game) PostMortem() PostMortem {
	var pm PostMortem
	decided := map[int]Decision{}
	for _, d := range g.decisions {
		decided[d.Seq] = d
	}
	incidents := map[int]*Incident{}
	if g.outcome != nil {
		for i := range g.outcome.Incidents {
			in := &g.outcome.Incidents[i]
			incidents[in.Seq] = in
		}
	}
	clientAfter := map[int][]Event{}
	for _, e := range g.log {
		if e.Kind == EvClient {
			clientAfter[e.Seq] = append(clientAfter[e.Seq], e)
		}
	}
	lessons := map[scenario.FlawType]*Lesson{}

	addClient := func(seq int) {
		for _, e := range clientAfter[seq] {
			pm.Items = append(pm.Items, Item{Client: e.Text, Clock: e.Clock})
		}
	}
	addClient(0)
	for _, c := range g.cands {
		e := &Entry{
			Seq:         c.Seq,
			TurnNo:      c.TurnNo,
			Title:       c.Title,
			Clock:       c.Clock,
			Health:      c.Health,
			Resubmitted: c.Resubmitted,
			Hunks:       c.Diff.NumHunks(),
			TestsRun:    c.TestsRun,
			TestsPass:   c.tests.Pass,
			Explained:   c.Explained,
			Misled:      c.Explained && !c.explain.Truthful,
			Incident:    incidents[c.Seq],
		}
		d, ok := decided[c.Seq]
		if ok {
			e.Read = d.Read
			if !d.Accepted {
				e.Reason = reasonLabel(d.Reason)
				e.Note = d.Note
			}
			switch {
			case d.Accepted && !d.Flawed:
				e.Call = AcceptedGood
			case d.Accepted:
				e.Call = AcceptedBad
			case !d.Flawed:
				e.Call = RejectedGood
			default:
				e.Call = RejectedBad
				e.Diagnosed = d.Reason == string(c.variant.Type)
			}
		} else {
			for _, x := range g.inspected[c.key] {
				if x {
					e.Read++
				}
			}
		}
		if v := c.variant; v != nil {
			e.Flaw = flawDetail(c, v, d.FlawRead || (!ok && g.inspected[c.key][v.Loc().Hunk]))
			l := lessons[v.Type]
			if l == nil {
				l = &Lesson{Type: v.Type}
				lessons[v.Type] = l
			}
			switch e.Call {
			case AcceptedBad:
				l.Missed++
			case RejectedBad:
				l.Caught++
			}
		}
		pm.Items = append(pm.Items, Item{Entry: e, Clock: e.Clock})
		addClient(c.Seq)
	}
	for _, t := range scenario.FlawTypes {
		if l := lessons[t]; l != nil {
			pm.Lessons = append(pm.Lessons, *l)
		}
	}
	return pm
}

func flawDetail(c *Candidate, v *scenario.Variant, read bool) *Flaw {
	loc := v.Loc()
	f, h := c.Diff.Hunk(loc.Hunk)
	lo := max(0, loc.Line-excerptContext)
	hi := min(len(h.Lines), loc.Line+excerptContext+1)
	line := h.Lines[loc.Line]
	return &Flaw{
		Type:    v.Type,
		Decay:   v.Plant == scenario.PlantDecay,
		Spotted: v.Spotted,
		File:    f.Path(),
		Header:  h.Header(),
		Hunk:    loc.Hunk,
		Line:    line.New,
		Text:    line.Text,
		Excerpt: h.Lines[lo:hi],
		Mark:    loc.Line - lo,
		Read:    read,
	}
}
