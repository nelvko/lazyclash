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
	m.keyHints.hidden = !m.keyHints.hidden
	m.keyHints.changed = true
	m.revealContentFocus()
	return m.saveKeyHints()
}

func (m *Model) saveKeyHints() tea.Cmd {
	if m.keyHints.pending {
		return nil
	}
	m.keyHints.pending = true
	m.keyHints.generation++
	id, hidden, backend, ctx := m.keyHints.generation, m.keyHints.hidden, m.backend, m.ctx
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
	// Serialize saves and coalesce intermediate toggles so the final choice wins.
	if msg.hidden != m.keyHints.hidden {
		return m.saveKeyHints()
	}
	if msg.err != nil && m.ctx.Err() == nil && !m.quitting {
		n := &m.notification
		n.nextID++
		n.nextOperation++
		n.generation++
		n.items = append(n.items, notificationItem{
			ID: n.nextID, Operation: n.nextOperation, Key: "key-hints.save", Page: m.page,
			Kind: "warning", Message: m.tr("keyHints.saveFailed", m.localizeError(msg.err)),
			remaining: 4 * time.Second, generation: n.generation,
		})
		n.visible = true
	}
	if m.quitting && !m.busy {
		return tea.Quit
	}
	return nil
}

func (m *Model) keyHintsShortcut(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if k.String() != "ctrl+t" || m.menu.open || m.ports.open || m.wizard.open || m.wizard.networkOpen || m.form != "" || m.notification.panel.open {
		return false, nil
	}
	if !m.helpOpen && (m.inlineInputActive() || (m.page == "profiles" && m.focus == "profiles.url" && !m.sidebarOpen) ||
		(m.page == "proxies" && m.proxies.editing) ||
		(m.page == "connections" && (m.connections.editing || m.connections.confirming)) ||
		(m.page == "rules" && (m.rules.editing || m.ruleProviders.editing)) || (m.page == "logs" && m.logs.editing)) {
		return false, nil
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
