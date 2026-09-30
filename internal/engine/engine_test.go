package engine

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/butaraul/lgtm/internal/scenario"
)

const fixtureYAML = `
id: fixture
title: Fixture
difficulty: 2
summary: Engine test fixture.
client: {name: Test Client, role: Tester}
brief: Build it.
requirements:
  - {id: core, text: Core, weight: 2}
  - {id: pay, text: Payments}
  - {id: extra, text: Extra}
budget: {clock: 900, tokens: 200000, context: 6000}
turns:
  - id: t1
    title: Core
    implements: [core]
    message: Core done.
    diff: |
      diff --git a/core.go b/core.go
      --- a/core.go
      +++ b/core.go
      @@ -1,2 +1,4 @@
       package core
      +
      +func Run() {}
       // end
      diff --git a/core_test.go b/core_test.go
      --- a/core_test.go
      +++ b/core_test.go
      @@ -1,1 +1,3 @@
       package core
      +
      +func TestRun(t *testing.T) { Run() }
    tests: {pass: true, output: ok core}
    explain: {text: Adds Run., truthful: true}
  - id: t2
    title: Payments
    implements: [pay]
    message: Payments done.
    diff: |
      diff --git a/pay.go b/pay.go
      --- a/pay.go
      +++ b/pay.go
      @@ -1,1 +1,3 @@
       package pay
      +
      +var key = os.Getenv("KEY")
      diff --git a/pay_test.go b/pay_test.go
      --- a/pay_test.go
      +++ b/pay_test.go
      @@ -1,1 +1,2 @@
       package pay
      +// tests
    tests: {pass: true, output: ok pay}
    explain: {text: Reads the key from env., truthful: true}
    variants:
      - id: leak
        type: hardcoded_secret
        plant: always
        patch:
          - {find: '+var key = os.Getenv("KEY")', replace: '+var key = "sk_live_123"'}
        line: sk_live_123
        fix: Moved the key to env.
        then: public
        spotted: Live key.
        incident: {title: Key leaked, detail: Scraped., cost: Lots.}
      - id: public
        type: insecure_default
        plant: revision
        patch:
          - {find: '+var key = os.Getenv("KEY")', replace: '+var key = os.Getenv("PUBLIC_KEY")'}
        line: PUBLIC_KEY
        fix: Used the private env var.
        spotted: Public env var.
        incident: {title: Key exposed, detail: Bundled., cost: Some., severity: medium, day: 3}
  - id: t3
    title: Extra
    implements: [extra]
    message: Extra done.
    diff: |
      diff --git a/extra.go b/extra.go
      --- a/extra.go
      +++ b/extra.go
      @@ -10,2 +10,3 @@ func Extra() {
         for i := 0; i < n; i++ {
      +    use(i)
         }
      diff --git a/extra_test.go b/extra_test.go
      --- a/extra_test.go
      +++ b/extra_test.go
      @@ -1,1 +1,2 @@
       package extra
      +// tests
    tests: {pass: true, output: ok extra}
    explain: {text: Loops., truthful: true}
    variants:
      - id: bound
        type: off_by_one
        plant: seeded
        chance: 0.5
        patch:
          - {find: '+    use(i)', replace: '+    use(i + 1)'}
        line: use(i + 1)
        fix: Fixed the bound.
        spotted: Off by one.
        incident: {title: Wrong total, detail: Off by one., cost: Refunds.}
      - id: forgot
        type: dropped_requirement
        plant: decay
        below: 0.5
        drops: [extra]
        patch:
          - {find: '+    use(i)', replace: '+    _ = i'}
        line: _ = i
        fix: Restored the call.
        spotted: Dropped.
        incident: {title: Missing extra, detail: Never built., cost: Client unhappy.}
changes:
  - id: more
    after: t1
    kind: scope_creep
    message: Add a report too.
    adds:
      - {id: report, text: Report}
    turns:
      - id: report
        title: Report
        implements: [report]
        message: Report added.
        diff: |
          diff --git a/report.go b/report.go
          --- /dev/null
          +++ b/report.go
          @@ -0,0 +1,2 @@
          +package report
          +func Build() {}
          diff --git a/report_test.go b/report_test.go
          --- /dev/null
          +++ b/report_test.go
          @@ -0,0 +1,2 @@
          +package report
          +// tests
        tests: {pass: true, output: ok report}
        explain: {text: Adds Build., truthful: true}
    conflicts: [t1]
    rework:
      id: rework
      title: Rework core
      implements: [core]
      message: Reworked core for reports.
      diff: |
        diff --git a/core.go b/core.go
        --- a/core.go
        +++ b/core.go
        @@ -3,1 +3,1 @@
        -func Run() {}
        +func Run() { report.Build() }
        diff --git a/core_test.go b/core_test.go
        --- a/core_test.go
        +++ b/core_test.go
        @@ -3,1 +3,1 @@
        -func TestRun(t *testing.T) { Run() }
        +func TestRun(t *testing.T) { Run(); Run() }
      tests: {pass: true, output: ok core}
      explain: {text: Calls Build., truthful: true}
`

func fixture(t *testing.T) *scenario.Scenario {
	t.Helper()
	s, err := scenario.Load("fixture.yaml", []byte(fixtureYAML))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newGame(t *testing.T, seed uint64) *Game {
	t.Helper()
	g, err := New(fixture(t), Config{Seed: seed, Difficulty: Normal})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func must(t *testing.T, g *Game, a Action) {
	t.Helper()
	if err := g.Apply(a); err != nil {
		t.Fatalf("%s: %v", a.Kind, err)
	}
}

var (
	start  = Action{Kind: ActStart}
	accept = Action{Kind: ActAccept}
	ship   = Action{Kind: ActShip}
)

func rejectFor(reason string) Action { return Action{Kind: ActReject, Reason: reason} }

func curTurn(g *Game) string {
	if g.cur == nil {
		return ""
	}
	if g.cur.variant != nil {
		return g.cur.turn.ID + "/" + g.cur.variant.ID
	}
	return g.cur.turn.ID
}

func TestCalibration(t *testing.T) {
	cases := []struct {
		name string
		m    Matrix
		want int
	}{
		{"no decisions", Matrix{}, 0},
		{"all clean accepted", Matrix{AcceptedGood: 10}, 100},
		{"rubber stamp", Matrix{AcceptedGood: 8, AcceptedBad: 2}, 50},
		{"reject everything", Matrix{RejectedGood: 8, RejectedBad: 2}, 50},
		{"perfect", Matrix{AcceptedGood: 8, RejectedBad: 2}, 100},
		{"one of each mistake", Matrix{AcceptedGood: 7, AcceptedBad: 1, RejectedGood: 1, RejectedBad: 1}, 69},
		{"only flaws, all caught", Matrix{RejectedBad: 3}, 100},
		{"inverted", Matrix{AcceptedBad: 2, RejectedGood: 8}, 0},
	}
	for _, c := range cases {
		if got := c.m.Calibration(); got != c.want {
			t.Errorf("%s: calibration = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestGrade(t *testing.T) {
	for score, want := range map[int]string{100: "A", 90: "A", 89: "B", 80: "B", 75: "C", 60: "D", 59: "F", 0: "F"} {
		if got := grade(score); got != want {
			t.Errorf("grade(%d) = %s, want %s", score, got, want)
		}
	}
}

func TestPhaseGuards(t *testing.T) {
	g := newGame(t, 1)
	for _, a := range []Action{accept, rejectFor("other"), {Kind: ActTests}, ship, {Kind: ActHold}} {
		if err := g.Apply(a); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s in brief: err = %v, want ErrInvalid", a.Kind, err)
		}
	}
	must(t, g, start)
	if err := g.Apply(start); !errors.Is(err, ErrInvalid) {
		t.Errorf("second start: %v", err)
	}
	if err := g.Apply(rejectFor("vibes")); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown reason: %v", err)
	}
	if err := g.Apply(Action{Kind: ActInspect, Hunk: 9}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad hunk: %v", err)
	}
	must(t, g, Action{Kind: ActTests})
	if err := g.Apply(Action{Kind: ActTests}); !errors.Is(err, ErrNoop) {
		t.Errorf("tests twice: %v", err)
	}
	must(t, g, Action{Kind: ActInspect, Hunk: 1})
	if err := g.Apply(Action{Kind: ActInspect, Hunk: 1}); !errors.Is(err, ErrNoop) {
		t.Errorf("inspect twice: %v", err)
	}
	if n := len(g.Actions()); n != 3 {
		t.Errorf("recorded %d actions, want 3 (errors are not recorded)", n)
	}
}

func TestFlawResolution(t *testing.T) {
	cases := []struct {
		name    string
		reasons []string // rejections applied to t2 in order
		want    []string // candidate after each rejection
	}{
		{"correct reason gets the fix, which has its own flaw", []string{"hardcoded_secret"}, []string{"t2/public"}},
		{"fix of the fix is clean", []string{"hardcoded_secret", "insecure_default"}, []string{"t2/public", "t2"}},
		{"third rejection drops the work", []string{"hardcoded_secret", "insecure_default", "other"}, []string{"t2/public", "t2", "t3"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newGame(t, 7)
			must(t, g, start)
			must(t, g, rejectFor(ReasonOther)) // t1 is clean: resubmitted as-is
			if got := curTurn(g); got != "t1" || !g.cur.Resubmitted {
				t.Fatalf("after rejecting clean t1: %s resubmitted=%v", got, g.cur.Resubmitted)
			}
			must(t, g, accept) // t1; change fires: rework, report
			must(t, g, accept) // rework
			must(t, g, accept) // report
			if got := curTurn(g); got != "t2/leak" {
				t.Fatalf("t2 should be planted with leak, got %s", got)
			}
			for i, r := range c.reasons {
				must(t, g, rejectFor(r))
				if got := curTurn(g); !strings.HasPrefix(got, c.want[i]) {
					t.Errorf("after rejection %d (%s): %s, want %s", i+1, r, got, c.want[i])
				}
			}
		})
	}
}

func TestWrongReasonIsSeeded(t *testing.T) {
	fixed, unchanged := 0, 0
	for seed := uint64(0); seed < 200; seed++ {
		run := func() string {
			g := newGame(t, seed)
			for _, a := range []Action{start, accept, accept, accept, rejectFor("off_by_one")} {
				must(t, g, a)
			}
			return curTurn(g)
		}
		first := run()
		if second := run(); first != second {
			t.Fatalf("seed %d: %s then %s", seed, first, second)
		}
		switch first {
		case "t2/public":
			fixed++
		case "t2/leak":
			unchanged++
		default:
			t.Fatalf("seed %d: unexpected %s", seed, first)
		}
	}
	// wrongReasonFix is 0.3; allow generous slack for 200 samples.
	if fixed < 30 || fixed > 90 {
		t.Errorf("wrong-reason fix rate %d/200, expected near 60", fixed)
	}
	if unchanged == 0 {
		t.Error("wrong reason never left the flaw in place")
	}
}

func TestSeededPlanting(t *testing.T) {
	planted := 0
	for seed := uint64(0); seed < 300; seed++ {
		g := newGame(t, seed)
		for _, a := range []Action{start, accept, accept, Action{Kind: ActSummarise}, accept, rejectFor("hardcoded_secret"), rejectFor("insecure_default"), Action{Kind: ActSummarise}, accept} {
			must(t, g, a)
		}
		switch got := curTurn(g); got {
		case "t3/bound":
			planted++
		case "t3":
		default:
			t.Fatalf("seed %d: unexpected %s", seed, got)
		}
	}
	// chance 0.5 at healthy context; allow slack.
	if planted < 110 || planted > 190 {
		t.Errorf("planted %d/300, expected near 150", planted)
	}
}

func TestContextDecay(t *testing.T) {
	// Without summarising, t3 arrives on a degraded context and the agent
	// forgets the requirement.
	g := newGame(t, 3)
	must(t, g, start)
	prev := g.health()
	for range 3 {
		must(t, g, accept)
		if h := g.health(); h >= prev && !strings.Contains(g.log[len(g.log)-1].Text, "compacted") {
			t.Errorf("health did not fall: %.2f -> %.2f", prev, h)
		}
		prev = g.health()
	}
	must(t, g, rejectFor("hardcoded_secret"))
	must(t, g, rejectFor("insecure_default"))
	must(t, g, accept)
	if got := curTurn(g); got != "t3/forgot" {
		t.Fatalf("degraded context: got %s, want t3/forgot (health %.2f)", got, g.health())
	}
	if g.cur.Health >= 0.5 && !strings.Contains(joinLog(g), "compacted") {
		t.Errorf("decay variant planted at health %.2f with no compaction", g.cur.Health)
	}

	// Summarising first keeps the constraint in mind.
	g = newGame(t, 3)
	for _, a := range []Action{start, accept, accept, accept, rejectFor("hardcoded_secret"), rejectFor("insecure_default"), {Kind: ActSummarise}} {
		must(t, g, a)
	}
	if h := g.health(); h < 0.87 || h > 0.89 {
		t.Errorf("health after summary = %.2f, want 0.88", h)
	}
	must(t, g, accept)
	if got := curTurn(g); got == "t3/forgot" {
		t.Errorf("fresh context still planted the decay variant")
	}
}

func TestAutoCompactionForgets(t *testing.T) {
	g := newGame(t, 11)
	must(t, g, start)
	g.ctx = g.window // force a full context
	must(t, g, accept)
	if !strings.Contains(joinLog(g), "compacted") {
		t.Fatal("no auto-compaction logged")
	}
	if !g.forgot {
		t.Fatal("compaction should leave the agent forgetful")
	}
	must(t, g, accept) // rework
	must(t, g, accept) // report
	must(t, g, Action{Kind: ActSummarise})
	if g.forgot {
		t.Error("summarising should restore what was forgotten")
	}
}

func joinLog(g *Game) string {
	var b strings.Builder
	for _, e := range g.log {
		b.WriteString(e.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestChangeConflicts(t *testing.T) {
	g := newGame(t, 5)
	must(t, g, start)
	must(t, g, accept)
	if got := curTurn(g); got != "rework" {
		t.Errorf("accepted t1 conflicts with the change: next = %s, want rework", got)
	}
	if n := len(g.Requirements()); n != 4 {
		t.Errorf("requirements = %d, want 4 after the change adds one", n)
	}

	g = newGame(t, 5)
	must(t, g, start)
	for range maxRejections {
		must(t, g, rejectFor(ReasonOther))
	}
	if got := curTurn(g); got != "report" {
		t.Errorf("dropped t1 does not conflict: next = %s, want report", got)
	}
}

func TestOutcome(t *testing.T) {
	g := newGame(t, 9)
	for _, a := range []Action{start, accept, accept, accept, accept} {
		must(t, g, a)
	}
	must(t, g, ship) // t3 left in review
	if g.Phase() != PhaseOver {
		t.Fatal("not over after ship")
	}
	o := g.Outcome()
	if !o.Shipped || len(o.Incidents) != 1 {
		t.Fatalf("outcome = %+v", o)
	}
	in := o.Incidents[0]
	if in.Type != scenario.HardcodedSecret || in.Severity != scenario.Critical {
		t.Errorf("incident = %+v", in)
	}
	if day := (ShipHour*60 + in.At) / 1440; day != scenario.HardcodedSecret.DefaultDay() {
		t.Errorf("incident day = %d, want the type default %d", day, scenario.HardcodedSecret.DefaultDay())
	}
	if len(o.Missing) != 1 || o.Missing[0].ID != "extra" {
		t.Errorf("missing = %+v, want extra", o.Missing)
	}
	r := g.Report()
	if r.Matrix != (Matrix{AcceptedGood: 3, AcceptedBad: 1}) {
		t.Errorf("matrix = %+v", r.Matrix)
	}
	if r.Production != 30 || r.Worst != scenario.Critical {
		t.Errorf("production = %d worst = %s", r.Production, r.Worst)
	}
	// core(2)+pay(1)+report(1) delivered of 5 weight = 80, minus 30 for the critical.
	if r.Satisfaction != 50 || r.Delivered != 3 || r.Required != 4 {
		t.Errorf("satisfaction = %d delivered %d/%d", r.Satisfaction, r.Delivered, r.Required)
	}
	if want := int(0.4*float64(r.Calibration) + 0.35*30 + 0.25*50 + 0.5); r.Score != want {
		t.Errorf("score = %d, want %d", r.Score, want)
	}
}

func TestHeld(t *testing.T) {
	g := newGame(t, 9)
	must(t, g, start)
	must(t, g, Action{Kind: ActHold})
	r := g.Report()
	if r.Shipped || r.Satisfaction != 0 || r.Production != 100 || r.Incidents != 0 {
		t.Errorf("held report = %+v", r)
	}
	if !strings.Contains(r.Verdict, "Nothing shipped") {
		t.Errorf("verdict = %q", r.Verdict)
	}
}

func TestDeadline(t *testing.T) {
	s := fixture(t)
	s.Budget.Clock = 20
	g, err := New(s, Config{Seed: 1, Difficulty: Normal})
	if err != nil {
		t.Fatal(err)
	}
	must(t, g, start)
	must(t, g, accept)
	for g.Phase() != PhaseOver {
		if err := g.Apply(Action{Kind: ActTests}); err != nil {
			must(t, g, accept)
		}
	}
	if o := g.Outcome(); o.End != EndDeadline || !o.Shipped {
		t.Errorf("outcome = %+v, want deadline ship", o)
	}
	if err := g.Apply(accept); !errors.Is(err, ErrInvalid) {
		t.Errorf("action after the end: %v", err)
	}
}

func TestTokensExhausted(t *testing.T) {
	s := fixture(t)
	s.Budget.Tokens = 3000
	g, err := New(s, Config{Seed: 1, Difficulty: Normal})
	if err != nil {
		t.Fatal(err)
	}
	must(t, g, start)
	for g.Phase() == PhaseReview {
		must(t, g, accept)
	}
	if g.Phase() != PhaseShip {
		t.Fatalf("phase = %d, want ship", g.Phase())
	}
	if !strings.Contains(joinLog(g), "Token budget spent") {
		t.Error("no token message")
	}
	if err := g.Apply(Action{Kind: ActTests}); !errors.Is(err, ErrInvalid) {
		t.Errorf("tests with no tokens: %v", err)
	}
}

func TestDifficultyScalesBudget(t *testing.T) {
	s := fixture(t)
	for d, want := range map[Difficulty]int{Easy: 1170, Normal: 900, Hard: 765} {
		g, err := New(s, Config{Difficulty: d})
		if err != nil {
			t.Fatal(err)
		}
		if got := g.Resources().ClockBudget; got != want {
			t.Errorf("%s clock = %d, want %d", d, got, want)
		}
	}
	if _, err := ParseDifficulty("brutal"); err == nil {
		t.Error("unknown difficulty accepted")
	}
}

// policy is a fixed, state-dependent strategy used to check determinism.
func policy(g *Game) Action {
	switch g.Phase() {
	case PhaseBrief:
		return start
	case PhaseShip:
		return ship
	}
	c := g.Current()
	for i := range c.Diff.NumHunks() {
		if !g.Inspected(i) {
			return Action{Kind: ActInspect, Hunk: i}
		}
	}
	if !c.TestsRun {
		return Action{Kind: ActTests}
	}
	if c.Seq%3 == 0 && !c.Resubmitted {
		return rejectFor(ReasonOther)
	}
	return accept
}

func playOut(t *testing.T, seed uint64) *Game {
	t.Helper()
	g := newGame(t, seed)
	for i := 0; g.Phase() != PhaseOver; i++ {
		if i > 500 {
			t.Fatal("run did not end")
		}
		must(t, g, policy(g))
	}
	return g
}

func TestDeterminism(t *testing.T) {
	for _, seed := range []uint64{1, 42, 20260930} {
		a, b := playOut(t, seed), playOut(t, seed)
		if !reflect.DeepEqual(a.Log(), b.Log()) {
			t.Errorf("seed %d: logs differ", seed)
		}
		if !reflect.DeepEqual(a.Report(), b.Report()) {
			t.Errorf("seed %d: reports differ", seed)
		}
		c, err := Replay(fixture(t), a.Config(), a.Actions())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a.Log(), c.Log()) || !reflect.DeepEqual(a.Report(), c.Report()) {
			t.Errorf("seed %d: replay differs", seed)
		}
	}
	if _, err := Replay(fixture(t), Config{Seed: 1}, []Action{accept}); err == nil {
		t.Error("replaying an invalid action should fail")
	}
}

func TestPostMortem(t *testing.T) {
	g := newGame(t, 9)
	for _, a := range []Action{start, accept, accept, accept, {Kind: ActInspect, Hunk: 0}, accept} {
		must(t, g, a)
	}
	must(t, g, rejectFor("off_by_one"))
	must(t, g, ship)
	pm := g.PostMortem()

	var bad, client *Item
	for i := range pm.Items {
		it := &pm.Items[i]
		switch {
		case it.Entry != nil && it.Entry.Call == AcceptedBad:
			bad = it
		case it.Client != "":
			client = it
		}
	}
	if client == nil || client.Client != "Add a report too." {
		t.Errorf("client change missing from timeline")
	}
	if bad == nil {
		t.Fatal("no accepted-bad entry")
	}
	f := bad.Entry.Flaw
	if f.File != "pay.go" || f.Line != 3 || !strings.Contains(f.Text, "sk_live_123") || !f.Read {
		t.Errorf("flaw = %+v", f)
	}
	if f.Excerpt[f.Mark].Text != f.Text {
		t.Errorf("mark points at %q", f.Excerpt[f.Mark].Text)
	}
	if bad.Entry.Incident == nil || bad.Entry.Incident.Title != "Key leaked" {
		t.Errorf("incident = %+v", bad.Entry.Incident)
	}
	if len(pm.Lessons) == 0 || pm.Lessons[0].Type != scenario.HardcodedSecret || pm.Lessons[0].Missed != 1 {
		t.Errorf("lessons = %+v", pm.Lessons)
	}
	r := g.Report()
	if r.Verdict != "You opened the hunk. You read past the line." {
		t.Errorf("verdict = %q", r.Verdict)
	}
}

func TestVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		actions []Action
		want    string
	}{
		{"clean sweep", []Action{start, accept, accept, accept, rejectFor("hardcoded_secret"), rejectFor("insecure_default"), accept}, ""},
		{"rubber stamp", []Action{start, accept, accept, accept, accept}, "Approved without reading. Production read it for you."},
		{"paranoid", []Action{start, rejectFor(ReasonOther), accept, accept, accept, rejectFor("hardcoded_secret"), rejectFor("insecure_default"), accept}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newGame(t, 9)
			for _, a := range c.actions {
				must(t, g, a)
			}
			// Finish t3 cleanly whatever was planted.
			for g.Phase() == PhaseReview && g.cur.variant != nil {
				must(t, g, rejectFor(string(g.cur.variant.Type)))
			}
			if g.Phase() == PhaseReview {
				must(t, g, accept)
			}
			must(t, g, ship)
			r := g.Report()
			want := c.want
			if want == "" {
				switch c.name {
				case "clean sweep":
					want = "Every call was right. Nothing happened."
				case "paranoid":
					want = "Nothing got past you, including some good work."
				}
			}
			if r.Verdict != want {
				t.Errorf("verdict = %q, want %q (report %+v)", r.Verdict, want, r)
			}
		})
	}
}

func TestFormatting(t *testing.T) {
	if got := FormatClock(0); got != "Day 1 09:00" {
		t.Errorf("FormatClock(0) = %s", got)
	}
	if got := FormatClock(8*60 + 95); got != "Day 2 10:35" {
		t.Errorf("FormatClock = %s", got)
	}
	if got := FormatDuration(185); got != "3h05m" {
		t.Errorf("FormatDuration = %s", got)
	}
	if got := FormatDuration(-4); got != "0m" {
		t.Errorf("FormatDuration(-4) = %s", got)
	}
	if got := FormatIncidentTime(2*1440 + 30); got != "Day 2 17:30" {
		t.Errorf("FormatIncidentTime = %s", got)
	}
}
