package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/rivo/uniseg"
)

const (
	minNotificationWidth = 26
	maxNotificationWidth = 52
)

type notificationOverlay struct {
	Content string
	Bounds  hitRegion
	Targets []hitRegion
	IDs     []uint64
}

func (card *notificationOverlay) position(x, y int) {
	card.Bounds.X, card.Bounds.Y = x, y
	for i := range card.Targets {
		card.Targets[i].X += x
		card.Targets[i].Y += y
	}
}

func notificationLabel(locale i18n.Localizer, kind, spinner string) string {
	if kind == "working" {
		return strings.TrimSpace(locale.T("notification.working", clean(spinner)))
	}
	return locale.T("notification." + kind)
}

func (n *notificationState) covers(x, y int) bool {
	point := hitRegion{X: x, Y: y, W: 1, H: 1}
	for _, card := range n.overlays {
		if regionsOverlap(point, card.Bounds) {
			return true
		}
	}
	return false
}

func notificationLayout(o dashboardOptions, headerActions ...hitRegion) []notificationOverlay {
	if o.Dim || o.SidebarOpen || len(o.Notifications) == 0 || o.Width < 20 || o.Height < 7 {
		return nil
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	items := make([]notificationItem, 0, len(o.Notifications))
	modeFeedback, hasError := false, false
	for _, item := range o.Notifications {
		if !item.Silent && (item.showAfter.IsZero() || !now.Before(item.showAfter)) {
			items = append(items, item)
			hasError = hasError || item.Kind == "error"
			if o.Page == "proxies" && item.Page == "proxies" && strings.HasPrefix(item.Key, "mode.") {
				modeFeedback = true
			}
		}
	}
	o.Notifications = items
	if len(items) == 0 {
		return nil
	}
	g := dashboardGeometry(o)
	compact := o.Width < 100 || o.Height < 24
	right := 2
	if g.sidebar {
		right = 3
	}
	rightEdge := o.Width - right
	// Right-side header actions may be covered, but the title, compact
	// navigation and core status must remain readable.
	page := "▎ " + pageTitle(o.Page, o.Locale)
	titleX, title := dashboardHeaderTitle(o, g, page)
	title = dashboardTitle(o, title, page, g.headerX+g.headerWidth-titleX)
	cornerWidth := min(maxNotificationWidth, rightEdge-titleX-ansi.StringWidth(title)-2)
	width := cornerWidth
	y := 0
	capacity := 2
	if compact {
		width = min(maxNotificationWidth, rightEdge)
		y, capacity = g.headerY+1, 1
	}
	x := rightEdge - width
	bottom := min(dashboardContentBottom(o), max(g.y, o.Height/2))
	inputTargets := notificationInputs(o, g)
	if modeFeedback {
		// Route changes can be retried from the fixed controls, including
		// when they wrap on a small terminal. Keep that whole strip exposed.
		y = g.y
		for _, input := range inputTargets {
			if input.Y == y {
				y += input.H
			}
		}
		if y >= dashboardContentBottom(o) {
			// Only the short header's separating row is free. A single
			// overflow entry keeps the notification list accessible.
			y, bottom = g.headerHeight, g.headerHeight+1
		}
	}
	for _, input := range inputTargets {
		if input.X < x+width && input.X+input.W > x && input.Y >= y {
			bottom = min(bottom, input.Y)
		}
	}
	singleResult := len(items) == 1 && items[0].Kind != "working"
	// A single padded result is three rows tall. It can occupy the full
	// header beside the title even when an input starts immediately below.
	paddedResultFits := singleResult && !compact && bottom-y >= 3
	if g.headerY > 0 && bottom == g.contentY && !modeFeedback && !paddedResultFits && (cornerWidth >= 16 || singleResult) {
		// A single compact result fits entirely above the title. A progress
		// action or overflow row must stay to the right of the title below.
		width = cornerWidth
		if singleResult {
			width = min(maxNotificationWidth, rightEdge)
			if width > cornerWidth {
				// This full-width card can only use the padding above the title.
				bottom = g.headerY
			}
		}
		x, y, capacity, compact = rightEdge-width, 0, 1, true
	}
	// A failed operation can be retried from a page header action. Keep the
	// actual button cells available even when the error outlives this page.
	if hasError {
		for _, action := range headerActions {
			if action.X < x+width && action.X+action.W > x && action.Y >= y && action.Y < bottom {
				y = max(y, action.Y+action.H)
			}
		}
	}
	var chosen []notificationOverlay
	for count := min(capacity, len(o.Notifications)); count > 0; count-- {
		start := len(o.Notifications) - count
		cursor := y
		var overlays []notificationOverlay
		fits := true
		limit := bottom
		// Reserve overflow after applying input bounds, so the entry point
		// cannot occupy the first row of an editable field.
		if start > 0 {
			limit--
		}
		for _, card := range notificationStack(items[start:], o, width, limit-y, compact) {
			card.position(rightEdge-card.Bounds.W, cursor)
			if cursor+card.Bounds.H > limit {
				fits = false
				break
			}
			for _, input := range inputTargets {
				if regionsOverlap(card.Bounds, input) {
					fits = false
					break
				}
			}
			if !fits {
				break
			}
			overlays = append(overlays, card)
			cursor += card.Bounds.H + 1
		}
		if fits {
			chosen = overlays
			break
		}
	}
	remaining := len(o.Notifications) - len(chosen)
	if remaining > 0 {
		row := y
		if len(chosen) > 0 {
			last := chosen[len(chosen)-1].Bounds
			row = last.Y + last.H
		}
		if len(chosen) == 0 && row >= bottom {
			// Header padding (or the separator below a short header) is
			// available even when its actions and the input consume all space.
			row = 0
			if o.Height < 24 {
				row = g.contentY - 1
			}
		}
		var overflow notificationOverlay
		if singleResult {
			overflow = notificationCard(items[0], o, width, 1, true)
		} else {
			const action = "notification.expand"
			label := notificationButton(action, notificationOverflowCaption(items[:remaining], width, o), buttonText, width, o)
			overflow = notificationOverlay{
				Content: label,
				Bounds:  hitRegion{ID: "notification", W: ansi.StringWidth(label), H: 1},
				Targets: []hitRegion{{ID: action, W: ansi.StringWidth(label), H: 1}},
			}
		}
		overflow.position(rightEdge-overflow.Bounds.W, row)
		chosen = append(chosen, overflow)
	}
	return chosen
}

func notificationStack(items []notificationItem, o dashboardOptions, width, height int, compact bool) []notificationOverlay {
	room := height - (len(items) - 1) // Keep a separator between cards.
	cards := make([]notificationOverlay, len(items))
	for i, item := range items {
		cards[i] = notificationCard(item, o, width, room, compact)
	}
	if len(cards) == 2 && cards[0].Bounds.H+cards[1].Bounds.H > room {
		// The first action shares the first body row, after any top padding.
		// Give both bodies room before spending spare rows on vertical padding.
		firstRows := cards[0].Bounds.H - 2*cards[0].Targets[0].Y
		secondRows := cards[1].Bounds.H - 2*cards[1].Targets[0].Y
		firstBudget := max(room-secondRows, min(firstRows, room/2))
		cards[0] = notificationCard(items[0], o, width, firstBudget, compact)
		cards[1] = notificationCard(items[1], o, width, room-cards[0].Bounds.H, compact)
	}
	return cards
}

// Surface the most important unresolved result alongside the overflow count.
func notificationOverflowCaption(items []notificationItem, width int, o dashboardOptions) string {
	if len(o.Notifications) == 1 {
		// The task and its Cancel button expand together from a one-row slot.
		first, _, _ := strings.Cut(notificationBody(items[0], o.Locale, o.Spinner), "\n")
		return first + " ›"
	}
	item := items[len(items)-1]
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Kind == "error" {
			item = items[i]
			break
		}
		if items[i].Kind == "warning" && item.Kind != "warning" {
			item = items[i]
		}
	}
	kind := notificationLabel(o.Locale, item.Kind, "")
	available := width - 4 // Button focus and padding.
	caption := kind + " · " + o.Locale.T("notification.more", len(items))
	if ansi.StringWidth(caption) > available {
		return fmt.Sprintf("%s ×%d", kind, len(items))
	}
	return caption
}

func notificationInputs(o dashboardOptions, g dashboardLayout) []hitRegion {
	var targets []hitRegion
	total := len(strings.Split(o.Body, "\n"))
	if o.BodyTotal > 0 {
		total = o.BodyTotal
	}
	scroll := max(0, min(o.Scroll, max(0, total-g.height)))
	for _, target := range o.BodyTop.targets {
		switch target.ID {
		case "proxies.search", "conn.search", "rules.search", "providers.search", "logs.search":
			target.X += g.x
			target.Y += g.contentY
			targets = append(targets, target)
		}
	}
	for _, target := range o.BodyTargets {
		switch target.ID {
		case "profiles.url", "proxies.search", "conn.search", "rules.search", "providers.search", "logs.search":
			target.X += g.x
			target.Y += g.y - scroll
			if target, ok := clipRegion(target, g.x, g.y, g.width, g.height); ok {
				targets = append(targets, target)
			}
		}
	}
	return targets
}

func notificationButton(id, label string, kind buttonKind, width int, o dashboardOptions) string {
	return noticePaint{background: raised, foreground: accent}.button(id, label, kind, width, o)
}

func notificationCard(item notificationItem, o dashboardOptions, maxWidth, maxHeight int, compact bool) notificationOverlay {
	return notificationCardWithControls(item, o, maxWidth, maxHeight, compact,
		fmt.Sprintf("notification.close.%d", item.ID), fmt.Sprintf("notification.details.%d", item.ID))
}

// Wrap at Unicode line breaks (including breaks between CJK characters).
// Only the first row shares its width with the controls.
func notificationLines(body string, firstWidth, width int) []string {
	var lines []string
	limit := firstWidth
	for paragraph := range strings.SplitSeq(body, "\n") {
		for {
			cut, cells, lastBreak := len(paragraph), 0, 0
			graphemes := uniseg.NewGraphemes(paragraph)
			for graphemes.Next() {
				start, end := graphemes.Positions()
				cluster := graphemes.Str()
				cells += ansi.StringWidth(cluster)
				if cells > limit {
					cut = start // A word or identifier may need a hard wrap.
					if lastBreak > 0 && cluster != " " {
						cut = lastBreak
					}
					if cut == 0 && limit == width {
						cut = end // Always advance, even at a single-cell width.
					}
					break
				}
				if graphemes.LineBreak() != uniseg.LineDontBreak {
					lastBreak = end
				}
			}
			lines = append(lines, strings.TrimRight(paragraph[:cut], " "))
			paragraph = strings.TrimLeft(paragraph[cut:], " ")
			limit = width
			if paragraph == "" {
				break
			}
		}
	}
	return lines
}

func notificationCardWithControls(item notificationItem, o dashboardOptions, maxWidth, maxHeight int, compact bool, closeID, detailsID string) notificationOverlay {
	maxWidth = min(maxNotificationWidth, maxWidth)
	paint := notificationPaint(item.Kind)
	leading, trailing := 2, 2
	if compact {
		// The close button already includes one cell of trailing padding.
		leading, trailing = 1, 0
	}
	if maxWidth < minNotificationWidth {
		leading, trailing = 0, 0
	}
	prefix := ""
	if paint.marker != "" {
		prefix = themed(paint.marker+" ", paint.signal, paint.background, false)
	}
	beforeBody := leading + ansi.StringWidth(prefix)
	close := paint.button(closeID, "×", buttonIcon, maxWidth, o)
	closeWidth := ansi.StringWidth(close)
	body := notificationBody(item, o.Locale, o.Spinner)
	if extra := notificationExtra(item); extra != "" {
		body += "\n" + extra
	}
	cancelID := fmt.Sprintf("notification.cancel.%d", item.ID)
	cancel := ""
	bodyRows := maxHeight
	if item.Kind == "working" && o.Busy {
		cancel = paint.button(cancelID, o.Locale.T("notification.cancel"), buttonText, maxWidth-leading-trailing, o)
		bodyRows--
	}
	bodyRows = max(1, bodyRows)
	first, _, _ := strings.Cut(body, "\n")
	desiredFirst := beforeBody + ansi.StringWidth(first) + closeWidth + trailing
	width := min(maxWidth, max(minNotificationWidth, desiredFirst, beforeBody+lipgloss.Width(body)+trailing, leading+ansi.StringWidth(cancel)+trailing))
	bodyWidth := width - beforeBody - trailing
	firstWidth := bodyWidth - closeWidth
	// Decide whether to fold at the full available width, before reserving
	// cells for Details. A shorter card already fits every explicit line.
	maxBodyWidth := maxWidth - beforeBody - trailing
	preview := notificationLines(body, maxBodyWidth-closeWidth, maxBodyWidth)
	truncated := len(preview) > bodyRows
	details := ""
	if truncated {
		details = paint.button(detailsID, o.Locale.T("notification.details"), buttonText, maxWidth, o)
		if ansi.StringWidth(details) > (maxBodyWidth-closeWidth)/2 {
			details = paint.button(detailsID, "⋯", buttonIcon, maxWidth, o)
		}
		width = min(maxWidth, max(width, desiredFirst+ansi.StringWidth(details)))
		bodyWidth = width - beforeBody - trailing
		firstWidth = bodyWidth - closeWidth - ansi.StringWidth(details)
		preview = notificationLines(body, firstWidth, bodyWidth)
		truncated = len(preview) > bodyRows
		if truncated {
			preview = preview[:bodyRows]
			lastWidth := bodyWidth
			if bodyRows == 1 {
				lastWidth = firstWidth
			}
			preview[bodyRows-1] = cellCrop(strings.TrimRight(preview[bodyRows-1], " ")+"…", lastWidth)
		} else {
			details = ""
			firstWidth = bodyWidth - closeWidth
		}
	}
	var lines []string
	var targets []hitRegion
	fill := func(row string) string {
		return themed(cellPad(row, width), paint.foreground, paint.background, false)
	}
	padded := !compact && len(preview)+2 <= bodyRows
	if padded {
		lines = append(lines, fill(""))
	}
	row := strings.Repeat(" ", leading) + prefix + cellPad(preview[0], firstWidth)
	if details != "" {
		targets = append(targets, hitRegion{ID: detailsID, X: beforeBody + firstWidth, Y: len(lines), W: ansi.StringWidth(details), H: 1})
		row += details
	}
	targets = append(targets, hitRegion{ID: closeID, X: width - trailing - closeWidth, Y: len(lines), W: closeWidth, H: 1})
	lines = append(lines, fill(row+close))
	for _, line := range preview[1:] {
		lines = append(lines, fill(strings.Repeat(" ", beforeBody)+line))
	}
	if cancel != "" {
		targets = append(targets, hitRegion{ID: cancelID, X: leading, Y: len(lines), W: ansi.StringWidth(cancel), H: 1})
		lines = append(lines, fill(strings.Repeat(" ", leading)+cancel))
	}
	if padded {
		lines = append(lines, fill(""))
	}
	card := notificationOverlay{
		Content: strings.Join(lines, "\n"),
		Bounds:  hitRegion{ID: "notification", W: width, H: len(lines)},
		Targets: targets,
	}
	if !truncated {
		card.IDs = []uint64{item.ID}
	}
	return card
}
