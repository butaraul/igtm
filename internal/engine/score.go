package engine

import (
	"math"

	"github.com/butaraul/lgtm/internal/scenario"
)

// Matrix is the 2x2 of review decisions. A flawed candidate is a positive:
// rejecting it is a catch, accepting it is a miss.
type Matrix struct {
	AcceptedGood int // accepted clean work
	AcceptedBad  int // accepted a flaw
	RejectedGood int // rejected clean work
	RejectedBad  int // rejected a flaw
}

// Calibration is balanced accuracy on a 0 to 100 scale: the mean of the
// share of flaws rejected and the share of clean work accepted. Approving
// everything scores 50, as does rejecting everything. With decisions on
// only one kind of candidate it is that kind's rate alone.
func (m Matrix) Calibration() int {
	caught, missed := m.RejectedBad, m.AcceptedBad
	trusted, doubted := m.AcceptedGood, m.RejectedGood
	var rates []float64
	if caught+missed > 0 {
		rates = append(rates, float64(caught)/float64(caught+missed))
	}
	if trusted+doubted > 0 {
		rates = append(rates, float64(trusted)/float64(trusted+doubted))
	}
	if len(rates) == 0 {
		return 0
	}
	sum := 0.0
	for _, r := range rates {
		sum += r
	}
	return int(math.Round(100 * sum / float64(len(rates))))
}

// Score weights. Speed is not scored.
const (
	weightCalibration  = 0.40
	weightProduction   = 0.35
	weightSatisfaction = 0.25
)

func productionPenalty(s scenario.Severity) int {
	return [...]int{0, 10, 25, 45, 70}[s.Rank()]
}

func satisfactionPenalty(s scenario.Severity) int {
	return [...]int{0, 5, 10, 20, 30}[s.Rank()]
}

// Report is the scorecard for a finished run.
type Report struct {
	Matrix       Matrix
	Calibration  int // 0-100
	End          EndReason
	Shipped      bool
	Satisfaction int // 0-100
	Production   int // 0-100; 100 is nothing broke
	Incidents    int
	Worst        scenario.Severity
	Coverage     float64 // share of hunks inspected, 0-1
	Efficiency   int     // share of budget left, 0-100
	Delivered    int     // requirements delivered
	Required     int     // requirements active at the end
	Diagnosed    int     // rejections that named the flaw correctly
	Score        int
	Grade        string
	Verdict      string
}

// Report scores the run. It is only meaningful once the phase is over.
func (g *Game) Report() Report {
	r := Report{End: g.end}
	for _, d := range g.decisions {
		switch {
		case d.Accepted && !d.Flawed:
			r.Matrix.AcceptedGood++
		case d.Accepted && d.Flawed:
			r.Matrix.AcceptedBad++
		case !d.Accepted && !d.Flawed:
			r.Matrix.RejectedGood++
		default:
			r.Matrix.RejectedBad++
			if d.Reason == string(g.cands[d.Seq-1].variant.Type) {
				r.Diagnosed++
			}
		}
	}
	r.Calibration = r.Matrix.Calibration()

	read, total := 0, 0
	for _, ins := range g.inspected {
		total += len(ins)
		for _, x := range ins {
			if x {
				read++
			}
		}
	}
	if total > 0 {
		r.Coverage = float64(read) / float64(total)
	}
	used := 0.5*float64(g.spent)/float64(g.tokenBudget) + 0.5*float64(g.clock)/float64(g.clockBudget)
	r.Efficiency = clamp(int(math.Round(100*(1-used))), 0, 100)

	o := g.outcome
	if o == nil {
		return r
	}
	r.Shipped = o.Shipped
	r.Incidents = len(o.Incidents)
	weight, delivered := 0, 0
	for _, req := range g.reqs {
		weight += req.Weight
		if g.delivered(req.ID) {
			delivered += req.Weight
			r.Delivered++
		}
	}
	r.Required = len(g.reqs)

	r.Production = 100
	sat := 0
	if o.Shipped {
		if weight > 0 {
			sat = 100 * delivered / weight
		}
		for _, in := range o.Incidents {
			r.Production -= productionPenalty(in.Severity)
			sat -= satisfactionPenalty(in.Severity)
			if in.Severity.Rank() > r.Worst.Rank() {
				r.Worst = in.Severity
			}
		}
	}
	r.Production = clamp(r.Production, 0, 100)
	r.Satisfaction = clamp(sat, 0, 100)

	r.Score = int(math.Round(weightCalibration*float64(r.Calibration) +
		weightProduction*float64(r.Production) +
		weightSatisfaction*float64(r.Satisfaction)))
	r.Grade = grade(r.Score)
	r.Verdict = g.verdict(r)
	return r
}

func grade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	}
	return "F"
}

// verdict is the one line at the bottom of the scorecard.
func (g *Game) verdict(r Report) string {
	m := r.Matrix
	switch r.End {
	case EndHeld:
		if m.AcceptedBad > 0 {
			return "Held. The flaws you approved never reached anyone. Neither did anything else."
		}
		return "Nothing shipped. Nothing broke. The client noticed one of those."
	case EndDeadline:
		if r.Incidents > 0 {
			return "The deadline shipped it for you. Production reviewed it instead."
		}
		return "The deadline shipped it for you. It held, this time."
	}
	if r.Incidents == 0 {
		switch {
		case m.RejectedGood > 0:
			return "Nothing got past you, including some good work."
		case r.Delivered < r.Required:
			return "Nothing broke. Not everything was built."
		}
		return "Every call was right. Nothing happened."
	}
	readPast := false
	for _, d := range g.decisions {
		if d.Accepted && d.Flawed && d.FlawRead {
			readPast = true
		}
	}
	switch {
	case r.Worst == scenario.Critical && readPast:
		return "You opened the hunk. You read past the line."
	case r.Worst == scenario.Critical && r.Coverage < 0.3:
		return "Approved without reading. Production read it for you."
	case r.Worst == scenario.Critical:
		return "Shipped. Broke in production."
	case m.AcceptedBad > m.RejectedBad:
		return "Shipped. Most of what review should have caught, production caught instead."
	case r.Incidents == 1:
		return "Shipped. Production found the one review missed."
	}
	return "Shipped. Production found what review missed."
}

func clamp(v, lo, hi int) int {
	return max(lo, min(hi, v))
}
