package diff

import (
	"strings"
	"testing"
)

const sample = `diff --git a/src/pay.ts b/src/pay.ts
index 3f2a1c0..9b8e7d2 100644
--- a/src/pay.ts
+++ b/src/pay.ts
@@ -10,4 +10,5 @@ export function charge(amount: number) {
   const cents = Math.round(amount * 100);
-  return stripe.charge(cents);
+  if (cents <= 0) throw new Error("amount must be positive");
+  return stripe.charge(cents, { idempotencyKey });

 }
diff --git a/src/new.ts b/src/new.ts
new file mode 100644
--- /dev/null
+++ b/src/new.ts
@@ -0,0 +1,2 @@
+export const a = 1;
+export const b = 2;
\ No newline at end of file
`

func TestParse(t *testing.T) {
	d, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(d.Files))
	}
	f := d.Files[0]
	if f.Path() != "src/pay.ts" || f.Created {
		t.Errorf("file 0 = %q created=%v", f.Path(), f.Created)
	}
	h := f.Hunks[0]
	if got, want := h.Header(), "@@ -10,4 +10,5 @@ export function charge(amount: number) {"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
	if a, r := h.Stats(); a != 2 || r != 1 {
		t.Errorf("stats = +%d -%d, want +2 -1", a, r)
	}
	// Empty line inside the hunk is a context line on both sides.
	blank := h.Lines[4]
	if blank.Kind != Context || blank.Old != 12 || blank.New != 13 {
		t.Errorf("blank line = %+v", blank)
	}
	added := h.Lines[3]
	if added.Kind != Added || added.New != 12 || added.Old != 0 {
		t.Errorf("added line = %+v", added)
	}
	if !d.Files[1].Created || d.Files[1].Path() != "src/new.ts" {
		t.Errorf("file 1 = %+v", d.Files[1])
	}
	if d.NumHunks() != 2 {
		t.Errorf("hunks = %d", d.NumHunks())
	}
	if a, r := d.Stats(); a != 4 || r != 1 {
		t.Errorf("diff stats = +%d -%d", a, r)
	}
	_, h1 := d.Hunk(1)
	if h1 == nil || h1.NewStart != 1 {
		t.Errorf("Hunk(1) = %+v", h1)
	}
	if f, h := d.Hunk(2); f != nil || h != nil {
		t.Error("Hunk(2) should be out of range")
	}
}

func TestFindAdded(t *testing.T) {
	d, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	locs := d.FindAdded("idempotencyKey")
	if len(locs) != 1 || locs[0] != (Loc{Hunk: 0, Line: 3}) {
		t.Fatalf("locs = %+v", locs)
	}
	if len(d.FindAdded("stripe.charge(cents);")) != 0 {
		t.Error("removed lines must not match")
	}
}

func TestParseBareHeaders(t *testing.T) {
	src := strings.Join([]string{
		"--- a/one.sql",
		"+++ b/one.sql",
		"@@ -1 +1 @@",
		"--- old comment",
		"+-- new comment",
		"--- a/two.sql",
		"+++ b/two.sql",
		"@@ -3,1 +3,1 @@",
		"-x",
		"+y",
	}, "\n")
	d, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(d.Files))
	}
	if got := d.Files[0].Hunks[0].Lines[0]; got.Kind != Removed || got.Text != "-- old comment" {
		t.Errorf("sql comment line = %+v", got)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"empty":      "",
		"no hunks":   "diff --git a/x b/x\n--- a/x\n+++ b/x\n",
		"bad header": "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ nonsense @@\n",
		"orphan":     "@@ -1 +1 @@\n+x\n",
		"junk":       "hello\n",
		"bad line":   "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n*x\n",
		"bad git":    "diff --git x y\n",
	}
	for name, src := range cases {
		if _, err := Parse(src); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
