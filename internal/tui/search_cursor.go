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

// activeInputField follows the same modal priority as renderLayout. An input
// behind another surface cannot claim the terminal cursor.
func (m Model) activeInputField() (textinput.Model, string, bool) {
	if m.busy || m.quitting || m.helpOpen || m.notification.panel.open || m.menu.open || m.sidebarOpen {
		return textinput.Model{}, "", false
	}
	if m.ports.open {
		if m.ports.step == "edit" {
			switch m.ports.focus {
			case "ports.mixed":
				return m.ports.mixed, m.ports.focus, true
			case "ports.controller":
				return m.ports.controller, m.ports.focus, true
			}
		}
		return textinput.Model{}, "", false
	}
	if m.wizard.open || m.wizard.networkOpen {
		if input := m.wizardInput(); input != nil {
			return *input, m.wizard.focus, true
		}
		return textinput.Model{}, "", false
	}
	if m.form != "" {
		if m.form == "profile-create" {
			if input := m.profileCreateActive(); input != nil && input.Focused() {
				return *input, profileCreateFieldID(m.profileCreate.field), true
			}
		} else if m.form != "tun-confirm" && m.input.Focused() {
			return m.input, "import-input", true
		}
		return textinput.Model{}, "", false
	}
	if m.ruleProviders.open {
		return m.ruleProviders.search, "rules.providers-search", m.ruleProviders.editing && m.focus == "rules.providers-search"
	}
	if m.inlineInputActive() {
		if m.focus == "setup.local" {
			return m.wizard.local, m.focus, true
		}
		return m.wizard.version, m.focus, true
	}
	switch m.page {
	case "profiles":
		return m.profilesUI.url, "profiles.url", m.profilesUI.editing && m.focus == "profiles.url"
	case "proxies":
		return m.proxies.search, "proxies.search", m.proxies.editing && m.focus == "proxies.search"
	case "connections":
		return m.connections.search, "conn.search", m.connections.editing && m.focus == "conn.search"
	case "rules":
		return m.rules.search, "rules.search", m.rules.editing && m.focus == "rules.search"
	case "logs":
		return m.logs.search, "logs.search", m.logs.editing && m.focus == "logs.search"
	}
	return textinput.Model{}, "", false
}

func (m Model) inputCursor() *tea.Cursor {
	input, id, editing := m.activeInputField()
	if !editing {
		return nil
	}
	for _, field := range m.targets {
		if field.ID != id || field.H != 3 || field.InputWidth <= 0 {
			continue
		}
		input = prepareInputField(input, field.InputWidth, bg, false)
		input.Focus()
		cursor := input.Cursor()
		column, ok := searchCursorColumn(input)
		if cursor == nil || !ok || column > field.InputWidth {
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
