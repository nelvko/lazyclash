package tui

import (
	"cmp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

type overviewWebsiteDragState struct {
	source, neighbor app.Website
	after            bool
	pager            hitRegion
	direction        string
}

func (d overviewWebsiteDragState) active() bool { return d.source.URL != "" }

func (m *Model) cancelOverviewWebsiteDrag() { m.websiteDrag = overviewWebsiteDragState{} }

func (m *Model) overviewWebsiteDragMouse(event tea.MouseMsg) (bool, tea.Cmd) {
	d := &m.websiteDrag
	p := event.Mouse()
	_, press := event.(tea.MouseClickMsg)
	if press || p.Button != tea.MouseLeft {
		// A new press or a free-moving pointer recovers a lost release.
		m.cancelOverviewWebsiteDrag()
	}
	if p.Button != tea.MouseLeft || !press && !d.active() {
		return false, nil
	}
	if !m.pageSelectionMenuAvailable("overview") || m.menu.open {
		m.cancelOverviewWebsiteDrag()
		return false, nil
	}
	point := hitRegion{X: p.X, Y: p.Y, W: 1, H: 1}
	var hit hitRegion
	if !m.notification.covers(p.X, p.Y) && !regionsOverlap(point, m.tooltip.bounds) {
		for _, target := range m.targets {
			if regionsOverlap(point, target) {
				hit = target
				break
			}
		}
	}
	_, release := event.(tea.MouseReleaseMsg)
	if press {
		if strings.HasPrefix(hit.ID, overviewWebsiteDropPrefix) {
			// Ignore a fresh press on the previous gesture's transient arrow.
			return true, nil
		}
		if !strings.HasPrefix(hit.ID, overviewWebsiteDragPrefix) {
			return false, nil
		}
		i := m.overviewWebsiteIndex(hit.ID)
		if i < 0 {
			return false, nil
		}
		site := m.websites[i]
		m.tooltip.dismiss()
		m.hover, m.focus = "", websiteActionID(site)
		if len(m.websites) > 1 {
			d.source, d.neighbor = site, site
		}
		return true, nil
	}
	if release {
		defer m.cancelOverviewWebsiteDrag()
	}
	// A shorter last page can move an unrelated control under the stationary
	// pointer. The old button stays inert until the pointer leaves it.
	if d.pager.ID != "" && (d.pager.ID == hit.ID || regionsOverlap(point, d.pager)) {
		return true, nil
	}
	if release {
		m.hover = hit.ID
	}
	d.pager, d.direction = hitRegion{}, ""
	if !release && (hit.ID == "website.prev" || hit.ID == "website.next") {
		d.neighbor, d.after, d.pager = app.Website{}, false, hit
		m.focus = hit.ID
		m.turnWebsitePage(hit.ID)
		return true, nil
	}
	to := m.overviewWebsiteIndex(hit.ID)
	d.neighbor, d.after = app.Website{}, false
	if to < 0 {
		return true, nil
	}
	d.neighbor = m.websites[to]
	from := slices.Index(m.websites, d.source)
	if from < 0 {
		m.cancelOverviewWebsiteDrag()
		return true, nil
	}
	d.after = to > from
	d.direction = m.websiteDragDirection(from, to)
	if release && to != from {
		// Saving changes which site will be under the pointer.
		m.hover = ""
		return true, m.relocateWebsite(d.source, d.neighbor, d.after)
	}
	return true, nil
}

func (m *Model) websiteDragDirection(from, to int) string {
	width := m.websiteCardInnerWidth()
	perPage := websitePager(width, m.websiteOffset, len(m.websites)).PerPage
	dx, dy := 0, cmp.Compare(to/perPage, from/perPage)
	if dy == 0 {
		cols := websiteColumns(width)
		from, to = from%perPage, to%perPage
		dx, dy = cmp.Compare(to%cols, from%cols), cmp.Compare(to/cols, from/cols)
	}
	arrows := [3][3]string{
		{"↖", "↑", "↗"},
		{"←", "", "→"},
		{"↙", "↓", "↘"},
	}
	return arrows[dy+1][dx+1]
}

func (m *Model) overviewWebsiteIndex(id string) int {
	for _, prefix := range []string{overviewWebsiteDragPrefix, overviewWebsiteDropPrefix, "website.site."} {
		if url, ok := strings.CutPrefix(id, prefix); ok {
			return slices.IndexFunc(m.websites, func(site app.Website) bool { return site.URL == url })
		}
	}
	return -1
}
