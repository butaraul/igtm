package scenario

import (
	"strings"
	"testing"
)

func TestEmbeddedScenariosValidate(t *testing.T) {
	list, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("no scenarios embedded")
	}
	for i := 1; i < len(list); i++ {
		if list[i].Difficulty < list[i-1].Difficulty {
			t.Errorf("scenarios not ordered by difficulty: %s before %s", list[i-1].ID, list[i].ID)
		}
	}
	for _, s := range list {
		for _, turn := range s.Turns {
			if turn.Parsed() == nil {
				t.Errorf("%s/%s: diff not parsed", s.ID, turn.ID)
			}
			for _, v := range turn.Variants {
				if v.Parsed() == nil {
					t.Errorf("%s/%s/%s: variant diff not parsed", s.ID, turn.ID, v.ID)
				}
			}
		}
	}
	if Find(list, list[0].ID) != list[0] || Find(list, "nope") != nil {
		t.Error("Find")
	}
}

// minimal is a valid scenario that each case below breaks in one way.
const minimal = `
id: tiny
title: Tiny
difficulty: 1
summary: A test scenario.
client: {name: Test Client, role: Tester}
brief: Build the thing.
requirements:
  - {id: thing, text: The thing}
budget: {clock: 60, tokens: 1000, context: 1000}
turns:
  - id: one
    title: First
    implements: [thing]
    message: Did the thing.
    diff: |
      diff --git a/a.go b/a.go
      --- a/a.go
      +++ b/a.go
      @@ -1,1 +1,2 @@
       package a
      +const key = "env"
      diff --git a/b.go b/b.go
      --- a/b.go
      +++ b/b.go
      @@ -1,1 +1,2 @@
       package b
      +var x = 1
    tests: {pass: true, output: ok}
    explain: {text: It does the thing., truthful: true}
    variants:
      - id: leak
        type: hardcoded_secret
        plant: always
        patch:
          - {find: '+const key = "env"', replace: '+const key = "sk_live_abc"'}
        line: sk_live_abc
        fix: Moved it.
        spotted: A key.
        incident: {title: Leak, detail: It leaked., cost: Money.}
`

func TestLoadMinimal(t *testing.T) {
	s, err := Load("tiny.yaml", []byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	v := s.Turns[0].Variants[0]
	if v.Loc().Hunk != 0 || v.Loc().Line != 1 {
		t.Errorf("flaw loc = %+v", v.Loc())
	}
	if s.Requirements[0].Weight != 1 {
		t.Errorf("default weight = %d", s.Requirements[0].Weight)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := []struct {
		name, from, to, want string
	}{
		{"bad id", "id: tiny", "id: Tiny Scenario", "id: must be lowercase"},
		{"unknown key", "summary: A test scenario.", "summary: A test scenario.\nsumary: typo", "field sumary not found"},
		{"difficulty", "difficulty: 1", "difficulty: 7", "difficulty: must be 1 to 4"},
		{"unknown requirement", "implements: [thing]", "implements: [nothing]", `unknown requirement "nothing"`},
		{"flaw type", "type: hardcoded_secret", "type: vibes", `unknown flaw type "vibes"`},
		{"plant", "plant: always", "plant: sometimes", "plant: must be always"},
		{"patch miss", `find: '+const key = "env"'`, `find: '+const key = "nope"'`, "matched 0 times"},
		{"line miss", "line: sk_live_abc", "line: AKIA", "must match exactly one added line"},
		{"lorem", "brief: Build the thing.", "brief: Lorem ipsum dolor.", "lorem ipsum"},
		{"missing fix", "fix: Moved it.", "fix: ''", ".fix: required"},
		{"one file", "      diff --git a/b.go b/b.go\n      --- a/b.go\n      +++ b/b.go\n      @@ -1,1 +1,2 @@\n       package b\n      +var x = 1\n", "", "touches 1 files"},
		{"no always", "plant: always", "plant: seeded", "no variant has plant: always"},
		{"then unknown", "fix: Moved it.", "fix: Moved it.\n        then: ghost", `unknown variant "ghost"`},
		{"drops wrong type", "fix: Moved it.", "fix: Moved it.\n        drops: [thing]", "only dropped_requirement"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(minimal, c.from) {
				t.Fatalf("fixture does not contain %q", c.from)
			}
			src := strings.Replace(minimal, c.from, c.to, 1)
			_, err := Load("tiny.yaml", []byte(src))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

func TestChangeValidation(t *testing.T) {
	src := minimal + `
changes:
  - id: pivot
    after: missing
    kind: pivot
    message: Change of plan.
    removes: [ghost]
    conflicts: [one]
`
	_, err := Load("tiny.yaml", []byte(src))
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{`after: must name a turn`, `unknown requirement "ghost"`, "rework: required"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestFlawTypes(t *testing.T) {
	for _, ft := range FlawTypes {
		if !ft.Valid() || ft.Label() == string(ft) || ft.LookFor() == "" || ft.DefaultSeverity().Rank() == 0 {
			t.Errorf("%s: incomplete metadata", ft)
		}
	}
	if FlawType("nope").Valid() {
		t.Error("unknown type reported valid")
	}
}
