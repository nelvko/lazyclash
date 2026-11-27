package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func overviewWebsiteCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	inner := overviewInnerWidth(width)
	all := overviewControlButton{id: "website.all", label: "⚡︎"}
	all.enabled = o.websiteTestsAvailable() && len(o.websites) > 0 && (o.websiteTests.canTestAll(o.websites) || o.Focus == all.id)
	id := ""
	if all.enabled {
		id = all.id
	}
	title := overviewLabelSlot(o, inner, overviewHeading(o, tr("dashboard.website.title")), overviewButton(o, all, buttonIcon), id, true)
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
	content.blank()
	pageLabel := ""
	if page.TotalPages > 1 {
		pageLabel = themed(page.View(), muted, panel, o.Dim)
	}
	content.append(overviewLabelControl(o, inner, pageLabel, overviewControlButton{
		id: "website.manage", label: tr("dashboard.website.manage"), enabled: !o.Busy,
	}, buttonText))
	if page.TotalPages > 1 {
		content.append(overviewButtons(o, inner, []overviewControlButton{
			{id: "website.prev", label: tr("dashboard.website.prev"), enabled: !page.OnFirstPage()},
			{id: "website.next", label: tr("dashboard.website.next"), enabled: !page.OnLastPage()},
		}))
	}
	return overviewPanel(o, width, title, content, nil)
}

func (o dashboardOptions) websiteTestsAvailable() bool {
	return !o.StateUnavailable && !o.Busy && overviewProxyAvailable(o.Snapshot)
}

func (o dashboardOptions) websiteCell(site app.Website, width int) dashboardBlock {
	id := websiteActionID(site)
	opts := dashboardButtonOptions(id, o, false, panel)
	opts.Kind, opts.Compact = buttonText, true
	opts.Busy = o.websiteTests.rows[site.URL].pending()
	opts.Disabled = !o.websiteTestsAvailable() || opts.Busy && o.Focus != id
	// A compact text button reserves a focus marker and one space on each side.
	name := ansi.Truncate(clean(site.Name), max(1, width-3), "…")
	face := cellCrop(renderButton(name, opts), width)
	b := dashboardBlock{lines: []string{cellPad(face, width)}}
	if !opts.Disabled {
		b.targets = []hitRegion{{ID: id, W: ansi.StringWidth(face), H: 1}}
	}
	indent := min(2, max(0, width-12))
	for _, line := range o.websiteTestLines(site.URL, max(1, width-indent)) {
		b.lines = append(b.lines, strings.Repeat(" ", indent)+line)
	}
	return b
}

func (o dashboardOptions) websiteTestLines(site string, width int) []string {
	tr := o.Locale.T
	row := o.websiteTests.rows[site]
	color, status := muted, tr("dashboard.website.untested")
	switch {
	case o.StateUnavailable || !overviewProxyAvailable(o.Snapshot):
		status = tr("dashboard.status.unavailable")
	case row.queued:
		status = tr("dashboard.website.queued")
	case row.pending():
		color, status = accent, tr("dashboard.website.running")
	case row.result.StatusCode > 0:
		color, status = success, fmt.Sprintf("%d ms", max(1, row.result.Duration.Milliseconds()))
	case row.err != nil:
		color, status = warning, tr("dashboard.website.timeout")
	}
	return overviewTextBlock([]string{themed(status, color, panel, o.Dim)}, width).lines
}
