package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// Bubbles' real cursor currently counts absolute rune positions, without its
// horizontal viewport offset. Measure its own virtual rendering on a copy to
// obtain the visible cell position, including wide characters. This probe is
// never part of the displayed canvas; the terminal draws the actual bar.
func searchCursorColumn(input textinput.Model) (int, bool) {
	marker := lipgloss.Color("#010203")
	styles := input.Styles()
	styles.Cursor.Color, styles.Cursor.Blink = marker, false
	input.SetStyles(styles)
	input.SetVirtualCursor(true)
	open, _, _ := strings.Cut(lipgloss.NewStyle().Foreground(marker).Inline(true).Reverse(true).Render("x"), "x")
	if open == "" {
		return 0, false
	}
	prefix, _, found := strings.Cut(input.View(), open)
	return ansi.StringWidth(prefix), found
}

func (m Model) searchCursor() *tea.Cursor {
	if m.busy || m.quitting || m.helpOpen || m.ports.open || m.wizard.open || m.wizard.networkOpen ||
		m.form != "" || m.notification.panel.open || m.menu.open || m.sidebarOpen {
		return nil
	}
	var input textinput.Model
	var id string
	var editing bool
	if m.ruleProviders.open {
		input, id, editing = m.ruleProviders.search, "rules.providers-search", m.ruleProviders.editing
	} else {
		switch m.page {
		case "proxies":
			input, id, editing = m.proxies.search, "proxies.search", m.proxies.editing
		case "connections":
			input, id, editing = m.connections.search, "conn.search", m.connections.editing
		case "rules":
			input, id, editing = m.rules.search, "rules.search", m.rules.editing
		case "logs":
			input, id, editing = m.logs.search, "logs.search", m.logs.editing
		default:
			return nil
		}
	}
	if !editing || !input.Focused() || m.focus != id {
		return nil
	}
	for _, field := range m.targets {
		if field.ID != id || field.H != 3 || field.W < 5 {
			continue
		}
		input = prepareSearchInput(input, field.W, panel, false)
		cursor := input.Cursor()
		column, ok := searchCursorColumn(input)
		if cursor == nil || !ok || column >= field.W-4 {
			return nil
		}
		cursor.Position.X, cursor.Position.Y = field.X+2+column, field.Y+1
		if cursor.Position.X < 0 || cursor.Position.X >= m.width || cursor.Position.Y < 0 || cursor.Position.Y >= m.height {
			return nil
		}
		profile := m.colorProfile
		if m.noColor {
			profile = colorprofile.ASCII
		}
		cursor.Color = terminalColor(cursor.Color, profile)
		return cursor
	}
	return nil
}
