package tui

import (
	"context"
	"errors"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

type siteManagerState struct {
	selected, offset int
	err              string
}

type websiteMoveMsg struct {
	id    uint64
	site  app.Website
	sites []app.Website
	err   error
}

func (m *Model) returnToSiteManager() {
	m.form, m.hover, m.siteManager.err = "website-manager", "", ""
	m.selectManagerSite(m.siteManager.selected)
}

func (m *Model) selectManagerSite(i int) {
	m.siteManager.selected = max(0, min(i, len(m.websites)-1))
	m.focus = "websiteManager.list"
	if len(m.websites) == 0 {
		m.focus = "websiteManager.add"
	}
}

func (m *Model) siteManagerButtons() []profileButton {
	var buttons []profileButton
	for _, item := range []struct {
		id      string
		enabled bool
	}{
		{"websiteManager.add", true},
		{"websiteManager.edit", len(m.websites) > 0},
		{"websiteManager.up", m.siteManager.selected > 0},
		{"websiteManager.down", m.siteManager.selected+1 < len(m.websites)},
		{"websiteManager.close", true},
	} {
		button := profileButton{label: m.tr(item.id)}
		if item.enabled {
			button.id = item.id
		}
		buttons = append(buttons, button)
	}
	return buttons
}

func (m *Model) siteManagerKey(k tea.KeyPressMsg) tea.Cmd {
	key := k.String()
	if m.busy || m.quitting {
		if key == "esc" && m.job == "website-move" && !m.quitting {
			m.cancelTask(false)
		}
		return nil
	}
	if len(m.targets) == 0 && key != "esc" {
		return nil
	}
	switch key {
	case "esc":
		return m.siteManagerAction("websiteManager.close")
	case "up", "down":
		delta := 1
		if key == "up" {
			delta = -1
		}
		m.selectManagerSite(m.siteManager.selected + delta)
	case "shift+up":
		return m.siteManagerAction("websiteManager.up")
	case "shift+down":
		return m.siteManagerAction("websiteManager.down")
	case "tab", "shift+tab":
		var ids []string
		if len(m.websites) > 0 {
			ids = append(ids, "websiteManager.list")
		}
		for _, b := range m.siteManagerButtons() {
			if b.id != "" {
				ids = append(ids, b.id)
			}
		}
		delta := 1
		if key == "shift+tab" {
			delta = -1
		}
		m.focus = ids[(slices.Index(ids, m.focus)+delta+len(ids))%len(ids)]
	case "enter":
		return m.siteManagerAction(m.focus)
	}
	return nil
}

func (m *Model) siteManagerWheel(mouse tea.Mouse) {
	if m.busy || m.quitting || mouse.Button != tea.MouseWheelUp && mouse.Button != tea.MouseWheelDown {
		return
	}
	for _, target := range m.targets {
		if strings.HasPrefix(target.ID, "websiteManager.site.") &&
			regionsOverlap(target, hitRegion{X: mouse.X, Y: mouse.Y, W: 1, H: 1}) {
			delta := 1
			if mouse.Button == tea.MouseWheelUp {
				delta = -1
			}
			m.selectManagerSite(m.siteManager.selected + delta)
			return
		}
	}
}

func (m *Model) siteManagerAction(id string) tea.Cmd {
	if m.busy || m.quitting || m.form != "website-manager" {
		return nil
	}
	if url, ok := strings.CutPrefix(id, "websiteManager.site."); ok {
		for i, site := range m.websites {
			if site.URL == url {
				m.selectManagerSite(i)
				break
			}
		}
		return nil
	}
	switch id {
	case "websiteManager.close":
		m.form, m.hover, m.focus = "", "", "website.manage"
		m.revealContentFocus()
	case "websiteManager.add":
		return m.beginWebsiteEdit(app.Website{})
	case "websiteManager.edit", "websiteManager.list":
		if len(m.websites) > 0 {
			return m.beginWebsiteEdit(m.websites[m.siteManager.selected])
		}
	case "websiteManager.up", "websiteManager.down":
		delta := 1
		if id == "websiteManager.up" {
			delta = -1
		}
		selected := m.siteManager.selected
		if selected+delta < 0 || selected+delta >= len(m.websites) {
			return nil
		}
		site := m.websites[selected]
		ctx := m.newJob("website-move")
		backend, request := m.backend, m.id
		m.siteManager.err = ""
		return func() tea.Msg {
			sites, err := backend.MoveWebsite(ctx, site, delta)
			return websiteMoveMsg{id: request, site: site, sites: sites, err: err}
		}
	}
	return nil
}

func (m *Model) acceptWebsiteMove(msg websiteMoveMsg) tea.Cmd {
	if m.id != msg.id || m.job != "website-move" {
		return nil
	}
	m.abortLookup()
	if msg.sites != nil {
		previousFocus := m.focus
		m.setWebsites(msg.sites)
		if i := slices.Index(msg.sites, msg.site); i >= 0 {
			m.selectManagerSite(i)
		} else {
			m.selectManagerSite(m.siteManager.selected)
		}
		if previousFocus == "websiteManager.up" && m.siteManager.selected > 0 ||
			previousFocus == "websiteManager.down" && m.siteManager.selected+1 < len(m.websites) {
			m.focus = previousFocus
		}
	}
	if msg.err != nil {
		m.siteManager.err = m.localizeError(msg.err)
		if errors.Is(msg.err, context.Canceled) {
			m.siteManager.err = m.tr("websiteEditor.notSaved")
		}
	}
	if m.quitting && !m.keyHints.pending {
		return tea.Quit
	}
	return nil
}
