package tui

import (
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

func (m *Model) websiteMenuItems(index int) dropdown {
	items := []menuItem{{ID: "edit", Label: m.tr("dashboard.website.edit")}}
	if index > 0 {
		items = append(items, menuItem{ID: "up", Label: m.tr("dashboard.website.moveUp")})
	}
	if index+1 < len(m.websites) {
		items = append(items, menuItem{ID: "down", Label: m.tr("dashboard.website.moveDown")})
	}
	items = append(items, menuItem{ID: "remove", Label: m.tr("websiteEditor.confirm"), Danger: true})
	return dropdown{Items: items, ShowCursor: true}
}

func (m *Model) openWebsiteMenuAt(x, y int) {
	if m.notification.covers(x, y) {
		return
	}
	for _, target := range m.targets {
		if strings.HasPrefix(target.ID, overviewWebsiteDragPrefix) &&
			regionsOverlap(target, hitRegion{X: x, Y: y, W: 1, H: 1}) {
			m.openWebsiteMenu(target)
			return
		}
	}
}

func (m *Model) websiteMenuKey(key string) bool {
	if key != "m" && key != "menu" && key != "shift+f10" || !m.pageSelectionMenuAvailable("overview") {
		return false
	}
	i := m.overviewWebsiteIndex(m.focus)
	if i < 0 {
		return false
	}
	for _, target := range m.targets {
		if target.ID == overviewWebsiteDragID(m.websites[i]) || target.ID == websiteActionID(m.websites[i]) {
			m.openWebsiteMenu(target)
			return true
		}
	}
	return false
}

func (m *Model) openWebsiteMenu(anchor hitRegion) {
	i := m.overviewWebsiteIndex(anchor.ID)
	if i < 0 || m.menu.open || !m.pageSelectionMenuAvailable("overview") {
		return
	}
	m.websiteMenu = m.websites[i]
	m.focus, m.hover = websiteActionID(m.websiteMenu), ""
	m.websiteMenuItems(i).show(&m.menu, anchor)
}

func (m *Model) dismissWebsiteMenu() {
	m.menu.dismiss()
	m.websiteMenu, m.hover = app.Website{}, ""
}

func (m *Model) websiteMenuResult(result menuResult) tea.Cmd {
	if !result.Closed {
		return nil
	}
	site := m.websiteMenu
	m.dismissWebsiteMenu()
	i := slices.Index(m.websites, site)
	if i < 0 || !m.pageSelectionMenuAvailable("overview") {
		return nil
	}
	switch result.Action {
	case "edit":
		return m.beginWebsiteEdit(site)
	case "remove":
		return m.beginWebsiteRemoval(site)
	case "up", "down":
		direction := 1
		if result.Action == "up" {
			direction = -1
		}
		if i+direction >= 0 && i+direction < len(m.websites) {
			backend := m.backend
			return m.startWebsiteMove(site, func(ctx context.Context) ([]app.Website, error) {
				return backend.MoveWebsite(ctx, site, direction)
			})
		}
	}
	return nil
}

func (m *Model) focusWebsiteAt(index int) {
	if len(m.websites) == 0 || index < 0 {
		m.focus = "website.add"
	} else {
		m.websiteOffset = min(index, len(m.websites)-1)
		m.focus = websiteActionID(m.websites[m.websiteOffset])
	}
	m.revealContentFocus()
}

type websiteMoveMsg struct {
	id    uint64
	site  app.Website
	sites []app.Website
	err   error
}

func (m *Model) relocateWebsite(site, neighbor app.Website, after bool) tea.Cmd {
	if site == neighbor || !m.pageSelectionMenuAvailable("overview") {
		return nil
	}
	backend := m.backend
	return m.startWebsiteMove(site, func(ctx context.Context) ([]app.Website, error) {
		return backend.RelocateWebsite(ctx, site, neighbor, after)
	})
}

func (m *Model) startWebsiteMove(site app.Website, save func(context.Context) ([]app.Website, error)) tea.Cmd {
	ctx := m.newJob("website-move")
	request := m.id
	return func() tea.Msg {
		sites, err := save(ctx)
		return websiteMoveMsg{id: request, site: site, sites: sites, err: err}
	}
}

func (m *Model) acceptWebsiteMove(msg websiteMoveMsg) tea.Cmd {
	if m.id != msg.id || m.job != "website-move" {
		return nil
	}
	m.abortLookup()
	if msg.sites != nil {
		m.setWebsites(msg.sites)
		if m.pageSelectionMenuAvailable("overview") {
			m.focusWebsiteAt(slices.Index(msg.sites, msg.site))
		}
	}
	if msg.err != nil {
		m.prepareNotification("website-move", m.tr("dashboard.website.title"))
		m.message, m.notification.warning = m.localizeError(msg.err), true
	}
	if m.quitting && !m.keyHints.pending {
		return tea.Quit
	}
	return nil
}
