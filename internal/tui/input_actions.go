package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// clearInputField routes the shared field's clear button through the same
// editing handlers as Ctrl+U, so validation and draft invalidation stay local
// to each form. Clearing never activates the field's submission action.
func (m *Model) clearInputField(id string) (bool, tea.Cmd) {
	field, clear := strings.CutSuffix(id, ".clear")
	if !clear {
		return false, nil
	}
	clearKey := tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	prepare := func(input *textinput.Model) {
		input.Focus()
		input.CursorEnd()
	}
	if m.ports.open {
		if field != "ports.mixed" && field != "ports.controller" {
			return false, nil
		}
		if m.busy || m.quitting || m.ports.step != "edit" {
			return true, nil
		}
		m.ports.focus = field
		input := &m.ports.mixed
		if field == "ports.controller" {
			input = &m.ports.controller
		}
		prepare(input)
		return true, m.updatePortsInput(clearKey)
	}
	if m.wizard.open || m.wizard.networkOpen {
		if (m.wizard.networkOpen && field != "prefix") || (!m.wizard.networkOpen && field != "local" && field != "version-input") {
			return false, nil
		}
		if m.busy || m.quitting {
			return true, nil
		}
		m.wizard.focus, m.wizard.reveal = field, true
		if input := m.wizardInput(); input != nil {
			prepare(input)
			return true, m.updateWizardInput(clearKey)
		}
		return true, nil
	}
	if m.form != "" && m.form != "tun-confirm" {
		if m.form == "profile-create" {
			var selected int
			switch field {
			case "profile-create.name":
				selected = profileCreateName
			case "profile-create.description":
				selected = profileCreateDescription
			case "profile-create.source":
				selected = profileCreateSource
			default:
				return false, nil
			}
			if m.busy || m.quitting || m.profileCreate.submitting {
				return true, nil
			}
			m.profileCreateSetField(selected)
			prepare(m.profileCreateActive())
			_, cmd := m.profileCreateKey(clearKey)
			return true, cmd
		}
		if field != "import-input" {
			return false, nil
		}
		if m.busy || m.quitting {
			return true, nil
		}
		m.focus = field
		if m.form == "profile-delete" {
			m.profileDeleteSetFocus(field)
		}
		m.paths.reset()
		prepare(&m.input)
		return true, m.formKey(clearKey)
	}
	if m.page == "overview" && m.inlineInstallVisible() && (field == "setup.local" || field == "setup.version-input") {
		if m.busy || m.quitting {
			return true, nil
		}
		m.focus = field
		m.wizard.focus = strings.TrimPrefix(field, "setup.")
		if input := m.wizardInput(); input != nil {
			prepare(input)
			return true, m.updateInlineInput(clearKey)
		}
		return true, nil
	}
	if m.page == "profiles" && field == "profiles.url" {
		if m.busy || m.quitting {
			return true, nil
		}
		m.focus, m.profilesUI.editing = field, true
		prepare(&m.profilesUI.url)
		_, cmd := m.profileURLKey(clearKey)
		m.clearProfileValidation()
		return true, cmd
	}
	return false, nil
}
