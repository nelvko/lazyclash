package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
)

type keyHintsState struct {
	hidden, changed, pending bool
	generation               uint64
	helpFocus                string
}

type keyHintsSavedMsg struct {
	generation uint64
	hidden     bool
	err        error
}

func (m *Model) loadKeyHintsPreference(hidden bool) {
	// Startup reads can finish after the user has already toggled the bar.
	if !m.keyHints.changed {
		m.keyHints.hidden = hidden
	}
}

func (m *Model) toggleKeyHints() tea.Cmd {
	if m.keyHints.pending || m.quitting {
		return nil
	}
	m.keyHints.pending = true
	m.keyHints.generation++
	id, hidden, backend, ctx := m.keyHints.generation, !m.keyHints.hidden, m.backend, m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		return keyHintsSavedMsg{generation: id, hidden: hidden, err: backend.SaveKeyHintsPreference(ctx, hidden)}
	}
}

func (m *Model) acceptKeyHintsSaved(msg keyHintsSavedMsg) tea.Cmd {
	if !m.keyHints.pending || msg.generation != m.keyHints.generation {
		return nil
	}
	m.keyHints.pending = false
	if msg.err == nil {
		m.keyHints.changed = true
		m.keyHints.hidden = msg.hidden
		m.revealContentFocus()
	}
	if msg.err != nil && m.ctx.Err() == nil && !m.quitting {
		m.preferenceSaveWarning("key-hints.save", m.tr("keyHints.saveFailed", m.localizeError(msg.err)))
	}
	if m.quitting && !m.busy {
		return m.quitWhenSaved()
	}
	return nil
}

func (m *Model) keyHintsShortcut(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if k.String() != "ctrl+t" || m.providers.open || m.menu.open || m.ports.open || m.wizard.open || m.wizard.networkOpen || m.form != "" || m.notification.panel.open {
		return false, nil
	}
	if !m.helpOpen && (m.inlineInputActive() || (m.page == "profiles" && m.focus == "profiles.url" && !m.sidebarOpen) ||
		(m.page == "proxies" && m.proxies.editing) ||
		(m.page == "connections" && (m.connections.editing || m.connections.confirming || m.connections.detail != nil)) ||
		(m.page == "rules" && (m.rules.editing || m.providers.editing)) || (m.page == "logs" && m.logs.editing)) {
		return false, nil
	}
	if k.IsRepeat {
		return true, nil
	}
	return true, m.toggleKeyHints()
}

func (m *Model) keyHintsAction(id string) (bool, tea.Cmd) {
	switch id {
	case "help.open":
		m.helpOpen, m.helpScroll = true, 0
		m.keyHints.helpFocus = "help.close"
		return true, nil
	case "help.close":
		m.helpOpen = false
		m.revealContentFocus()
		return true, nil
	case "key-hints.toggle":
		return true, m.toggleKeyHints()
	default:
		return false, nil
	}
}
