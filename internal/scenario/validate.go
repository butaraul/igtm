package scenario

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/butaraul/lgtm/internal/diff"
)

var idPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// DefaultBelow is the context health under which a decay variant fires
// when the scenario does not set one.
const DefaultBelow = 0.4

// Files per candidate diff.
const (
	minFiles = 2
	maxFiles = 4
)

// validate checks the scenario, fills defaults, parses every diff and
// applies every patch. Problems are appended to p.
func (s *Scenario) validate(p *Problems) {
	if !idPattern.MatchString(s.ID) {
		p.add("id", "must be lowercase words joined by hyphens, got %q", s.ID)
	}
	required(p, "title", s.Title)
	required(p, "summary", s.Summary)
	required(p, "brief", s.Brief)
	required(p, "client.name", s.Client.Name)
	required(p, "client.role", s.Client.Role)
	if s.Difficulty < 1 || s.Difficulty > 4 {
		p.add("difficulty", "must be 1 to 4, got %d", s.Difficulty)
	}
	if s.Budget.Clock <= 0 || s.Budget.Tokens <= 0 || s.Budget.Context <= 0 {
		p.add("budget", "clock, tokens and context must all be positive")
	}

	reqs := map[string]bool{}
	checkReqs := func(at string, list []Requirement) {
		for i := range list {
			r := &list[i]
			rp := fmt.Sprintf("%s[%d]", at, i)
			if !idPattern.MatchString(r.ID) {
				p.add(rp+".id", "invalid id %q", r.ID)
			}
			if reqs[r.ID] {
				p.add(rp+".id", "duplicate requirement %q", r.ID)
			}
			reqs[r.ID] = true
			required(p, rp+".text", r.Text)
			if r.Weight < 0 {
				p.add(rp+".weight", "must not be negative")
			}
			if r.Weight == 0 {
				r.Weight = 1
			}
		}
	}
	checkReqs("requirements", s.Requirements)
	if len(s.Requirements) == 0 {
		p.add("requirements", "at least one is required")
	}
	for i, c := range s.Changes {
		checkReqs(fmt.Sprintf("changes[%d].adds", i), c.Adds)
	}

	turns := map[string]bool{}
	base := map[string]bool{}
	always := 0
	checkTurn := func(at string, t *Turn) {
		if t == nil {
			p.add(at, "empty turn")
			return
		}
		if turns[t.ID] {
			p.add(at+".id", "duplicate turn %q", t.ID)
		}
		turns[t.ID] = true
		always += t.validate(p, at, reqs)
	}
	if len(s.Turns) == 0 {
		p.add("turns", "at least one is required")
	}
	for i, t := range s.Turns {
		checkTurn(fmt.Sprintf("turns[%d]", i), t)
		if t != nil {
			base[t.ID] = true
		}
	}
	for i, c := range s.Changes {
		for j, t := range c.Turns {
			checkTurn(fmt.Sprintf("changes[%d].turns[%d]", i, j), t)
		}
		if c.Rework != nil {
			checkTurn(fmt.Sprintf("changes[%d].rework", i), c.Rework)
		}
	}
	if always == 0 {
		p.add("turns", "no variant has plant: always; every run needs at least one guaranteed flaw")
	}

	changes := map[string]bool{}
	for i, c := range s.Changes {
		at := fmt.Sprintf("changes[%d]", i)
		if !idPattern.MatchString(c.ID) {
			p.add(at+".id", "invalid id %q", c.ID)
		}
		if changes[c.ID] {
			p.add(at+".id", "duplicate change %q", c.ID)
		}
		changes[c.ID] = true
		if !base[c.After] {
			p.add(at+".after", "must name a turn in turns, got %q", c.After)
		}
		if c.Kind != ScopeCreep && c.Kind != Pivot {
			p.add(at+".kind", "must be scope_creep or pivot, got %q", c.Kind)
		}
		required(p, at+".message", c.Message)
		for _, id := range c.Removes {
			if !reqs[id] {
				p.add(at+".removes", "unknown requirement %q", id)
			}
		}
		for _, id := range c.Cancels {
			if !base[id] {
				p.add(at+".cancels", "must name a turn in turns, got %q", id)
			}
		}
		for _, id := range c.Conflicts {
			if !turns[id] {
				p.add(at+".conflicts", "unknown turn %q", id)
			}
		}
		if len(c.Conflicts) > 0 && c.Rework == nil {
			p.add(at+".rework", "required when conflicts are listed")
		}
		if len(c.Conflicts) == 0 && c.Rework != nil {
			p.add(at+".conflicts", "rework is set but no conflicting turns are listed")
		}
	}
}

// validate checks one turn and its variants and returns how many variants
// are planted always.
func (t *Turn) validate(p *Problems, at string, reqs map[string]bool) int {
	if !idPattern.MatchString(t.ID) {
		p.add(at+".id", "invalid id %q", t.ID)
	}
	required(p, at+".title", t.Title)
	required(p, at+".message", t.Message)
	required(p, at+".tests.output", t.Tests.Output)
	required(p, at+".explain.text", t.Explain.Text)
	for _, id := range t.Implements {
		if !reqs[id] {
			p.add(at+".implements", "unknown requirement %q", id)
		}
	}
	d, err := parseCandidate(t.Diff)
	if err != nil {
		p.add(at+".diff", "%v", err)
		return 0
	}
	t.parsed = d

	always := 0
	ids := map[string]bool{}
	targets := map[string]bool{}
	for i, v := range t.Variants {
		vp := fmt.Sprintf("%s.variants[%d]", at, i)
		if !idPattern.MatchString(v.ID) {
			p.add(vp+".id", "invalid id %q", v.ID)
		}
		if ids[v.ID] {
			p.add(vp+".id", "duplicate variant %q", v.ID)
		}
		ids[v.ID] = true
		if v.Then != "" {
			targets[v.Then] = true
		}
		v.validate(p, vp, t)
		if v.Plant == PlantAlways {
			always++
		}
	}
	if always > 1 {
		p.add(at+".variants", "at most one variant per turn may be planted always")
	}
	for i, v := range t.Variants {
		if v.Plant == PlantRevision && !targets[v.ID] {
			p.add(fmt.Sprintf("%s.variants[%d]", at, i), "revision variant %q is not the then of any variant", v.ID)
		}
	}
	// Follow every then chain; a chain that revisits a variant never ends.
	for _, v := range t.Variants {
		seen := map[string]bool{v.ID: true}
		for cur := v; cur != nil && cur.Then != ""; {
			if seen[cur.Then] {
				p.add(at+".variants", "then chain from %q loops", v.ID)
				break
			}
			seen[cur.Then] = true
			cur = t.Variant(cur.Then)
		}
	}
	return always
}

func (v *Variant) validate(p *Problems, at string, t *Turn) {
	if !v.Type.Valid() {
		p.add(at+".type", "unknown flaw type %q", v.Type)
	}
	switch v.Plant {
	case PlantAlways, PlantRevision:
	case PlantSeeded:
		if v.Chance < 0 || v.Chance > 1 {
			p.add(at+".chance", "must be between 0 and 1")
		}
	case PlantDecay:
		if v.Below < 0 || v.Below >= 1 {
			p.add(at+".below", "must be between 0 and 1")
		}
		if v.Below == 0 {
			v.Below = DefaultBelow
		}
	default:
		p.add(at+".plant", "must be always, seeded, decay or revision, got %q", v.Plant)
	}
	if v.Plant != PlantSeeded && v.Chance != 0 {
		p.add(at+".chance", "only applies to seeded variants")
	}
	if v.Plant != PlantDecay && v.Below != 0 {
		p.add(at+".below", "only applies to decay variants")
	}
	required(p, at+".fix", v.Fix)
	required(p, at+".spotted", v.Spotted)
	required(p, at+".line", v.Line)
	required(p, at+".incident.title", v.Incident.Title)
	required(p, at+".incident.detail", v.Incident.Detail)
	required(p, at+".incident.cost", v.Incident.Cost)
	if v.Incident.Severity != "" && v.Incident.Severity.Rank() == 0 {
		p.add(at+".incident.severity", "must be low, medium, high or critical")
	}
	if v.Incident.Day != nil && *v.Incident.Day < 0 {
		p.add(at+".incident.day", "must not be negative")
	}
	if v.Tests != nil {
		required(p, at+".tests.output", v.Tests.Output)
	}
	if v.Explain != nil {
		required(p, at+".explain.text", v.Explain.Text)
	}
	if v.Then != "" {
		target := t.Variant(v.Then)
		switch {
		case v.Then == v.ID:
			p.add(at+".then", "a variant cannot follow itself")
		case target == nil:
			p.add(at+".then", "unknown variant %q", v.Then)
		case target.Plant != PlantRevision:
			p.add(at+".then", "target %q must have plant: revision", v.Then)
		}
	}
	if v.Type == DroppedRequirement && len(v.Drops) == 0 {
		p.add(at+".drops", "a dropped_requirement variant must name what it drops")
	}
	if v.Type != DroppedRequirement && len(v.Drops) > 0 {
		p.add(at+".drops", "only dropped_requirement variants drop requirements")
	}
	for _, id := range v.Drops {
		if !slices.Contains(t.Implements, id) {
			p.add(at+".drops", "%q is not implemented by this turn", id)
		}
	}

	if len(v.Patch) == 0 {
		p.add(at+".patch", "at least one edit is required")
		return
	}
	src := t.Diff
	for i, e := range v.Patch {
		ep := fmt.Sprintf("%s.patch[%d]", at, i)
		if e.Find == "" {
			p.add(ep+".find", "must not be empty")
			return
		}
		if n := strings.Count(src, e.Find); n != 1 {
			p.add(ep+".find", "must match the diff exactly once, matched %d times: %q", n, clip(e.Find))
			return
		}
		src = strings.Replace(src, e.Find, e.Replace, 1)
	}
	if src == t.Diff {
		p.add(at+".patch", "leaves the diff unchanged")
		return
	}
	d, err := parseCandidate(src)
	if err != nil {
		p.add(at+".patch", "patched diff: %v", err)
		return
	}
	v.parsed = d
	if v.Line == "" {
		return
	}
	locs := d.FindAdded(v.Line)
	if len(locs) != 1 {
		p.add(at+".line", "must match exactly one added line, matched %d: %q", len(locs), v.Line)
		return
	}
	v.loc = locs[0]
}

func parseCandidate(src string) (*diff.Diff, error) {
	d, err := diff.Parse(src)
	if err != nil {
		return nil, err
	}
	if n := len(d.Files); n < minFiles || n > maxFiles {
		return nil, fmt.Errorf("touches %d files; a candidate touches %d to %d", n, minFiles, maxFiles)
	}
	return d, nil
}

func required(p *Problems, at, v string) {
	if strings.TrimSpace(v) == "" {
		p.add(at, "required")
	}
}

func clip(s string) string {
	s = strings.ReplaceAll(s, "\n", `\n`)
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}
