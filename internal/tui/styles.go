// Package tui is the Bubble Tea front end. It renders engine state and turns
// keys into engine actions; it holds no game rules of its own.
package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Palette: one warm accent, one danger colour, everything else neutral.
func cc(dark, light [3]string) lipgloss.CompleteAdaptiveColor {
	return lipgloss.CompleteAdaptiveColor{
		Dark:  lipgloss.CompleteColor{TrueColor: dark[0], ANSI256: dark[1], ANSI: dark[2]},
		Light: lipgloss.CompleteColor{TrueColor: light[0], ANSI256: light[1], ANSI: light[2]},
	}
}

var (
	colAccent = cc([3]string{"#E0A458", "179", "3"}, [3]string{"#A8641A", "130", "3"})
	colDanger = cc([3]string{"#E5534B", "167", "1"}, [3]string{"#C0392B", "124", "1"})
	colFg     = cc([3]string{"#D0D0D0", "252", "7"}, [3]string{"#262626", "235", "0"})
	colBright = cc([3]string{"#F2F2F2", "255", "15"}, [3]string{"#000000", "16", "0"})
	colDim    = cc([3]string{"#8A8A8A", "245", "8"}, [3]string{"#707070", "242", "8"})
	colFaint  = cc([3]string{"#4A4A4A", "238", "8"}, [3]string{"#C6C6C6", "251", "7"})
	colString = cc([3]string{"#C9AE85", "180", "7"}, [3]string{"#7A5A2A", "94", "0"})
	colAddBg  = cc([3]string{"#26221A", "235", ""}, [3]string{"#FBF4E6", "230", ""})
	colDelBg  = cc([3]string{"#2A1C1C", "234", ""}, [3]string{"#FBECEA", "224", ""})
)

// styles is every style the UI uses, bound to one renderer so tests can
// render without colour.
type styles struct {
	r *lipgloss.Renderer

	base, bright, dim, faint, bold lipgloss.Style
	accent, danger                 lipgloss.Style
	title, section, key            lipgloss.Style
	rule                           lipgloss.Style

	// diff
	file, hunk, hunkRead, gutter  lipgloss.Style
	addMark, delMark              lipgloss.Style
	addLine, delLine, ctxLine     lipgloss.Style
	kw, str, comment, num, fn, op lipgloss.Style
	cursor                        lipgloss.Style
}

func newStyles(r *lipgloss.Renderer) *styles {
	s := &styles{r: r}
	n := r.NewStyle
	s.base = n().Foreground(colFg)
	s.bright = n().Foreground(colBright)
	s.dim = n().Foreground(colDim)
	s.faint = n().Foreground(colFaint)
	s.bold = n().Foreground(colBright).Bold(true)
	s.accent = n().Foreground(colAccent)
	s.danger = n().Foreground(colDanger)
	s.title = n().Foreground(colAccent).Bold(true)
	s.section = n().Foreground(colDim).Bold(true)
	s.key = n().Foreground(colAccent)
	s.rule = n().Foreground(colFaint)

	s.file = n().Foreground(colBright).Bold(true)
	s.hunk = n().Foreground(colFg)
	s.hunkRead = n().Foreground(colDim)
	s.gutter = n().Foreground(colFaint)
	s.addMark = n().Foreground(colAccent).Background(colAddBg)
	s.delMark = n().Foreground(colDanger).Background(colDelBg)
	s.addLine = n().Background(colAddBg)
	s.delLine = n().Background(colDelBg)
	s.ctxLine = n()
	s.kw = n().Foreground(colBright).Bold(true)
	s.str = n().Foreground(colString)
	s.comment = n().Foreground(colDim).Italic(true)
	s.num = n().Foreground(colString)
	s.fn = n().Foreground(colBright)
	s.op = n().Foreground(colDim)
	s.cursor = n().Foreground(colAccent)
	return s
}
