// Package diff parses unified diffs into files, hunks and numbered lines.
//
// The parser is deliberately lenient about hunk header counts: they are
// recomputed from the lines that follow, so scenario authors only need the
// start line numbers to be right. An empty line inside a hunk is read as an
// empty context line, because editors strip the single leading space.
package diff

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Kind is the role of a line within a hunk.
type Kind uint8

// Line kinds.
const (
	Context Kind = iota
	Added
	Removed
)

// Line is one line of a hunk. Old and New are 1-based line numbers in the
// old and new file; a number is 0 when the line does not exist on that side.
type Line struct {
	Kind Kind
	Text string
	Old  int
	New  int
}

// Hunk is a contiguous block of changes.
type Hunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
	Section            string
	Lines              []Line
}

// Header renders the hunk header with recomputed counts.
func (h *Hunk) Header() string {
	s := fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldCount, h.NewStart, h.NewCount)
	if h.Section != "" {
		s += " " + h.Section
	}
	return s
}

// Stats counts added and removed lines.
func (h *Hunk) Stats() (added, removed int) {
	for _, l := range h.Lines {
		switch l.Kind {
		case Added:
			added++
		case Removed:
			removed++
		}
	}
	return added, removed
}

// File is the set of hunks for one path.
type File struct {
	OldPath, NewPath string
	Created, Deleted bool
	Hunks            []*Hunk
}

// Path is the path a reader cares about: the new path, or the old one for a
// deleted file.
func (f *File) Path() string {
	if f.Deleted || f.NewPath == "" {
		return f.OldPath
	}
	return f.NewPath
}

// Stats counts added and removed lines across the file.
func (f *File) Stats() (added, removed int) {
	for _, h := range f.Hunks {
		a, r := h.Stats()
		added += a
		removed += r
	}
	return added, removed
}

// Diff is a parsed multi-file unified diff.
type Diff struct {
	Files []*File
}

// Stats counts added and removed lines across the diff.
func (d *Diff) Stats() (added, removed int) {
	for _, f := range d.Files {
		a, r := f.Stats()
		added += a
		removed += r
	}
	return added, removed
}

// NumHunks is the number of hunks across all files.
func (d *Diff) NumHunks() int {
	n := 0
	for _, f := range d.Files {
		n += len(f.Hunks)
	}
	return n
}

// Hunk returns the i-th hunk across all files (0-based) and its file.
func (d *Diff) Hunk(i int) (*File, *Hunk) {
	for _, f := range d.Files {
		if i < len(f.Hunks) {
			return f, f.Hunks[i]
		}
		i -= len(f.Hunks)
	}
	return nil, nil
}

// Loc pins one line of a diff.
type Loc struct {
	Hunk int // global hunk index
	Line int // index into Hunk.Lines
}

// FindAdded returns every added line whose text contains substr.
func (d *Diff) FindAdded(substr string) []Loc {
	var out []Loc
	g := 0
	for _, f := range d.Files {
		for _, h := range f.Hunks {
			for i, l := range h.Lines {
				if l.Kind == Added && strings.Contains(l.Text, substr) {
					out = append(out, Loc{Hunk: g, Line: i})
				}
			}
			g++
		}
	}
	return out
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$`)

// Parse reads a unified diff. Every file must have at least one hunk.
func Parse(src string) (*Diff, error) {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	d := &Diff{}
	var f *File
	var h *Hunk

	closeHunk := func() {
		if h == nil {
			return
		}
		for len(h.Lines) > 0 {
			last := h.Lines[len(h.Lines)-1]
			if last.Kind != Context || last.Text != "" {
				break
			}
			h.Lines = h.Lines[:len(h.Lines)-1]
		}
		number(h)
		f.Hunks = append(f.Hunks, h)
		h = nil
	}
	openFile := func() {
		closeHunk()
		f = &File{}
		d.Files = append(d.Files, f)
	}

	for i := 0; i < len(lines); i++ {
		l := lines[i]
		n := i + 1
		fileHeader := strings.HasPrefix(l, "--- ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ ")
		switch {
		case strings.HasPrefix(l, "diff --git "):
			openFile()
			a, b, ok := gitPaths(strings.TrimPrefix(l, "diff --git "))
			if !ok {
				return nil, fmt.Errorf("line %d: malformed diff --git header", n)
			}
			f.OldPath, f.NewPath = a, b
		case fileHeader && (h != nil || f == nil || len(f.Hunks) > 0):
			// A bare ---/+++ pair starts a new file when no git header did.
			openFile()
			fallthrough
		case fileHeader:
			f.OldPath = sidePath(l[4:], "a/")
			f.NewPath = sidePath(lines[i+1][4:], "b/")
			f.Created = f.OldPath == ""
			f.Deleted = f.NewPath == ""
			i++
		case strings.HasPrefix(l, "@@"):
			if f == nil {
				return nil, fmt.Errorf("line %d: hunk before any file header", n)
			}
			m := hunkHeader.FindStringSubmatch(l)
			if m == nil {
				return nil, fmt.Errorf("line %d: malformed hunk header %q", n, l)
			}
			closeHunk()
			h = &Hunk{Section: strings.TrimSpace(m[3])}
			h.OldStart, _ = strconv.Atoi(m[1])
			h.NewStart, _ = strconv.Atoi(m[2])
		case h != nil:
			if l == "" {
				h.Lines = append(h.Lines, Line{Kind: Context})
				continue
			}
			switch l[0] {
			case ' ':
				h.Lines = append(h.Lines, Line{Kind: Context, Text: l[1:]})
			case '+':
				h.Lines = append(h.Lines, Line{Kind: Added, Text: l[1:]})
			case '-':
				h.Lines = append(h.Lines, Line{Kind: Removed, Text: l[1:]})
			case '\\':
				// "\ No newline at end of file"
			default:
				return nil, fmt.Errorf("line %d: unexpected line in hunk: %q", n, l)
			}
		case f != nil:
			switch {
			case strings.HasPrefix(l, "new file mode"):
				f.Created = true
			case strings.HasPrefix(l, "deleted file mode"):
				f.Deleted = true
			}
			// index, mode and similarity lines carry nothing we render.
		case strings.TrimSpace(l) == "":
		default:
			return nil, fmt.Errorf("line %d: expected a diff header, got %q", n, l)
		}
	}
	closeHunk()

	if len(d.Files) == 0 {
		return nil, fmt.Errorf("no files in diff")
	}
	for _, f := range d.Files {
		if len(f.Hunks) == 0 {
			return nil, fmt.Errorf("%s: no hunks", f.Path())
		}
		if f.Path() == "" {
			return nil, fmt.Errorf("file with no path")
		}
	}
	return d, nil
}

// number assigns line numbers and recomputes the header counts.
func number(h *Hunk) {
	o, n := h.OldStart, h.NewStart
	h.OldCount, h.NewCount = 0, 0
	for i := range h.Lines {
		l := &h.Lines[i]
		switch l.Kind {
		case Context:
			l.Old, l.New = o, n
			o++
			n++
			h.OldCount++
			h.NewCount++
		case Added:
			l.New = n
			n++
			h.NewCount++
		case Removed:
			l.Old = o
			o++
			h.OldCount++
		}
	}
}

func gitPaths(s string) (a, b string, ok bool) {
	i := strings.Index(s, " b/")
	if !strings.HasPrefix(s, "a/") || i < 0 {
		return "", "", false
	}
	return s[2:i], s[i+3:], true
}

func sidePath(s, prefix string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		s = s[:i]
	}
	if s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, prefix)
}
