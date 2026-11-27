package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

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
	options := dropdown{ShowCursor: true, Items: []menuItem{
		{ID: "edit", Label: m.tr("dashboard.website.edit")},
		{ID: "remove", Label: m.tr("dashboard.website.remove"), Danger: true},
	}}
	options.show(&m.menu, anchor)
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
	if !slices.Contains(m.websites, site) || !m.pageSelectionMenuAvailable("overview") {
		return nil
	}
	switch result.Action {
	case "edit":
		return m.beginWebsiteEdit(site)
	case "remove":
		return m.removeWebsite(site)
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

type websiteChangeMsg struct {
	id    uint64
	site  app.Website
	sites []app.Website
	err   error
}

func (m *Model) removeWebsite(site app.Website) tea.Cmd {
	ctx := m.newJob("website-remove")
	backend, request := m.backend, m.id
	return func() tea.Msg {
		sites, err := backend.RemoveWebsite(ctx, site)
		return websiteChangeMsg{id: request, site: site, sites: sites, err: err}
	}
}

func (m *Model) relocateWebsite(site, neighbor app.Website, after bool) tea.Cmd {
	if site == neighbor || !m.pageSelectionMenuAvailable("overview") {
		return nil
	}
	backend := m.backend
	ctx := m.newJob("website-move")
	request := m.id
	return func() tea.Msg {
		sites, err := backend.RelocateWebsite(ctx, site, neighbor, after)
		return websiteChangeMsg{id: request, site: site, sites: sites, err: err}
	}
}

func (m *Model) acceptWebsiteChange(msg websiteChangeMsg) tea.Cmd {
	job := m.job
	if m.id != msg.id || (job != "website-move" && job != "website-remove") {
		return nil
	}
	m.abortLookup()
	if msg.sites != nil {
		index := slices.IndexFunc(msg.sites, func(s app.Website) bool { return s.URL == msg.site.URL })
		if job == "website-remove" && index < 0 {
			index = slices.Index(m.websites, msg.site)
		}
		m.setWebsites(msg.sites)
		if m.pageSelectionMenuAvailable("overview") {
			m.focusWebsiteAt(index)
		}
	}
	if msg.err != nil {
		m.prepareNotification(job, m.tr("dashboard.website.title"))
		m.message, m.notification.warning = m.localizeError(msg.err), true
	}
	if m.quitting && !m.keyHints.pending {
		return tea.Quit
	}
	return nil
}
