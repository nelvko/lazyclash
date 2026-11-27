package tui

import (
	"context"
	"errors"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

type websiteEditorState struct {
	address, name textinput.Model
	original      app.Website
	err           string
	canceled      bool
}

type websiteListSavedMsg struct {
	id    uint64
	sites []app.Website
	err   error
}

func (m *Model) beginWebsiteEdit(original app.Website) tea.Cmd {
	e := websiteEditorState{original: original, address: textinput.New(), name: textinput.New()}
	e.address.Placeholder = "example.com"
	e.name.Placeholder = m.tr("websiteEditor.hostname")
	e.address.SetValue(original.URL)
	e.name.SetValue(original.Name)
	m.websiteEditor, m.form, m.hover = e, "website-editor", ""
	return m.focusWebsiteEditor("websiteEditor.address")
}

func (m *Model) closeWebsiteEditor(index int) {
	m.websiteEditor, m.form, m.hover = websiteEditorState{}, "", ""
	m.focusWebsiteAt(index)
}

func (e *websiteEditorState) draft() app.Website {
	return app.Website{Name: strings.TrimSpace(e.name.Value()), URL: strings.TrimSpace(e.address.Value())}
}

func (m *Model) websiteEditorInput() *textinput.Model {
	if m.form == "website-editor" {
		switch m.focus {
		case "websiteEditor.address":
			return &m.websiteEditor.address
		case "websiteEditor.name":
			return &m.websiteEditor.name
		}
	}
	return nil
}

func (m *Model) focusWebsiteEditor(id string) tea.Cmd {
	if field := m.websiteEditorInput(); field != nil {
		field.Blur()
	}
	m.focus = id
	if field := m.websiteEditorInput(); field != nil {
		return field.Focus()
	}
	return nil
}

func (m *Model) updateWebsiteEditorInput(msg tea.Msg) tea.Cmd {
	input := m.websiteEditorInput()
	if m.busy || m.quitting || input == nil {
		return nil
	}
	before := input.Value()
	var cmd tea.Cmd
	*input, cmd = input.Update(msg)
	if before != input.Value() {
		m.websiteEditor.err, m.websiteEditor.canceled = "", false
	}
	return cmd
}

func (m *Model) websiteEditorKey(key tea.KeyPressMsg) tea.Cmd {
	if m.busy || m.quitting {
		if !m.quitting && key.String() == "esc" && m.job == "website-editor" && m.opCtx.Err() == nil {
			m.cancelTask(false)
		}
		return nil
	}
	if key.String() == "esc" {
		return m.websiteEditorAction("websiteEditor.cancel")
	}
	if len(m.targets) == 0 {
		return nil
	}
	switch key.String() {
	case "tab", "shift+tab":
		controls := []string{"websiteEditor.address", "websiteEditor.name", "websiteEditor.cancel", "websiteEditor.save"}
		i := slices.Index(controls, m.focus)
		delta := 1
		if key.String() == "shift+tab" {
			delta = -1
		}
		return m.focusWebsiteEditor(controls[(i+len(controls)+delta)%len(controls)])
	case "enter":
		if m.focus == "websiteEditor.address" {
			if strings.TrimSpace(m.websiteEditor.address.Value()) == "" {
				m.websiteEditor.err = m.tr("websiteEditor.required")
				return nil
			}
			return m.focusWebsiteEditor("websiteEditor.name")
		}
		if m.focus == "websiteEditor.name" {
			return m.websiteEditorAction("websiteEditor.save")
		}
		return m.websiteEditorAction(m.focus)
	}
	return m.updateWebsiteEditorInput(key)
}

func (m *Model) websiteEditorAction(id string) tea.Cmd {
	if m.form != "website-editor" || m.busy || m.quitting {
		return nil
	}
	if field, ok := strings.CutSuffix(id, ".clear"); ok {
		cmd := m.focusWebsiteEditor(field)
		if input := m.websiteEditorInput(); input != nil {
			input.Reset()
			m.websiteEditor.err, m.websiteEditor.canceled = "", false
		}
		return cmd
	}
	e := &m.websiteEditor
	switch id {
	case "websiteEditor.address", "websiteEditor.name":
		return m.focusWebsiteEditor(id)
	case "websiteEditor.cancel":
		m.closeWebsiteEditor(slices.Index(m.websites, e.original))
		return nil
	case "websiteEditor.save":
		original := e.original
		site := e.draft()
		if site.URL == "" {
			e.err = m.tr("websiteEditor.required")
			return m.focusWebsiteEditor("websiteEditor.address")
		}
		ctx := m.newJob("website-editor")
		backend, request := m.backend, m.id
		e.err, e.canceled = "", false
		return func() tea.Msg {
			var sites []app.Website
			var err error
			if original.URL != "" {
				sites, err = backend.UpdateWebsite(ctx, original, site)
			} else {
				sites, err = backend.AddWebsite(ctx, site)
			}
			return websiteListSavedMsg{id: request, sites: sites, err: err}
		}
	}
	return nil
}

func (m *Model) acceptWebsiteList(msg websiteListSavedMsg) tea.Cmd {
	if m.id != msg.id || m.job != "website-editor" {
		return nil
	}
	m.abortLookup()
	switch {
	case msg.err == nil || msg.sites != nil:
		e := &m.websiteEditor
		index := slices.Index(m.websites, e.original)
		if msg.err == nil {
			// Editing a canonical address or moving the site in another session
			// can change both its identity and position while the form is open.
			if saved, err := e.draft().Normalized(); err == nil {
				index = slices.Index(msg.sites, saved)
			}
		} else if current := slices.IndexFunc(msg.sites, func(s app.Website) bool { return s.URL == e.original.URL }); current >= 0 {
			index = current
		}
		m.setWebsites(msg.sites)
		m.closeWebsiteEditor(index)
		if msg.err != nil {
			m.prepareNotification("website-editor", m.tr("dashboard.website.title"))
			m.message, m.notification.warning = m.localizeError(msg.err), true
		}
	case errors.Is(msg.err, context.Canceled):
		m.websiteEditor.canceled = true
	default:
		m.websiteEditor.err = m.localizeError(msg.err)
	}
	if m.quitting && !m.keyHints.pending {
		return tea.Quit
	}
	return nil
}
