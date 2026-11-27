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
	remove        app.Website
	err           string
	canceled      bool
}

type websiteListSavedMsg struct {
	id    uint64
	sites []app.Website
	err   error
}

func (m *Model) websiteManagerMenu() dropdown {
	d := dropdown{ShowCursor: true, CenterPopup: true, Items: []menuItem{{ID: "add", Label: m.tr("websiteEditor.add")}}}
	for _, site := range m.websites {
		d.Items = append(d.Items, menuItem{ID: site.URL, Label: m.tr("websiteEditor.removeSite", clean(site.Name)), Danger: true})
	}
	return d
}

func (m *Model) openWebsiteManager() tea.Cmd {
	m.openPageSelectionMenu("overview", "website.manage", m.websiteManagerMenu())
	return nil
}

func (m *Model) websiteManagerChoice(result menuResult) tea.Cmd {
	if !result.Closed || result.Action == "" || !m.pageSelectionMenuAvailable("overview") {
		return nil
	}
	m.hover = ""
	if result.Action == "add" {
		m.websiteEditor = websiteEditorState{address: textinput.New(), name: textinput.New()}
		m.websiteEditor.address.Placeholder = "example.com"
		m.websiteEditor.name.Placeholder = m.tr("websiteEditor.hostname")
		m.form = "website-editor"
		return m.focusWebsiteEditor("websiteEditor.address")
	}
	for _, site := range m.websites {
		if site.URL == result.Action {
			m.websiteEditor = websiteEditorState{remove: site}
			m.form, m.focus = "website-editor", "websiteEditor.cancel"
			break
		}
	}
	return nil
}

func (m *Model) closeWebsiteEditor() {
	m.websiteEditor = websiteEditorState{}
	m.form, m.hover, m.focus = "", "", "website.manage"
	m.revealContentFocus()
}

func (m *Model) websiteEditorInput() *textinput.Model {
	if m.form == "website-editor" && m.websiteEditor.remove.URL == "" {
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
		m.closeWebsiteEditor()
		return nil
	}
	if len(m.targets) == 0 {
		return nil
	}
	switch key.String() {
	case "tab", "shift+tab":
		controls := []string{"websiteEditor.cancel", "websiteEditor.confirm"}
		if m.websiteEditor.remove.URL == "" {
			controls = []string{"websiteEditor.address", "websiteEditor.name", "websiteEditor.save", "websiteEditor.cancel"}
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
		m.closeWebsiteEditor()
		return nil
	case "websiteEditor.save", "websiteEditor.confirm":
		site := e.remove
		remove := site.URL != ""
		if remove != (id == "websiteEditor.confirm") {
			return nil
		}
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
			if remove {
				sites, err = backend.RemoveWebsite(ctx, site)
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
	case msg.err == nil:
		// The store can finish publishing just before cancellation arrives.
		m.setWebsites(msg.sites)
		m.closeWebsiteEditor()
	case msg.sites != nil:
		// A stale deletion includes the authoritative list read under the store lock.
		m.setWebsites(msg.sites)
		m.closeWebsiteEditor()
		m.prepareNotification("website-editor", m.tr("websiteEditor.removeTitle"))
		m.message, m.notification.warning = m.localizeError(msg.err), true
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
