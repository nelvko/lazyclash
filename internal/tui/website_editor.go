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
	confirmRemove bool
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
	m.websiteEditor, m.form = e, "website-editor"
	return m.focusWebsiteEditor("websiteEditor.address")
}

func (m *Model) closeWebsiteEditor() {
	m.websiteEditor = websiteEditorState{}
	m.returnToSiteManager()
}

func (m *Model) websiteEditorInput() *textinput.Model {
	if m.form == "website-editor" && !m.websiteEditor.confirmRemove {
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
		controls := []string{"websiteEditor.cancel", "websiteEditor.confirm"}
		if !m.websiteEditor.confirmRemove {
			controls = []string{"websiteEditor.address", "websiteEditor.name", "websiteEditor.save", "websiteEditor.cancel"}
			if m.websiteEditor.original.URL != "" {
				controls = append(controls, "websiteEditor.remove")
			}
		}
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
	if m.busy || m.quitting {
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
		if e.confirmRemove {
			e.confirmRemove, e.err, e.canceled = false, "", false
			return m.focusWebsiteEditor("websiteEditor.remove")
		}
		m.closeWebsiteEditor()
		return nil
	case "websiteEditor.remove":
		if e.original.URL != "" && !e.confirmRemove {
			m.focusWebsiteEditor("websiteEditor.cancel")
			e.confirmRemove, e.err, e.canceled = true, "", false
		}
		return nil
	case "websiteEditor.save", "websiteEditor.confirm":
		original := e.original
		remove := e.confirmRemove
		if remove != (id == "websiteEditor.confirm") {
			return nil
		}
		var site app.Website
		if !remove {
			site = app.Website{Name: strings.TrimSpace(e.name.Value()), URL: strings.TrimSpace(e.address.Value())}
			if site.URL == "" {
				e.err = m.tr("websiteEditor.required")
				return m.focusWebsiteEditor("websiteEditor.address")
			}
		}
		ctx := m.newJob("website-editor")
		backend, request := m.backend, m.id
		e.err, e.canceled = "", false
		return func() tea.Msg {
			var sites []app.Website
			var err error
			switch {
			case remove:
				sites, err = backend.RemoveWebsite(ctx, original)
			case original.URL != "":
				sites, err = backend.UpdateWebsite(ctx, original, site)
			default:
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
	case msg.err == nil:
		// The store can finish publishing just before cancellation arrives.
		m.setWebsites(msg.sites)
		if m.websiteEditor.original.URL == "" {
			m.siteManager.selected = len(msg.sites) - 1
		}
		m.closeWebsiteEditor()
	case msg.sites != nil:
		// A concurrent edit or removal returns the current list from the store.
		m.setWebsites(msg.sites)
		m.closeWebsiteEditor()
		m.siteManager.err = m.localizeError(msg.err)
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
