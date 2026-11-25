package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
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

func notificationColor(kind string) string {
	switch kind {
	case "error":
		return errorColor
	case "warning":
		return warning
	case "done":
		return success
	default:
		return info
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
	cornerWidth := min(54, rightEdge-titleX-ansi.StringWidth(title)-2)
	width := cornerWidth
	y := 0
	capacity := 2
	if compact {
		width = rightEdge - 2
		y, capacity = g.headerY+1, 1
	}
	x := rightEdge - width
	bottom := min(dashboardContentBottom(o), max(g.y, min(15, o.Height/2)))
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
	if g.headerY > 0 && bottom == g.contentY && !modeFeedback && (cornerWidth >= 16 || singleResult) {
		// A single compact result fits entirely above the title. A progress
		// action or overflow row must stay to the right of the title below.
		width = cornerWidth
		if singleResult {
			width = min(54, rightEdge-2)
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
		for _, item := range o.Notifications[start:] {
			card := notificationCard(item, o, width, compact)
			card.position(x, cursor)
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
			cursor += card.Bounds.H
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
			overflow = notificationCard(items[0], o, width, true)
		} else {
			const action = "notification.expand"
			label := notificationButton(action, notificationOverflowCaption(items[:remaining], width, o), width, o)
			overflow = notificationOverlay{
				Content: themed(cellPad(label, width), text, raised, false),
				Bounds:  hitRegion{ID: "notification", W: width, H: 1},
				Targets: []hitRegion{{ID: action, W: ansi.StringWidth(label), H: 1}},
			}
		}
		overflow.position(x, row)
		chosen = append(chosen, overflow)
	}
	return chosen
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

func notificationButton(id, label string, width int, o dashboardOptions) string {
	label = strings.Trim(label, "[] ")
	return cellCrop(renderButton(cellCrop(label, max(1, width-4)), buttonOptions{
		Kind: buttonText, Surface: raised, Focused: o.Focus == id, Hovered: o.Hover == id, Plain: o.PlainButtons,
	}), width)
}

func notificationCard(item notificationItem, o dashboardOptions, width int, compact bool) notificationOverlay {
	color := notificationColor(item.Kind)
	padding := 2
	if compact {
		padding = 0
	}
	inner := max(1, width-2*padding)
	closeID := fmt.Sprintf("notification.close.%d", item.ID)
	detailsID := fmt.Sprintf("notification.details.%d", item.ID)
	closeLabel := notificationButton(closeID, "×", inner, o)
	detailsLabel := notificationButton(detailsID, o.Locale.T("notification.details"), inner, o)
	body := notificationBody(item, o.Locale, o.Spinner)
	hasExtra := notificationExtra(item) != ""
	closeWidth := ansi.StringWidth(closeLabel)
	bodyWidth := inner - closeWidth
	var preview []string
	var truncated bool
	if compact {
		first, _, more := strings.Cut(body, "\n")
		truncated = more || ansi.StringWidth(first) > bodyWidth
		if truncated || hasExtra {
			// Keep at least half the row for the message on very narrow screens.
			if ansi.StringWidth(detailsLabel) > bodyWidth/2 {
				detailsLabel = notificationButton(detailsID, "⋯", inner, o)
			}
			bodyWidth -= ansi.StringWidth(detailsLabel)
			truncated = more || ansi.StringWidth(first) > bodyWidth
		}
		preview = []string{first}
	} else {
		preview = strings.Split(wrapText(body, bodyWidth), "\n")
		if len(preview) > 2 {
			preview, truncated = preview[:2], true
		}
	}
	if truncated {
		last := len(preview) - 1
		preview[last] = cellCrop(preview[last]+"…", bodyWidth)
	}
	showDetails := truncated || hasExtra
	var lines []string
	var targets []hitRegion
	if !compact {
		lines = append(lines, themed("╭"+strings.Repeat("─", width-2)+"╮", color, raised, false))
	}
	wrap := func(value string) string {
		if compact {
			return themed(cellPad(value, width), color, raised, false)
		}
		return themed("│ ", color, raised, false) + themed(cellPad(value, inner), text, raised, false) + themed(" │", color, raised, false)
	}
	for i, line := range preview {
		value := cellPad(line, bodyWidth)
		if i == 0 {
			if compact && showDetails {
				targets = append(targets, hitRegion{ID: detailsID, X: padding + bodyWidth, Y: len(lines), W: ansi.StringWidth(detailsLabel), H: 1})
				value += detailsLabel
			}
			targets = append(targets, hitRegion{ID: closeID, X: padding + inner - closeWidth, Y: len(lines), W: closeWidth, H: 1})
			value += closeLabel
		}
		lines = append(lines, wrap(value))
	}
	if item.Kind == "working" && o.Busy {
		id := fmt.Sprintf("notification.cancel.%d", item.ID)
		label := notificationButton(id, o.Locale.T("notification.cancel"), inner, o)
		targets = append(targets, hitRegion{ID: id, X: padding, Y: len(lines), W: ansi.StringWidth(label), H: 1})
		lines = append(lines, wrap(label))
	}
	if !compact {
		edge := "╰" + strings.Repeat("─", width-2) + "╯"
		if showDetails {
			x := width - ansi.StringWidth(detailsLabel) - 2
			edge = ansi.Cut(edge, 0, x) + detailsLabel + "─╯"
			targets = append(targets, hitRegion{ID: detailsID, X: x, Y: len(lines), W: ansi.StringWidth(detailsLabel), H: 1})
		}
		lines = append(lines, themed(edge, color, raised, false))
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
