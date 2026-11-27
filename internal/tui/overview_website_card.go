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
	inner := overviewInnerWidth(width)
	visible := o.websites[:min(len(o.websites), websiteCapacity(inner))]
	content := o.websiteGrid(visible, inner, panel)
	for len(content.lines) < 5 {
		content.blank()
	}
	card := overviewPanel(o, width, overviewWebsiteHeading(o, inner), content, nil)
	o.paintWebsiteDropArrow(&card, width, panel)
	return card
}

func (o dashboardOptions) websiteGrid(sites []app.Website, inner int, surface string) dashboardBlock {
	var content dashboardBlock
	cols := websiteColumns(inner)
	cellWidth := (inner - 2*(cols-1)) / cols
	for row := range slices.Chunk(sites, cols) {
		if len(content.lines) > 0 {
			content.blank()
		}
		var cells []dashboardBlock
		for _, site := range row {
			cells = append(cells, o.websiteCell(site, cellWidth, surface))
		}
		content.append(joinDashboardBlocks(2, cells...))
	}
	if len(sites) == 0 {
		content.append(overviewTextBlock([]string{themed(o.Locale.T("dashboard.website.empty"), muted, surface, o.Dim)}, inner))
	}
	return content
}

// Paint in the existing gap on the incoming side, after wrapping and column
// layout have established the actual name and result positions.
func (o dashboardOptions) paintWebsiteDropArrow(card *dashboardBlock, width int, surface string) {
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
	card.lines[drop.Y] = ansi.Cut(line, 0, drop.X) + themed(mark, focus, surface, o.Dim) + ansi.Cut(line, drop.X+drop.W, width)
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
	// Reserve the overflow action's space even when hidden. Crossing the
	// two-row limit must not move the neighboring cards on compact terminals.
	more := overviewControlButton{id: "website.viewAll", label: o.Locale.T("dashboard.website.viewAll"), enabled: !o.Busy}
	overflow := len(o.websites) > websiteCapacity(width)
	moreFace := overviewButton(o, more, buttonIcon)
	addFace, allFace := overviewButton(o, add, buttonIcon), overviewButton(o, all, buttonIcon)
	counted := more
	counted.label = fmt.Sprintf("%s %d", more.label, len(o.websites))
	if face := overviewButton(o, counted, buttonIcon); ansi.StringWidth(title+face+addFace+allFace)+2 <= width {
		moreFace = face
	}
	if !overflow {
		moreFace = strings.Repeat(" ", ansi.StringWidth(moreFace))
		more.enabled = false
	}

	// Group actions from the right so Add and Test all stay adjacent when wrapping.
	type slot struct {
		control overviewControlButton
		face    string
	}
	slots := []slot{{more, moreFace}, {add, addFace}, {all, allFace}}
	var rows []dashboardBlock
	for end := len(slots); end > 0; {
		start := end - 1
		face := cellCrop(slots[start].face, width)
		for start > 0 && ansi.StringWidth(slots[start-1].face+face) <= width {
			start--
			face = slots[start].face + face
		}
		x := width - ansi.StringWidth(face)
		row := dashboardBlock{lines: []string{strings.Repeat(" ", x) + face}}
		if start == 0 {
			row = overviewLabelSlot(o, width, title, face, "", true)
		}
		for _, action := range slots[start:end] {
			w := min(width, ansi.StringWidth(action.face))
			if action.control.enabled {
				row.targets = append(row.targets, hitRegion{ID: action.control.id, X: x, Y: len(row.lines) - 1, W: w, H: 1})
			}
			x += w
		}
		rows = append(rows, row)
		end = start
	}
	var b dashboardBlock
	for _, row := range slices.Backward(rows) {
		b.append(row)
	}
	return b
}

func (o dashboardOptions) websiteTestsAvailable() bool {
	return !o.StateUnavailable && !o.Busy && overviewProxyAvailable(o.Snapshot)
}

func (o dashboardOptions) websiteCell(site app.Website, width int, background string) dashboardBlock {
	dragID := overviewWebsiteDragID(site)
	// The two rows share the compact button's focus gutter and left padding.
	inset := min(2, max(0, width-1))
	surface, style := background, lipgloss.NewStyle()
	marker := ""
	switch {
	case o.websiteDrag.source == site:
		surface, style = selectionBackground, style.Bold(true)
		marker = themed("⠿", accent, background, o.Dim)
	case o.websiteDrag.neighbor == site:
		surface, style = hoverBackground, style.Underline(true)
	case !o.websiteDrag.active() && !o.Busy && o.Hover == dragID && len(o.websites) > 1:
		marker = themed("⠿", muted, background, o.Dim)
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
	opts := dashboardButtonOptions(id, o, false, background)
	opts.Kind, opts.Compact = buttonText, true
	opts.TextHoverBackground = hoverBackground
	opts.Busy = o.websiteTests.rows[site.URL].pending()
	canTest := o.websiteTestsAvailable() && !opts.Busy
	opts.Hovered = opts.Hovered && canTest
	opts.Disabled = !canTest && !opts.Focused
	status, color := o.websiteTestStatus(site.URL)
	opts.TextColor = color
	value := ansi.Truncate(renderButton(ansi.Truncate(status, max(1, width-2), "…"), opts), width, "")
	if !o.Busy {
		b.targets = append(b.targets, hitRegion{ID: id, Y: 1, W: ansi.StringWidth(value), H: 1})
	}
	b.lines = append(b.lines, value)
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
		ms := max(1, row.result.Duration.Milliseconds())
		color := success
		if ms >= 500 {
			color = websiteSlowLatency
		}
		return fmt.Sprintf("%d ms", ms), color
	case row.err != nil:
		return tr("dashboard.website.timeout"), errorColor
	default:
		return tr("dashboard.website.test"), accent
	}
}
