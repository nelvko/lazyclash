package tui

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func searchInputWidth(width int) int {
	return inputFieldTextWidth(min(inputFieldMaxWidth, width), 0, true)
}

func resizeSearchInput(input *textinput.Model, width int) { resizeInputField(input, width) }

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
	case "providers.search":
		return "providers.clear"
	}
	return ""
}

func (m *Model) searchField(id, placeholder string, input textinput.Model, width int, invalid bool) dashboardBlock {
	return m.searchToolbar(id, placeholder, input, dashboardBlock{}, width, invalid)
}

// Search fields use the same surface as their containing page or dialog. Their
// outline and trailing controls own a single bounded region, capped at 60 cells.
func (m *Model) searchToolbar(id, placeholder string, input textinput.Model, controls dashboardBlock, width int, invalid bool) dashboardBlock {
	surface := bg
	if id == "providers.search" {
		surface = raised
	}
	input.Placeholder = placeholder
	clearID := searchClearID(id)
	return renderInputField(input, inputFieldOptions{
		ID: id, ClearID: clearID, Surface: surface, Width: min(inputFieldMaxWidth, max(1, width)),
		Focused: m.focus == id, Hovered: m.hover == id, Invalid: invalid,
		Disabled:     m.busy || m.quitting,
		Compact:      id == "proxies.search" && m.proxySearchRows() == 1,
		ClearFocused: m.focus == clearID, ClearHovered: m.hover == clearID, Trailing: controls,
	})
}

func (m *Model) searchControlOptions(id string) buttonOptions {
	return buttonOptions{Surface: bg, Focused: m.focus == id, Hovered: m.hover == id,
		Plain: m.buttonPlain(), Dim: m.feedbackDialogOpen() || m.helpOpen || m.notification.panel.open || m.providers.open}
}

func searchControlWidth(controls dashboardBlock) int {
	if len(controls.lines) == 0 {
		return 0
	}
	return ansi.StringWidth(controls.lines[0])
}

func (m *Model) filterToolbarRows(width int) int {
	if m.page == "logs" {
		return m.logFilterRows(width)
	}
	rows := inputFieldRows(m.ruleSearchFieldWidth(width), searchControlWidth(m.ruleFilterButtons()), true)
	if m.rulesList().matchErr != nil {
		rows++
	}
	return rows
}

func (m *Model) searchMatchingButtons(prefix string, caseSensitive, wholeWord, regular bool) dashboardBlock {
	var b dashboardBlock
	line := ""
	for _, item := range []struct {
		id, label string
		selected  bool
	}{{prefix + ".case", "Aa", caseSensitive}, {prefix + ".word", "ab", wholeWord}, {prefix + ".regex", ".*", regular}} {
		o := m.searchControlOptions(item.id)
		o.Disabled = m.busy || m.quitting
		o.Selected = item.selected
		value := renderSearchToggle(item.label, o)
		if !o.Disabled {
			b.targets = append(b.targets, hitRegion{ID: item.id, X: ansi.StringWidth(line), W: ansi.StringWidth(value), H: 1})
		}
		line += value
	}
	b.lines = []string{line}
	return b
}

// One status cell per toggle keeps adjacent labels one cell apart. Plain
// terminals combine selection and focus in that cell; color uses text emphasis.
// The whole-word icon is always underlined, including when the option is off.
func renderSearchToggle(label string, o buttonOptions) string {
	foreground := muted
	if o.Selected {
		foreground = accent
	}
	if o.Hovered && !o.Disabled && !o.Selected {
		foreground = text
	}
	if o.Disabled {
		foreground = disabled
	}
	marker := " "
	if o.Plain && o.Selected {
		marker = "*"
	}
	if o.Focused && !o.Disabled {
		marker = "›"
		if o.Plain && o.Selected {
			marker = "+"
		}
	}
	face := lipgloss.NewStyle().Bold(o.Selected).Underline(label == "ab").Render(label)
	return themed(marker, focus, bg, o.Dim) + themed(face, foreground, bg, o.Dim)
}
