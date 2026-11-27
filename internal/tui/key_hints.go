package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
)

type keyHintsState struct {
	hidden, pending bool
	generation      uint64
	originPage      string
}

type keyHintsSavedMsg struct {
	generation uint64
	hidden     bool
	err        error
}

func (m *Model) saveKeyHints(hidden bool) tea.Cmd {
	if m.keyHints.pending || m.quitting || m.keyHints.hidden == hidden {
		return nil
	}
	m.keyHints.pending = true
	m.keyHints.originPage = m.page
	m.keyHints.generation++
	id, previous := m.keyHints.generation, m.keyHints.hidden
	backend, parent := m.backend, m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		defer cancel()
		observed := hidden
		err := backend.SaveKeyHintsPreference(ctx, hidden)
		if err != nil {
			// The write may have published the preference before returning an
			// error. Read it back, keeping the last observation if that also fails.
			readCtx, stop := context.WithTimeout(parent, 3*time.Second)
			defer stop()
			prefs, readErr := backend.ReadPreferences(readCtx)
			if readErr == nil {
				observed = prefs.HideKeyHints
			} else {
				observed = previous
				err = errors.Join(err, readErr)
			}
		}
		return keyHintsSavedMsg{generation: id, hidden: observed, err: err}
	}
}

func (m *Model) acceptKeyHintsSaved(msg keyHintsSavedMsg) tea.Cmd {
	if !m.keyHints.pending || msg.generation != m.keyHints.generation {
		return nil
	}
	m.keyHints.pending = false
	if m.keyHints.hidden != msg.hidden {
		m.keyHints.hidden = msg.hidden
		m.revealContentFocus()
	}
	const key = "key-hints.save"
	if msg.err == nil {
		m.resolveNotificationIssue(key)
	} else if m.ctx.Err() == nil && !m.quitting {
		n := &m.notification
		n.nextOperation++
		n.publish(notificationItem{
			Operation: n.nextOperation, Key: key, Page: m.keyHints.originPage,
			Kind: "warning", Message: m.tr("keyHints.saveFailed", m.localizeError(msg.err)),
		}, n.itemIndex(0, key))
	}
	if m.quitting && !m.busy {
		return tea.Quit
	}
	return nil
}
