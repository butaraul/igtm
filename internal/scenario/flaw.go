package scenario

// FlawType is a category of planted flaw. It doubles as the reason a player
// gives when rejecting a candidate.
type FlawType string

// Flaw types.
const (
	HardcodedSecret    FlawType = "hardcoded_secret"
	UnhandledEdgeCase  FlawType = "unhandled_edge_case"
	DroppedRequirement FlawType = "dropped_requirement"
	OffByOne           FlawType = "off_by_one"
	InsecureDefault    FlawType = "insecure_default"
	EmptyTest          FlawType = "empty_test"
	Hallucinated       FlawType = "hallucinated_api"
	Destructive        FlawType = "destructive_change"
)

// FlawTypes lists every flaw type in the order the reject picker shows them.
var FlawTypes = []FlawType{
	HardcodedSecret,
	UnhandledEdgeCase,
	DroppedRequirement,
	OffByOne,
	InsecureDefault,
	EmptyTest,
	Hallucinated,
	Destructive,
}

type flawInfo struct {
	label    string
	severity Severity
	day      int
	lookFor  string
}

var flawTable = map[FlawType]flawInfo{
	HardcodedSecret: {
		label:    "Hardcoded secret",
		severity: Critical,
		day:      2,
		lookFor:  "String literals next to words like key, secret, token or password. Prefixes such as sk_live_, AKIA and ghp_ are real credentials. Anything bundled for the browser is public, whatever the variable is called.",
	},
	UnhandledEdgeCase: {
		label:    "Unhandled edge case",
		severity: Medium,
		day:      1,
		lookFor:  "Ask what happens with empty input, zero, a duplicate, a missing record, or the same request twice. Code that only describes the happy path has only been tested on it.",
	},
	DroppedRequirement: {
		label:    "Silently dropped requirement",
		severity: Medium,
		day:      4,
		lookFor:  "Check the diff against the brief, not against the agent's summary. A summary lists what was built. Look for what was asked for and is absent, and for errors swallowed with a bare continue or pass.",
	},
	OffByOne: {
		label:    "Off-by-one or timezone",
		severity: High,
		day:      6,
		lookFor:  "Every < versus <=, every slice bound, every date without a zone. Dates parsed without an explicit timezone use the server's, and the server is in UTC.",
	},
	InsecureDefault: {
		label:    "Insecure default",
		severity: High,
		day:      9,
		lookFor:  "Defaults that open things up: CORS set to *, auth checks behind a flag, debug on, verification skipped, permissions granted before they are checked. Secure behaviour should be what happens when nobody configures anything.",
	},
	EmptyTest: {
		label:    "Test that asserts nothing",
		severity: Low,
		day:      12,
		lookFor:  "A test that calls the code and never compares the result, asserts on a mock it just configured, or catches the error it should fail on. A passing suite only means something if a broken change would fail it.",
	},
	Hallucinated: {
		label:    "Hallucinated package or API",
		severity: High,
		day:      0,
		lookFor:  "New dependencies and unfamiliar method names. Check that the package exists under that exact name, and that the method exists in the version pinned. A test that mocks the module cannot tell you either.",
	},
	Destructive: {
		label:    "Unrequested destructive change",
		severity: Critical,
		day:      0,
		lookFor:  "DROP, DELETE, TRUNCATE, cascades, force flags and removed files that nobody asked for. Migrations run once, in production, against data that has no second copy.",
	},
}

// Valid reports whether t is a known flaw type.
func (t FlawType) Valid() bool {
	_, ok := flawTable[t]
	return ok
}

// Label is the human name of the flaw type.
func (t FlawType) Label() string {
	if i, ok := flawTable[t]; ok {
		return i.label
	}
	return string(t)
}

// LookFor is the post-mortem note on how to catch this flaw type.
func (t FlawType) LookFor() string { return flawTable[t].lookFor }

// DefaultSeverity is the incident severity when a scenario does not set one.
func (t FlawType) DefaultSeverity() Severity { return flawTable[t].severity }

// DefaultDay is the day after ship the incident surfaces when a scenario
// does not set one.
func (t FlawType) DefaultDay() int { return flawTable[t].day }
