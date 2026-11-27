package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

type websiteListState struct {
	viewport   viewport.Model
	gridBounds hitRegion
	reveal     bool
}

func (m *Model) websiteInteractionsAvailable() bool {
	if m.form == "website-list" {
		return m.page == "overview" && !m.busy && !m.quitting
	}
	return m.pageSelectionMenuAvailable("overview")
}

func (m *Model) openWebsiteList() {
	if !m.pageSelectionMenuAvailable("overview") || len(m.websites) <= websiteCapacity(m.websiteCardInnerWidth()) {
		return
	}
	m.websiteList = &websiteListState{viewport: viewport.New()}
	m.form, m.focus, m.hover = "website-list", "website.back", ""
	m.tooltip.dismiss()
}

func (m *Model) normalizeWebsiteCardFocus() {
	capacity := websiteCapacity(m.websiteCardInnerWidth())
	if m.overviewWebsiteIndex(m.focus) >= capacity {
		m.focus = "website.viewAll"
	} else if m.focus == "website.viewAll" && len(m.websites) <= capacity {
		m.focus = "website.add"
	}
}

func (m *Model) websiteListAction(id string) tea.Cmd {
	if m.busy || m.quitting {
		return nil
	}
	if id == "website.back" {
		m.websiteList = nil
		m.form, m.focus, m.hover = "", "website.viewAll", ""
		m.normalizeWebsiteCardFocus()
		m.revealContentFocus()
		return nil
	}
	m.focus = id
	return m.testWebsites(id)
}

func (m *Model) websiteListKey(key string) tea.Cmd {
	if key == "esc" {
		return m.websiteListAction("website.back")
	}
	if m.busy || m.quitting || m.websiteMenuKey(key) {
		return nil
	}
	switch key {
	case "enter", " ":
		// Wheel scrolling may have taken the focused result out of view.
		if hasWebsiteTarget(m.targets, m.focus) {
			return m.websiteListAction(m.focus)
		}
	case "tab", "shift+tab":
		ids := make([]string, 0, len(m.websites)+2)
		for _, site := range m.websites {
			ids = append(ids, websiteActionID(site))
		}
		ids = append(ids, "website.back")
		if hasWebsiteTarget(m.targets, "website.all") {
			ids = append(ids, "website.all")
		}
		i := slices.Index(ids, m.focus)
		if key == "shift+tab" {
			i += len(ids) - 1
		} else {
			i++
		}
		m.focus = ids[(i+len(ids))%len(ids)]
		m.websiteList.reveal = true
	case "left", "right", "up", "down", "home", "end", "pgup", "pgdown":
		if len(m.websites) == 0 {
			return nil
		}
		i := m.overviewWebsiteIndex(m.focus)
		cols := websiteColumns(m.websiteListInnerWidth())
		page := max(1, m.websiteList.viewport.Height()/3) * cols
		switch key {
		case "left":
			i--
		case "right":
			i++
		case "up":
			i -= cols
		case "down":
			i += cols
		case "pgup":
			i -= page
		case "pgdown":
			i += page
		case "home":
			i = 0
		case "end":
			i = len(m.websites) - 1
		}
		m.focusWebsiteAt(max(0, i))
	}
	return nil
}

func hasWebsiteTarget(targets []hitRegion, id string) bool {
	return slices.ContainsFunc(targets, func(hit hitRegion) bool { return hit.ID == id })
}

func (m *Model) websiteListWheel(w tea.MouseWheelMsg) {
	list := m.websiteList
	if !regionsOverlap(list.gridBounds, hitRegion{X: w.X, Y: w.Y, W: 1, H: 1}) {
		return
	}
	step := 3
	switch w.Button {
	case tea.MouseWheelUp:
		step = -step
	case tea.MouseWheelDown:
	default:
		return
	}
	list.viewport.SetYOffset(list.viewport.YOffset() + step)
	m.hover = ""
	if m.websiteDrag.active() {
		m.websiteDrag.neighbor, m.websiteDrag.direction = app.Website{}, ""
	}
}

func (m *Model) websiteListInnerWidth() int { return min(82, m.width) - 2*modalPaddingX }

func (m *Model) renderWebsiteList(base string) (string, []hitRegion) {
	if m.width < 24 || m.height < 10 {
		return m.renderModalNotice(m.tr("profiles.enlarge"))
	}
	inner := m.websiteListInnerWidth()
	o := m.navigationOptions()
	o.Dim = false
	body := o.websiteGrid(m.websites, inner, modalBackground)
	if m.websiteDrag.direction != "" {
		for i, line := range body.lines {
			body.lines[i] = cellPad(line, inner)
		}
		o.paintWebsiteDropArrow(&body, inner, modalBackground)
	}
	allID := ""
	if o.websiteTestsAvailable() && (m.websiteTests.canTestAll(m.websites) || (m.websiteTests.batch && m.focus == "website.all")) {
		allID = "website.all"
	}
	footer := m.profileButtons([]profileButton{{id: "website.back", label: m.tr("dashboard.website.back")}}, inner, modalBackground)
	testAll := m.profileButtons([]profileButton{{id: allID, label: "⚡︎ " + m.tr("dashboard.website.all"), primary: true}}, inner, modalBackground)
	if gap := inner - ansi.StringWidth(footer.lines[0]) - ansi.StringWidth(testAll.lines[0]); gap >= 1 {
		footer = joinDashboardBlocks(gap, footer, testAll)
	} else {
		footer.append(testAll)
	}
	height := min(len(body.lines)+6+len(footer.lines), m.height)
	surface := m.newModalSurface(inner+2*modalPaddingX, height)
	surface.accessHeader(m.tr("dashboard.website.listTitle"), m.tr("dashboard.website.count", len(m.websites)), modalPaddingX)
	list := m.websiteList
	v := &list.viewport
	v.SetWidth(inner)
	v.SetHeight(height - 6 - len(footer.lines))
	v.SetContentLines(body.lines)
	if list.reveal {
		for _, hit := range body.targets {
			if hit.ID == m.focus {
				if hit.Y-1 < v.YOffset() {
					v.SetYOffset(hit.Y - 1)
				}
				if hit.Y >= v.YOffset()+v.Height() {
					v.SetYOffset(hit.Y - v.Height() + 1)
				}
				break
			}
		}
		list.reveal = false
	}
	const top = 4
	list.gridBounds = surface.screen(hitRegion{X: modalPaddingX, Y: top, W: inner, H: v.Height()})
	for i, line := range strings.Split(v.View(), "\n") {
		surface.put(modalPaddingX, top+i, cellCrop(line, inner))
	}
	var hits []hitRegion
	for _, hit := range body.targets {
		if clipped, ok := clipRegion(hit, 0, v.YOffset(), inner, v.Height()); ok {
			clipped.X += modalPaddingX
			clipped.Y += top - v.YOffset()
			hits = append(hits, surface.screen(clipped))
		}
	}
	footerY := height - 1 - len(footer.lines)
	surface.divider(footerY - 1)
	for i, line := range footer.lines {
		surface.put(modalPaddingX, footerY+i, line)
	}
	for _, hit := range footer.targets {
		hit.X += modalPaddingX
		hit.Y += footerY
		hits = append(hits, surface.screen(hit))
	}
	if !v.AtBottom() || !v.AtTop() {
		marker := "↕"
		if v.AtBottom() {
			marker = "↑"
		} else if v.AtTop() {
			marker = "↓"
		}
		surface.put(inner+modalPaddingX-ansi.StringWidth(marker), footerY-1, themed(marker, muted, modalBackground, false))
	}
	return surface.render(base), hits
}
