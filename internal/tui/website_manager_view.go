package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"github.com/nelvko/lazyclash/internal/app"
)

func (m *Model) renderSiteManager(base string) (string, []hitRegion) {
	width := min(72, m.width)
	inner := width - 2*modalPaddingX
	if width < 24 || m.height < 8 {
		return m.renderModalNotice(m.tr("profiles.enlarge"))
	}
	footer := m.profileButtons(m.siteManagerButtons(), inner, modalBackground)
	// Keep the header, buttons and a bottom padding row outside the viewport.
	available := m.height - 4 - len(footer.lines)
	if available < 1 {
		return m.renderModalNotice(m.tr("profiles.enlarge"))
	}
	var feedback []string
	message, messageColor := clean(m.siteManager.err), errorColor
	if m.busy && m.job == "website-move" {
		message, messageColor = m.tr("websiteEditor.saving"), muted
		if m.opCtx.Err() != nil {
			message = m.tr("model.canceling")
		}
	}
	if message != "" && available > 1 {
		feedback = strings.Split(wrapText(message, inner), "\n")
		limit := min(3, available-1)
		if len(feedback) > limit {
			feedback = feedback[:limit]
			feedback[limit-1] = cellCrop(feedback[limit-1], inner-1) + "…"
		}
	}
	available -= len(feedback)
	gap := 0
	if available >= 3 {
		gap = 1
	}
	count := len(m.websites)
	listHeight := min(max(1, count), 12, available-gap)
	selected := max(0, min(m.siteManager.selected, count-1))
	positionIndex := selected
	var rows []string
	for i, site := range m.websites {
		rows = append(rows, m.siteManagerRow(site, i == selected, inner))
		if site == m.siteManager.drag.neighbor {
			positionIndex = i
		}
	}
	if count == 0 {
		rows = []string{themed(m.tr("dashboard.website.empty"), muted, modalBackground, false)}
	}
	list := viewport.New(viewport.WithWidth(inner), viewport.WithHeight(listHeight))
	list.SetContentLines(rows)
	offset := m.siteManager.offset
	if !m.siteManager.drag.active() {
		offset = max(selected-listHeight+1, min(offset, selected))
	}
	list.SetYOffset(offset)
	m.siteManager.offset = list.YOffset()

	var body dashboardBlock
	body.lines = strings.Split(list.View(), "\n")
	for row := 0; row < listHeight && row+m.siteManager.offset < count; row++ {
		site := m.websites[row+m.siteManager.offset]
		if count > 1 {
			body.targets = append(body.targets, hitRegion{ID: siteDragGrip + site.URL, Y: row, W: 2, H: 1})
		}
		body.targets = append(body.targets, hitRegion{ID: siteManagerRowID + site.URL, X: 2, Y: row, W: inner - 2, H: 1})
	}
	if gap > 0 {
		body.blank()
	}
	for _, line := range feedback {
		body.lines = append(body.lines, themed(line, messageColor, modalBackground, false))
	}
	body.append(footer)
	position := ""
	if count > listHeight {
		position = fmt.Sprintf("%d/%d", positionIndex+1, count)
	}
	surface := m.newModalSurface(width, 4+len(body.lines))
	surface.accessHeader(m.tr("dashboard.website.manage"), position, modalPaddingX)
	for i, line := range body.lines {
		surface.put(modalPaddingX, 3+i, line)
	}
	if m.busy || m.quitting {
		return surface.render(base), nil
	}
	for i, hit := range body.targets {
		hit.X += modalPaddingX
		hit.Y += 3
		body.targets[i] = surface.screen(hit)
	}
	return surface.render(base), body.targets
}

func (m *Model) siteManagerRow(site app.Website, selected bool, width int) string {
	marker, markerColor, surface := "  ", muted, modalBackground
	grip, gripColor := "  ", muted
	if len(m.websites) > 1 {
		grip = "⠿ "
	}
	d := m.siteManager.drag
	if selected {
		marker, surface = "› ", selectionBackground
		if m.focus == "websiteManager.list" {
			markerColor = focus
		}
	} else if d.active() && site == d.neighbor {
		marker, markerColor, surface = "↑ ", focus, hoverBackground
		if d.after {
			marker = "↓ "
		}
	} else if m.hover == siteManagerRowID+site.URL {
		surface = hoverBackground
	}
	if m.hover == siteDragGrip+site.URL || d.source == site {
		gripColor = focus
	}
	nameWidth := width - 4
	if width >= 40 {
		nameWidth = min(28, width/3)
	}
	row := themed(grip, gripColor, surface, false) + themed(marker, markerColor, surface, false) + themed(cellPad(clean(site.Name), nameWidth), text, surface, false)
	if width >= 40 {
		row += themed("  "+cellPad(clean(site.URL), width-6-nameWidth), muted, surface, false)
	}
	return row
}
