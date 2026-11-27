package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

const overviewWebsiteDragPrefix = "website.drag."

func overviewWebsiteDragID(site app.Website) string { return overviewWebsiteDragPrefix + site.URL }

func overviewWebsiteCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	inner := overviewInnerWidth(width)
	title := overviewWebsiteHeading(o, inner)
	var content dashboardBlock
	page := websitePager(inner, o.websiteOffset, len(o.websites))
	start, end := page.GetSliceBounds(len(o.websites))
	visible := o.websites[start:end]
	cols := websiteColumns(inner)
	cellWidth := (inner - 2*(cols-1)) / cols
	for sites := range slices.Chunk(visible, cols) {
		if len(content.lines) > 0 {
			content.blank()
		}
		var cells []dashboardBlock
		for _, site := range sites {
			cells = append(cells, o.websiteCell(site, cellWidth))
		}
		content.append(joinDashboardBlocks(2, cells...))
	}
	if len(visible) == 0 {
		content.append(overviewTextBlock([]string{themed(tr("dashboard.website.empty"), muted, panel, o.Dim)}, inner))
	}
	if page.TotalPages > 1 {
		content.blank()
		content.append(overviewTextBlock([]string{themed(page.View(), muted, panel, o.Dim)}, inner))
		content.append(overviewButtons(o, inner, []overviewControlButton{
			{id: "website.prev", label: tr("dashboard.website.prev"), enabled: !page.OnFirstPage()},
			{id: "website.next", label: tr("dashboard.website.next"), enabled: !page.OnLastPage()},
		}))
	}
	return overviewPanel(o, width, title, content, nil)
}

func overviewWebsiteHeading(o dashboardOptions, width int) dashboardBlock {
	title := overviewHeading(o, o.Locale.T("dashboard.website.title"))
	add := overviewControlButton{id: "website.add", label: "＋", enabled: !o.Busy}
	all := overviewControlButton{id: "website.all", label: "⚡︎"}
	all.enabled = o.websiteTestsAvailable() && len(o.websites) > 0 && (o.websiteTests.canTestAll(o.websites) || o.Focus == all.id)
	left, right := overviewButton(o, add, buttonIcon), overviewButton(o, all, buttonIcon)
	leftW, rightW := ansi.StringWidth(left), ansi.StringWidth(right)
	if leftW+rightW > width {
		b := overviewLabelControl(o, width, title, add, buttonIcon)
		b.append(overviewLabelControl(o, width, "", all, buttonIcon))
		return b
	}
	b := overviewLabelSlot(o, width, title, left+right, "", true)
	y := len(b.lines) - 1
	if add.enabled {
		b.targets = append(b.targets, hitRegion{ID: add.id, X: width - leftW - rightW, Y: y, W: leftW, H: 1})
	}
	if all.enabled {
		b.targets = append(b.targets, hitRegion{ID: all.id, X: width - rightW, Y: y, W: rightW, H: 1})
	}
	return b
}

func (o dashboardOptions) websiteTestsAvailable() bool {
	return !o.StateUnavailable && !o.Busy && overviewProxyAvailable(o.Snapshot)
}

func (o dashboardOptions) websiteCell(site app.Website, width int) dashboardBlock {
	dragID := overviewWebsiteDragID(site)
	surface, style := panel, lipgloss.NewStyle()
	switch {
	case o.websiteDrag.source == site:
		surface, style = selectionBackground, style.Bold(true)
	case o.websiteDrag.neighbor == site:
		surface, style = hoverBackground, style.Underline(true)
	case o.Hover == dragID:
		surface = hoverBackground
	}
	// The two rows share the compact button's focus gutter and left padding.
	inset := min(2, max(0, width-1))
	name := ansi.Truncate(clean(site.Name), max(1, width-inset), "…")
	face := strings.Repeat(" ", inset) + themed(style.Render(name), text, surface, o.Dim)
	b := dashboardBlock{lines: []string{cellPad(face, width)}}
	if !o.Busy && ansi.StringWidth(name) > 0 {
		b.targets = append(b.targets, hitRegion{ID: dragID, X: inset, W: ansi.StringWidth(name), H: 1})
	}

	id := websiteActionID(site)
	opts := dashboardButtonOptions(id, o, false, panel)
	opts.Kind, opts.Compact = buttonText, true
	opts.Busy = o.websiteTests.rows[site.URL].pending()
	canTest := o.websiteTestsAvailable() && !opts.Busy
	opts.Hovered = opts.Hovered && canTest
	opts.Disabled = !canTest && !opts.Focused
	status, color := o.websiteTestStatus(site.URL)
	opts.TextColor = color
	for _, line := range strings.Split(wrapText(status, max(1, width-2)), "\n") {
		// At the narrowest width only the button's trailing padding is clipped.
		value := ansi.Truncate(renderButton(line, opts), width, "")
		if !o.Busy {
			b.targets = append(b.targets, hitRegion{ID: id, Y: len(b.lines), W: ansi.StringWidth(value), H: 1})
		}
		b.lines = append(b.lines, value)
	}
	return b
}

func (o dashboardOptions) websiteTestStatus(site string) (string, string) {
	tr := o.Locale.T
	row := o.websiteTests.rows[site]
	switch {
	case o.StateUnavailable || !overviewProxyAvailable(o.Snapshot):
		return tr("dashboard.status.unavailable"), muted
	case row.queued:
		return tr("dashboard.website.queued"), muted
	case row.pending():
		return tr("dashboard.website.running"), accent
	case row.result.StatusCode > 0:
		return fmt.Sprintf("%d ms", max(1, row.result.Duration.Milliseconds())), success
	case row.err != nil:
		return tr("dashboard.website.timeout"), warning
	default:
		return tr("dashboard.website.test"), accent
	}
}
