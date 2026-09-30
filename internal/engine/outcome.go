package engine

import (
	"fmt"
	"slices"

	"github.com/butaraul/lgtm/internal/scenario"
)

// ShipHour is the wall-clock hour on day 0 when a release goes out.
// Incident times are minutes after it.
const ShipHour = 17

// Incident is a shipped flaw surfacing in production.
type Incident struct {
	Seq      int // candidate that carried the flaw
	Type     scenario.FlawType
	Severity scenario.Severity
	At       int // minutes after ship
	Title    string
	Detail   string
	Cost     string
}

// Outcome is what happened after the run ended.
type Outcome struct {
	End       EndReason
	Shipped   bool
	Incidents []Incident
	// Missing is every requirement that was never built, excluding ones an
	// accepted flaw dropped; those surface as incidents.
	Missing []scenario.Requirement
}

// Outcome is nil until the run is over.
func (g *Game) Outcome() *Outcome { return g.outcome }

// FormatIncidentTime renders minutes after ship as a day and wall-clock
// time, counting the ship day as day 0.
func FormatIncidentTime(at int) string {
	wall := ShipHour*60 + at
	d, m := wall/1440, wall%1440
	return fmt.Sprintf("Day %d %02d:%02d", d, m/60, m%60)
}

func (g *Game) resolveOutcome() *Outcome {
	o := &Outcome{End: g.end, Shipped: g.end != EndHeld}
	if !o.Shipped {
		return o
	}
	dropped := map[string]bool{}
	for _, d := range g.decisions {
		if !d.Accepted || !d.Flawed {
			continue
		}
		c := g.cands[d.Seq-1]
		v := c.variant
		for _, id := range v.Drops {
			dropped[id] = true
		}
		sev := v.Incident.Severity
		if sev == "" {
			sev = v.Type.DefaultSeverity()
		}
		day := v.Type.DefaultDay()
		if v.Incident.Day != nil {
			day = *v.Incident.Day
		}
		r := g.roll(c.turn.ID, v.ID, "incident")
		at := 20 + int(r*280) // day 0: within hours of the release
		if day > 0 {
			at = day*1440 - ShipHour*60 + int(r*1440)
		}
		o.Incidents = append(o.Incidents, Incident{
			Seq:      c.Seq,
			Type:     v.Type,
			Severity: sev,
			At:       at,
			Title:    v.Incident.Title,
			Detail:   v.Incident.Detail,
			Cost:     v.Incident.Cost,
		})
	}
	slices.SortStableFunc(o.Incidents, func(a, b Incident) int { return a.At - b.At })
	for _, r := range g.reqs {
		if !g.delivered(r.ID) && !dropped[r.ID] {
			o.Missing = append(o.Missing, r)
		}
	}
	return o
}
