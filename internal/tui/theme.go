package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	bg            = "#131524"
	panel         = "#1c1e33"
	purple        = "#b296ff"
	text          = "#dedfed"
	muted         = "#9a9db5"
	border        = "#383a55"
	dim           = "#55586f"
	warning       = "#e6bb70"
	selectedPanel = "#302843"
)

func themed(s, foreground, background string, dimmed bool) string {
	if dimmed {
		foreground = dim
		if background == purple || background == selectedPanel {
			background = panel
		}
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(foreground)).Background(lipgloss.Color(background))
	// Nested spans reset all SGR attributes. Restore the enclosing surface after
	// each span so padding and plain text keep their card/modal background.
	probe := style.Render("x")
	open, _, _ := strings.Cut(probe, "x")
	if open != "" {
		s = strings.NewReplacer("\x1b[0m", "\x1b[0m"+open, "\x1b[m", "\x1b[m"+open).Replace(s)
	}
	return style.Render(s)
}

// cellCrop and wrapText operate on display cells rather than bytes or runes.
// Hard wrapping also keeps unbroken URLs and CJK text inside their container.
func cellCrop(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}
func cellPad(s string, w int) string {
	s = cellCrop(s, w)
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}
func wrapText(s string, w int) string {
	if w <= 0 {
		return ""
	}
	lines := strings.Split(ansi.Wrap(s, w, "/"), "\n")
	for i := range lines {
		lines[i] = cellCrop(lines[i], w)
	}
	return strings.Join(lines, "\n")
}

// overlay places an ANSI-styled box on an existing terminal-sized canvas.
// Negative coordinates and partial wide characters are clipped by cell width.
// width/height describe the resulting canvas, not the size of the box.
func overlay(base, box string, x, y, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := strings.Split(base, "\n")
	result := make([]string, height)
	for i := range result {
		if i < len(rows) {
			result[i] = cellPad(rows[i], width)
		} else {
			result[i] = strings.Repeat(" ", width)
		}
	}
	putRows(result, x, y, box, width)
	return strings.Join(result, "\n")
}

func putRows(rows []string, x, y int, s string, width int) {
	for i, line := range strings.Split(s, "\n") {
		row := y + i
		if row < 0 || row >= len(rows) || x >= width {
			continue
		}
		start := max(0, x)
		lineWidth := ansi.StringWidth(line)
		end := min(width, x+lineWidth)
		if end <= start {
			continue
		}
		fragment := ansi.Cut(line, start-x, end-x)
		// Cut may omit a grapheme bisected at a boundary. Padding retains cell
		// alignment so an overlay cannot pull the old right-hand content left.
		fragment = cellPad(fragment, end-start)
		left := cellPad(ansi.Cut(rows[row], 0, start), start)
		right := cellPad(ansi.Cut(rows[row], end, width), width-end)
		rows[row] = left + fragment + right
	}
}
