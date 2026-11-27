package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

const (
	overviewWebsiteDragPrefix = "website.drag."
	overviewWebsiteDropPrefix = "website.drop."
)

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
	card := overviewPanel(o, width, title, content, nil)
	o.paintWebsiteDropArrow(&card, width)
	return card
}

// Paint in the existing gap on the incoming side, after wrapping and column
// layout have established the actual name and result positions.
func (o dashboardOptions) paintWebsiteDropArrow(card *dashboardBlock, width int) {
	drag := o.websiteDrag
	if drag.direction == "" {
		return
	}
	nameID, resultID := overviewWebsiteDragID(drag.neighbor), websiteActionID(drag.neighbor)
	var name hitRegion
	below, resultEnd := 0, width
	for _, hit := range card.targets {
		switch hit.ID {
		case nameID:
			name = hit
		case resultID:
			below = hit.Y + hit.H
			resultEnd = min(resultEnd, hit.X+hit.W)
		}
	}
	if name.W == 0 {
		return
	}
	mark := drag.direction
	drop := hitRegion{ID: overviewWebsiteDropPrefix + drag.neighbor.URL, Y: name.Y, W: 1, H: 1}
	diagonal := false
	switch mark {
	case "→":
		drop.X, drop.W, mark = name.X-2, 2, "→ "
	case "←":
		drop.X, drop.W, mark = name.X+name.W, 2, " ←"
	case "↓":
		drop.X, drop.Y = name.X+(name.W-1)/2, name.Y-1
	case "↑":
		// Keep a path from the name through even a short or wrapped result.
		drop.X, drop.Y = min(name.X+(name.W-1)/2, resultEnd-1), below
	case "↙", "↘", "↖", "↗":
		diagonal = true
		drop.Y = name.Y - 1
		if mark == "↖" || mark == "↗" {
			drop.Y = below
		}
		if mark == "↙" || mark == "↖" {
			drop.X = name.X + name.W
		} else {
			drop.X = name.X - 1
		}
	default:
		return
	}
	if drop.X < 0 || drop.X+drop.W > width || drop.Y < 0 || drop.Y >= len(card.lines) {
		return
	}
	line := card.lines[drop.Y]
	card.lines[drop.Y] = ansi.Cut(line, 0, drop.X) + themed(mark, focus, panel, o.Dim) + ansi.Cut(line, drop.X+drop.W, width)
	if diagonal {
		// A pointer can approach a corner horizontally or vertically first.
		// Keep the two columns straddling the name edge active up to the arrow.
		drop.X = min(drop.X, name.X+name.W-1)
		drop.W = 2
		top := min(name.Y, drop.Y)
		drop.H, drop.Y = max(name.Y, drop.Y)-top+1, top
	}
	card.targets = append(card.targets, drop)
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
	// The two rows share the compact button's focus gutter and left padding.
	inset := min(2, max(0, width-1))
	surface, style := panel, lipgloss.NewStyle()
	marker := ""
	switch {
	case o.websiteDrag.source == site:
		surface, style = selectionBackground, style.Bold(true)
		marker = themed("⠿", accent, panel, o.Dim)
	case o.websiteDrag.neighbor == site:
		surface, style = hoverBackground, style.Underline(true)
	case !o.websiteDrag.active() && !o.Busy && o.Hover == dragID && len(o.websites) > 1:
		marker = themed("⠿", muted, panel, o.Dim)
	}
	name := ansi.Truncate(clean(site.Name), max(1, width-inset), "…")
	face := cellPad(marker, inset) + themed(style.Render(name), text, surface, o.Dim)
	b := dashboardBlock{lines: []string{cellPad(face, width)}}
	if nameWidth := ansi.StringWidth(name); !o.Busy && nameWidth > 0 {
		hit := hitRegion{ID: dragID, X: inset, W: nameWidth, H: 1}
		if marker != "" {
			hit.X, hit.W = 0, inset+nameWidth
		}
		b.targets = append(b.targets, hit)
	}

	id := websiteActionID(site)
	opts := dashboardButtonOptions(id, o, false, panel)
	opts.Kind, opts.Compact = buttonText, true
	opts.TextHoverBackground = hoverBackground
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
