# igtm

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Made with Go](https://img.shields.io/badge/Made%20with-Go-00ADD8.svg?logo=go&logoColor=white)](https://go.dev/)
[![GitHub stars](https://img.shields.io/github/stars/butaraul/igtm.svg?style=social)](https://github.com/butaraul/igtm/stargazers)

A terminal game about supervising an AI coding agent.

You take a contract from a client with a vague brief. A simulated agent
submits the work one diff at a time. You accept it, reject it, or read it
first. Reading costs time. Tests cost time and tokens, and passing tests are
not proof. The agent will explain itself if asked, and is sometimes wrong
with total confidence. Over a long session its context fills up and it starts
forgetting what it was told. Then you ship, and production decides.

The skill it trains is trust calibration: knowing which changes you can wave
through and which ones you need to read line by line.

The agent is not an LLM. It is a deterministic state machine driven by
scenario data, so a seed and a sequence of inputs always produce the same
run. Nothing leaves your machine: no network, no telemetry, no accounts.

## Install

```sh
go install github.com/butaraul/lgtm/cmd/lgtm@latest
```

Requires Go 1.24 or later. Release binaries for Linux, macOS and Windows are
built with GoReleaser from `.goreleaser.yaml`: tag a version and run
`goreleaser release`.

## Play

```sh
lgtm                       # menu
lgtm --scenario booking    # straight into a contract
lgtm --scenario booking --seed 1234 --difficulty hard
lgtm --daily               # today's challenge: same run for everyone on this UTC date
lgtm --list                # scenarios
lgtm validate [file.yaml]  # check the embedded scenarios, plus any files given
lgtm --reset               # delete progress and the saved run (settings are kept)
lgtm --no-color            # or set NO_COLOR
```

The terminal must be at least 80x24.

| Key | Action |
| --- | --- |
| `j` `k`, arrows | move; scroll the log |
| `n` `p` | next or previous hunk |
| `enter`, `space` | open or close a hunk; on a file header, every hunk in it |
| `tab` | switch the left pane between the diff and the log |
| `a` | accept |
| `r` | reject, with a reason (`1`-`9`) and an optional note |
| `t` | run the tests |
| `e` | ask the agent to explain |
| `s` | summarise and restart the agent's context |
| `D` | ship now with what has been accepted |
| `?` | help |
| `q` | quit; a run in progress is saved and resumes from the menu |

### The rules

- Hunks start closed. `○` is unread, `●` is read. Opening an unread hunk costs
  clock time in proportion to its size. You may accept without reading.
- Rejecting asks why. A reason that names the actual flaw gets it fixed,
  though a fix can carry a flaw of its own. A wrong reason gets it fixed 30% of
  the time, "something else" 50%; otherwise the agent resubmits unchanged.
  Rejecting clean work gets the same work back and costs the time anyway. On
  the third rejection of the same work, the agent drops it.
- Every agent call re-reads its context, so long sessions cost more tokens
  per step. Context health falls as the session grows. Below a threshold,
  usually 40%, some changes are written by an agent that has forgotten an
  earlier constraint, and old bugs come back. A full context compacts itself,
  lossily. Summarise-and-restart brings health back to 88% for 15 minutes and
  some tokens.
- The client changes their mind mid-build. Some changes conflict with work
  you already accepted, which then needs rework.
- The clock is a working day (09:00 to 17:00). At the deadline the client
  ships whatever was accepted. With no tokens left the agent stops.
- Each accepted flaw becomes a production incident with a severity and a
  delay that depend on its type. A clean run ends with "Nothing happened."

### Scoring

Outcome is scored, not speed.

| Part | Weight | Meaning |
| --- | --- | --- |
| Trust calibration | 40% | Balanced accuracy over the 2x2 of accepted/rejected × clean/flawed: the mean of the share of flaws you rejected and the share of clean work you accepted. Accepting everything scores 50. So does rejecting everything. |
| Production | 35% | 100 minus a penalty per incident: low 10, medium 25, high 45, critical 70. |
| Client satisfaction | 25% | Weighted share of requirements actually delivered, minus a smaller penalty per incident. Zero if you hold instead of shipping. |

Grades: A 90+, B 80+, C 70+, D 60+, F below. The scorecard also reports
review coverage (share of hunks read), diagnosis (catches where you named the
flaw correctly), and budget left. The post-mortem replays the run: for every
flaw you accepted it shows the file, hunk and line, whether you opened it,
whether tests or an explanation misled you, and what it cost in production.
For every flaw you caught, it shows what you spotted.

Difficulty scales the budget (easy ×1.3, hard ×0.85) and the chance of
seeded flaws (easy ×0.6, hard ×1.35).

## Scenarios

| id | Difficulty | Contract |
| --- | --- | --- |
| `landing` | 1 | Landing page with a contact form |
| `expenses` | 2 | Expense tracker with CSV import |
| `booking` | 3 | Booking system with payments |
| `admin` | 4 | Admin dashboard with user deletion |

### Adding a scenario

A scenario is one YAML file in `internal/scenario/data/`. It is embedded at
build time and checked by `lgtm validate` and by the test suite. Unknown keys
are errors.

```yaml
id: kebab-case-id
title: Shown in the menu
difficulty: 1            # 1 to 4
summary: One line for --list.

client:
  name: Client Name
  role: Their role and company
  about: One or two lines of persona.

brief: |
  The vague brief, in the client's voice.

requirements:            # what the brief means, shown to the player
  - id: pay
    text: Pay by card at booking
    weight: 2            # default 1; weights satisfaction

budget:
  clock: 440             # working minutes (8 per hour, 480 per day)
  tokens: 340000         # agent spend
  context: 26000         # context window; smaller means earlier decay

turns:                   # units of work, in order
  - id: checkout
    title: Stripe Checkout for a single class
    implements: [pay]    # a requirement is delivered when every turn implementing it is accepted
    message: |
      What the agent says it did.
    diff: |              # unified diff, 2 to 4 files; hunk counts are recomputed
      diff --git a/src/lib/stripe.ts b/src/lib/stripe.ts
      ...
    tests:
      pass: true
      output: |
        What `run tests` prints.
    explain:
      truthful: true
      text: What the agent says when asked to explain.
    variants:            # flawed versions of this turn
      - id: inline-key
        type: hardcoded_secret      # see flaw types below
        plant: always               # always | seeded | decay | revision
        chance: 0.5                 # seeded only; default 0.4
        below: 0.4                  # decay only: context health threshold
        patch:                      # edits to the clean diff; each find must match exactly once
          - find: '+const key = process.env.STRIPE_SECRET_KEY;'
            replace: '+const key = "sk_live_...";'
        line: 'sk_live_'            # substring of exactly one added line: where the flaw is
        message: optional override of the agent's message
        tests: optional override {pass, output}; by default the clean tests
        explain: optional override; by default the clean text, marked untruthful
        fix: The agent's message when it resubmits after a correct rejection.
        then: other-variant-id      # optional: the fix is this plant: revision variant
        drops: [pay]                # dropped_requirement only
        spotted: What a player who caught it saw.
        incident:
          title: Headline in production
          detail: What happened.
          cost: What it cost.
          severity: critical        # optional; default by type
          day: 2                    # optional; days after ship, default by type

changes:                 # client messages mid-build
  - id: pivot
    after: checkout      # fires when this turn is accepted or dropped
    kind: pivot          # or scope_creep
    message: |
      The client's message.
    adds: [{id: packs, text: Buy a 10-class pack}]
    removes: [some-requirement]
    turns: [...]         # new work, queued next
    cancels: [turn-id]   # queued work no longer needed
    conflicts: [turn-id] # if any of these were accepted...
    rework: {...}        # ...this turn is queued first
```

Flaw types: `hardcoded_secret`, `unhandled_edge_case`, `dropped_requirement`,
`off_by_one` (including timezones), `insecure_default`, `empty_test`,
`hallucinated_api`, `destructive_change`.

Planting: at most one `always` variant per turn, and every scenario needs at
least one. When a turn comes up, a triggered `decay` variant wins, then
`always`, then a `seeded` variant, which is picked by a hash of the seed and
the turn id. A seeded variant's chance rises as context health drops below
50%.

Write diffs the way an agent would: plausible, idiomatic, mostly right. The
flaw should be discoverable by reading the diff and not by being cartoonish.
Leading spaces of context lines can be dropped from blank lines, since
editors strip them. For `find` strings that must start or end mid-line, use
YAML's `|-` block style to drop the trailing newline.

## Development

```sh
go test ./...                      # engine table tests, golden files, UI drive test
go test ./internal/tui -update     # rewrite golden files after an intended change
go vet ./... && staticcheck ./...
```

Layout:

- `internal/engine`: the game. Pure and deterministic, no I/O.
- `internal/scenario`: schema, loader, validator, embedded YAML.
- `internal/diff`: unified diff parser.
- `internal/tui`: Bubble Tea models and pure renderers. The engine never imports it.
- `internal/store`: settings, progress and the saved run as JSON in the XDG
  config and state directories (`$XDG_CONFIG_HOME/lgtm`, `$XDG_STATE_HOME/lgtm`;
  defaults `~/.config` and `~/.local/state`, or `%AppData%` and `%LocalAppData%` on Windows).
- `cmd/lgtm`: flags and wiring.

`internal/engine/budget_test.go` plays every scenario with a careful strategy
and with a rubber stamp, and fails if careful play cannot earn an A or the
rubber stamp earns more than a C. Run it after changing budgets or costs.

### Decisions not covered by the brief

- **Hunks start collapsed**, and opening one is the "inspect" action. Review
  coverage is measured, not self-reported.
- **A rejection reason is one of the eight flaw types or "something else"**,
  plus an optional free-text note. The category drives what the agent does;
  the note appears in the post-mortem.
- **Flaws are patches on a clean diff**, so each flawed version differs from
  the clean one by one plausible edit, and the post-mortem can point at the
  exact line.
- **A saved run is its seed plus its action log.** Resume replays it through
  the engine, which is deterministic.
- **The daily challenge uses the UTC date** and always runs on normal
  difficulty. It picks the scenario and the seed.
- **Removed lines are shown dimmed**, without syntax colour. Added lines get
  the accent and a faint background.
- **The terminal UI uses Bubble Tea v1**, the stable line with the widest
  terminal support at the time of writing.

## License

MIT. See [LICENSE](LICENSE).
