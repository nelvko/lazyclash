package tui

import (
	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"
)

// The frame uses two border cells, two padding cells, the "/ " prompt and
// Bubbles' extra cursor cell. Editing and horizontal scrolling stay in Bubbles.
func searchInputWidth(width int) int { return max(1, width-7) }

func (m Model) searchField(id, placeholder string, input textinput.Model, width int, invalid bool) dashboardBlock {
	input.Prompt, input.Placeholder = "/ ", placeholder
	input.SetWidth(searchInputWidth(width))
	input.SetCursor(input.Position())
	styleInput(&input, panel, invalid)
	edge := divider
	if m.hover == id {
		edge = accent
	}
	if m.focus == id || input.Focused() {
		edge = focus
	}
	if invalid {
		edge = errorColor
	}
	b := dashboardBlock{lines: inputFrame([]string{input.View()}, width, edge, panel)}
	if !m.busy && !m.quitting {
		b.targets = []hitRegion{{ID: id, W: width, H: len(b.lines)}}
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
