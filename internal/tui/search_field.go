package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Two cells of horizontal padding on either side, Bubbles' cursor cell, and
// a stable clear-action slot. Very small fields give that slot back to editing.
func searchClearWidth(width int) int {
	if width >= 20 {
		return 7
	}
	return 0
}

func searchInputWidth(width int) int { return max(1, width-5-searchClearWidth(width)) }

// Bubbles does not recalculate a scrolled viewport when a new width still
// contains the logical cursor. Rebuild it on resize, then restore the position.
func resizeSearchInput(input *textinput.Model, width int) {
	position := input.Position()
	if input.Width() != width {
		input.SetWidth(width)
		input.CursorEnd()
	}
	input.SetCursor(position)
}

func searchClearID(id string) string {
	switch id {
	case "proxies.search":
		return "proxies.clear"
	case "conn.search":
		return "conn.clear"
	case "rules.search":
		return "rules.clear"
	case "logs.search":
		return "logs.reset-search"
	case "rules.providers-search":
		return "rules.providers-clear"
	}
	return ""
}

func prepareSearchInput(input textinput.Model, width int, surface string, invalid bool) textinput.Model {
	input.Prompt = ""
	resizeSearchInput(&input, searchInputWidth(width))
	styleInput(&input, surface, invalid)
	input.SetVirtualCursor(false)
	styles := input.Styles()
	styles.Cursor.Shape = tea.CursorBar
	input.SetStyles(styles)
	return input
}

func (m Model) searchSurface(id string, input textinput.Model) string {
	if m.focus == id || m.focus == searchClearID(id) || input.Focused() {
		return raised
	}
	if m.hover == id || m.hover == searchClearID(id) {
		return panel
	}
	return inputBackground
}

func (m Model) searchField(id, placeholder string, input textinput.Model, width int, invalid bool) dashboardBlock {
	width = max(1, width)
	surface := m.searchSurface(id, input)
	input.Placeholder = placeholder
	input = prepareSearchInput(input, width, surface, invalid)
	// Bubbles' placeholder padding may contain NULs with wide characters.
	value := cellPad(strings.ReplaceAll(input.View(), "\x00", ""), searchInputWidth(width)+1)
	clearID, clearWidth := searchClearID(id), searchClearWidth(width)
	canClear := clearID != "" && clearWidth > 0 && input.Value() != "" && !m.busy && !m.quitting
	clear := strings.Repeat(" ", clearWidth)
	if canClear {
		marker, color := " ", muted
		if m.focus == clearID {
			marker, color = "›", focus
		} else if m.hover == clearID {
			color = text
		}
		clear = themed(marker+cellPad(m.tr("search.clear"), 5)+" ", color, surface, false)
	}
	content := value + clear
	blank := themed(strings.Repeat(" ", width), text, surface, false)
	b := dashboardBlock{lines: []string{blank, themed(cellPad("  "+content, width), text, surface, false), blank}}
	if m.buttonPlain() {
		edge := divider
		if m.focus == id || input.Focused() {
			edge = focus
		}
		if invalid {
			edge = errorColor
		}
		b.lines = inputFrame([]string{content}, width, edge, surface)
	}
	if !m.busy && !m.quitting {
		if canClear {
			b.targets = append(b.targets, hitRegion{ID: clearID, X: width - 2 - clearWidth, Y: 1, W: clearWidth, H: 1})
		}
		b.targets = append(b.targets, hitRegion{ID: id, W: width, H: len(b.lines)})
	}
	return b
}

// Controls sit on the text baseline, or below the field on narrow terminals.
func (m Model) searchToolbar(id, placeholder string, input textinput.Model, controls dashboardBlock, width int, inline, invalid bool) dashboardBlock {
	fieldWidth := width
	if inline {
		fieldWidth -= ansi.StringWidth(controls.lines[0]) + 2
	}
	b := m.searchField(id, placeholder, input, fieldWidth, invalid)
	if inline {
		b.lines[1] += themed("  ", text, bg, false) + controls.lines[0]
		for _, hit := range controls.targets {
			hit.X += fieldWidth + 2
			hit.Y++
			b.targets = append(b.targets, hit)
		}
	} else {
		b.append(controls)
	}
	return b
}

func filterToolbarRows(width int) int {
	if width >= 68 {
		return 5
	}
	return 6
}
