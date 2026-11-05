package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	// Mariana is the sole built-in theme. Source expressions and the small
	// readability adaptations are recorded in docs/theme-mariana.md.
	themeID                 = "mariana"
	themeName               = "Mariana"
	bg                      = "#303841"
	panel                   = "#2b323a"
	raised                  = "#3a424a"
	sidebarBackground       = "#1d2227"
	inputBackground         = "#282b2f"
	text                    = "#d8dee9"
	muted                   = "#a9afbc"
	disabled                = "#7b8896"
	backdropText            = disabled
	border                  = "#647382"
	divider                 = "#444c54"
	accent                  = "#85b3e0"
	focus                   = "#f9ae58"
	selectionBackground     = "#4c5864"
	selectionForeground     = text
	hoverBackground         = "#3c444c"
	primaryButtonBackground = "#5c99d6"
	primaryButtonForeground = sidebarBackground
	success                 = "#99c794"
	warning                 = focus
	errorColor              = "#f28d92"
	info                    = accent
)

func themed(s, foreground, background string, dimmed bool) string {
	if dimmed {
		// A real, opaque backdrop: flatten nested components before rendering
		// the active modal. No terminal transparency or faint attribute needed.
		s = ansi.Strip(s)
		foreground, background = backdropText, sidebarBackground
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

// styleInput replaces Bubbles' independent default palette, including its
// placeholder, suggestion and cursor styles. Input geometry stays unchanged.
func styleInput(input *textinput.Model, background string, invalid bool) {
	style := func(fg string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(background))
	}
	s := textinput.Styles{}
	s.Blurred = textinput.StyleState{Text: style(text), Placeholder: style(muted), Suggestion: style(muted), Prompt: style(muted)}
	s.Focused = s.Blurred
	s.Focused.Prompt = style(focus)
	if invalid {
		s.Focused.Prompt, s.Blurred.Prompt = style(errorColor), style(errorColor)
	}
	s.Cursor = textinput.CursorStyle{Color: lipgloss.Color(focus), Blink: true}
	input.SetStyles(s)
}

func inputFrame(lines []string, width int, edge, surface string) []string {
	if width < 4 {
		var out []string
		for _, line := range lines {
			out = append(out, cellCrop(line, width))
		}
		return out
	}
	rows := []string{themed("╭"+strings.Repeat("─", width-2)+"╮", edge, surface, false)}
	for _, line := range lines {
		rows = append(rows, themed("│ "+cellPad(line, width-4)+" │", edge, surface, false))
	}
	return append(rows, themed("╰"+strings.Repeat("─", width-2)+"╯", edge, surface, false))
}

func helpText(s, background string, dimmed bool) string {
	// Keep key names brighter than explanations without changing cell geometry.
	parts := strings.Fields(s)
	keys := map[string]bool{"Tab": true, "Enter": true, "Esc": true, "Ctrl+C": true, "↑↓": true, "?": true, "q": true}
	for i, word := range parts {
		color := muted
		// Translations can attach punctuation directly to a key name.
		if keys[strings.TrimRight(word, ":：")] {
			color = text
		}
		parts[i] = themed(word, color, background, dimmed)
	}
	return strings.Join(parts, themed(" ", muted, background, dimmed))
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
