package tui

import (
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/butaraul/lgtm/internal/diff"
)

const tabWidth = 4

type rowKind uint8

const (
	rowBlank rowKind = iota
	rowFile
	rowHunk
	rowLine
)

// row is one rendered line of the diff pane, without the cursor column.
type row struct {
	kind rowKind
	file int // file index
	hunk int // global hunk index, -1 for file and blank rows
	text string
}

// seg is a run of text with one style.
type seg struct {
	text  string
	style lipgloss.Style
}

// highlight splits a hunk into per-line styled segments using the lexer for
// the file's extension. Lines are lexed together so multi-line strings and
// comments come out right.
func highlight(st *styles, path string, h *diff.Hunk) [][]seg {
	lines := make([]string, len(h.Lines))
	for i, l := range h.Lines {
		lines[i] = strings.ReplaceAll(l.Text, "\t", strings.Repeat(" ", tabWidth))
	}
	out := make([][]seg, len(lines))
	lexer := lexers.Match(path)
	if lexer == nil {
		for i, l := range lines {
			out[i] = []seg{{l, st.base}}
		}
		return out
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, strings.Join(lines, "\n"))
	if err != nil {
		for i, l := range lines {
			out[i] = []seg{{l, st.base}}
		}
		return out
	}
	i := 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		parts := strings.Split(tok.Value, "\n")
		for j, p := range parts {
			if j > 0 {
				i++
			}
			if p != "" && i < len(out) {
				out[i] = append(out[i], seg{p, tokenStyle(st, tok.Type)})
			}
		}
	}
	return out
}

func tokenStyle(st *styles, t chroma.TokenType) lipgloss.Style {
	switch {
	case t.InCategory(chroma.Comment):
		return st.comment
	case t.InSubCategory(chroma.LiteralString):
		return st.str
	case t.InSubCategory(chroma.LiteralNumber):
		return st.num
	case t.InCategory(chroma.Keyword), t == chroma.NameTag, t == chroma.NameBuiltin:
		return st.kw
	case t == chroma.NameFunction, t == chroma.NameClass:
		return st.fn
	case t.InCategory(chroma.Operator), t.InCategory(chroma.Punctuation):
		return st.op
	}
	return st.base
}

// diffRows renders the diff for a pane of the given width (excluding the
// cursor column). open says which hunks are expanded, read which have been
// inspected. hl caches highlighting per hunk and may be nil.
func diffRows(st *styles, d *diff.Diff, open, read []bool, width int, hl map[int][][]seg) []row {
	var rows []row
	g := 0
	for fi, f := range d.Files {
		if fi > 0 {
			rows = append(rows, row{kind: rowBlank, file: fi, hunk: -1})
		}
		a, r := f.Stats()
		path := f.Path()
		tag := ""
		switch {
		case f.Created:
			tag = " new"
		case f.Deleted:
			tag = " deleted"
		}
		stats := fmt.Sprintf("+%d -%d", a, r)
		room := width - runewidth.StringWidth(stats) - runewidth.StringWidth(tag) - 2
		path = truncLeft(path, room)
		gap := width - 1 - runewidth.StringWidth(path) - runewidth.StringWidth(tag) - runewidth.StringWidth(stats)
		rows = append(rows, row{kind: rowFile, file: fi, hunk: -1, text: " " + st.file.Render(path) + st.dim.Render(tag) +
			strings.Repeat(" ", max(1, gap)) + statStr(st, a, r)})

		for _, h := range f.Hunks {
			rows = append(rows, hunkRow(st, fi, g, h, open[g], read[g], width))
			if open[g] {
				segs := hl[g]
				if segs == nil {
					segs = highlight(st, path, h)
					if hl != nil {
						hl[g] = segs
					}
				}
				for i, l := range h.Lines {
					rows = append(rows, lineRows(st, fi, g, l, segs[i], width)...)
				}
			}
			g++
		}
	}
	return rows
}

func statStr(st *styles, a, r int) string {
	return st.accent.Render(fmt.Sprintf("+%d", a)) + " " + st.danger.Render(fmt.Sprintf("-%d", r))
}

func hunkRow(st *styles, fi, g int, h *diff.Hunk, open, read bool, width int) row {
	fold := "▸"
	if open {
		fold = "▾"
	}
	mark, style := "○", st.hunk
	if read {
		mark, style = "●", st.hunkRead
	}
	a, r := h.Stats()
	stats := fmt.Sprintf("+%d -%d", a, r)
	label := h.Header()
	room := width - 7 - runewidth.StringWidth(stats)
	label = truncRight(label, room)
	gap := width - 6 - runewidth.StringWidth(label) - runewidth.StringWidth(stats)
	markStyle := st.accent
	if read {
		markStyle = st.dim
	}
	text := "  " + st.dim.Render(fold) + " " + markStyle.Render(mark) + " " + style.Render(label) +
		strings.Repeat(" ", max(1, gap)) + st.dim.Render(stats)
	return row{kind: rowHunk, file: fi, hunk: g, text: text}
}

// lineRows renders one diff line, soft-wrapped to width.
func lineRows(st *styles, fi, g int, l diff.Line, segs []seg, width int) []row {
	num := l.New
	markText, markStyle, bg := " ", st.gutter, st.ctxLine
	switch l.Kind {
	case diff.Added:
		markText, markStyle, bg = "+", st.addMark, st.addLine
	case diff.Removed:
		num = l.Old
		markText, markStyle, bg = "-", st.delMark, st.delLine
	}
	gutter := fmt.Sprintf("  %4d ", num)
	codeW := max(8, width-len(gutter)-2)
	if l.Kind == diff.Removed {
		// Removed lines are read for what went, not how it was spelled.
		plain := ""
		for _, s := range segs {
			plain += s.text
		}
		segs = []seg{{plain, st.dim}}
	}
	wrapped := wrapSegs(segs, codeW)
	rows := make([]row, len(wrapped))
	for i, w := range wrapped {
		var b strings.Builder
		if i == 0 {
			b.WriteString(st.gutter.Render(gutter))
			b.WriteString(markStyle.Render(markText + " "))
		} else {
			b.WriteString(strings.Repeat(" ", len(gutter)))
			b.WriteString(bg.Render("  "))
		}
		used := 0
		for _, s := range w {
			b.WriteString(s.style.Inherit(bg).Render(s.text))
			used += runewidth.StringWidth(s.text)
		}
		if pad := codeW - used; pad > 0 {
			b.WriteString(bg.Render(strings.Repeat(" ", pad)))
		}
		rows[i] = row{kind: rowLine, file: fi, hunk: g, text: b.String()}
	}
	return rows
}

// wrapSegs breaks styled segments into rows no wider than w.
func wrapSegs(segs []seg, w int) [][]seg {
	var out [][]seg
	var cur []seg
	used := 0
	for _, s := range segs {
		var buf strings.Builder
		for _, r := range s.text {
			rw := runewidth.RuneWidth(r)
			if used+rw > w {
				if buf.Len() > 0 {
					cur = append(cur, seg{buf.String(), s.style})
					buf.Reset()
				}
				out = append(out, cur)
				cur, used = nil, 0
			}
			buf.WriteRune(r)
			used += rw
		}
		if buf.Len() > 0 {
			cur = append(cur, seg{buf.String(), s.style})
		}
	}
	return append(out, cur)
}

func truncRight(s string, w int) string {
	if w <= 1 {
		return ""
	}
	if runewidth.StringWidth(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "…")
}

func truncLeft(s string, w int) string {
	if w <= 1 {
		return ""
	}
	if runewidth.StringWidth(s) <= w {
		return s
	}
	rs := []rune(s)
	for len(rs) > 0 && runewidth.StringWidth(string(rs))+1 > w {
		rs = rs[1:]
	}
	return "…" + string(rs)
}

// viewRows draws visible rows with the cursor column, padded to height.
func viewRows(st *styles, rows []row, cursor, offset, height, revealed int) string {
	var b strings.Builder
	for i := 0; i < height; i++ {
		ri := offset + i
		if i > 0 {
			b.WriteByte('\n')
		}
		if ri >= len(rows) || ri >= revealed {
			continue
		}
		if ri == cursor {
			b.WriteString(st.cursor.Render("▌"))
		} else {
			b.WriteByte(' ')
		}
		b.WriteString(rows[ri].text)
	}
	return b.String()
}
