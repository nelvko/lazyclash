package tui

import (
	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"
)

func searchInputWidth(width int) int {
	return inputFieldTextWidth(min(inputFieldMaxWidth, width), 0, true)
}

func resizeSearchInput(input *textinput.Model, width int) { resizeInputField(input, width) }

func searchClearID(id string) string {
	switch id {
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

func (m Model) searchField(id, placeholder string, input textinput.Model, width int, invalid bool) dashboardBlock {
	return m.searchToolbar(id, placeholder, input, dashboardBlock{}, width, invalid)
}

// Search fields use the same surface as their containing page or dialog. Their
// outline and trailing controls own a single bounded region, capped at 60 cells.
func (m Model) searchToolbar(id, placeholder string, input textinput.Model, controls dashboardBlock, width int, invalid bool) dashboardBlock {
	surface := bg
	if id == "rules.providers-search" {
		surface = raised
	}
	input.Placeholder = placeholder
	clearID := searchClearID(id)
	return renderInputField(input, inputFieldOptions{
		ID: id, ClearID: clearID, Surface: surface, Width: min(inputFieldMaxWidth, max(1, width)),
		Focused: m.focus == id, Hovered: m.hover == id, Invalid: invalid,
		Disabled:     m.busy || m.quitting,
		ClearFocused: m.focus == clearID, ClearHovered: m.hover == clearID, Trailing: controls,
	})
}

func (m Model) searchControlOptions(id string) buttonOptions {
	return buttonOptions{Surface: bg, Focused: m.focus == id, Hovered: m.hover == id,
		Plain: m.buttonPlain(), Dim: m.feedbackDialogOpen() || m.helpOpen || m.notification.panel.open || m.ruleProviders.open}
}

func searchControlWidth(controls dashboardBlock) int {
	if len(controls.lines) == 0 {
		return 0
	}
	return ansi.StringWidth(controls.lines[0])
}

func (m Model) searchToolbarGap() int {
	if m.height >= 24 {
		return 1
	}
	return 0
}

func (m Model) searchToolbarRows(width int, controls dashboardBlock) int {
	return inputFieldRows(min(inputFieldMaxWidth, max(1, width)), searchControlWidth(controls), true) + 2 + m.searchToolbarGap()
}

func (m Model) filterToolbarRows(width int) int {
	if m.page == "logs" {
		return m.searchToolbarRows(width, m.logControls())
	}
	return m.searchToolbarRows(width, m.ruleFilterButtons())
}
