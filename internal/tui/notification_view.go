package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

type notificationOverlay struct {
	Content string
	Bounds  hitRegion
	Targets []hitRegion
	IDs     []uint64
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

func notificationHeading(item notificationItem, locale i18n.Localizer, spinner string) string {
	parts := []string{notificationLabel(locale, item.Kind, spinner)}
	if item.Page != "" {
		parts = append(parts, pageTitle(item.Page, locale))
	}
	if item.Context != "" {
		parts = append(parts, clean(item.Context))
	}
	return strings.Join(parts, " · ")
}

// Preserve diagnostic paragraphs while excluding terminal control sequences.
func notificationText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(value))
}

func notificationLayout(o dashboardOptions) []notificationOverlay {
	if o.Dim || o.SidebarOpen || len(o.Notifications) == 0 || o.Width < 20 || o.Height < 7 {
		return nil
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	items := make([]notificationItem, 0, len(o.Notifications))
	modeFeedback := false
	for _, item := range o.Notifications {
		if !item.Silent && (item.showAfter.IsZero() || !now.Before(item.showAfter)) {
			items = append(items, item)
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
	if g.headerY > 0 && bottom == g.contentY && !modeFeedback && cornerWidth >= 16 {
		// An input directly below the three-row header leaves no room for
		// a bordered card. The existing compact controls fit from 16 cells.
		width, x = cornerWidth, rightEdge-cornerWidth
		y, capacity, compact = 0, 1, true
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
			card.Bounds.X, card.Bounds.Y = x, cursor
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
			for i := range card.Targets {
				card.Targets[i].X += x
				card.Targets[i].Y += cursor
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
		// Header space always retains an entry point for an oversized message.
		label := notificationButton("notification.expand", o.Locale.T("notification.more", remaining), width, o)
		overflow := notificationOverlay{
			Content: themed(cellPad(label, width), text, raised, false),
			Bounds:  hitRegion{ID: "notification", X: x, Y: row, W: width, H: 1},
			Targets: []hitRegion{{ID: "notification.expand", X: x, Y: row, W: ansi.StringWidth(label), H: 1}},
		}
		chosen = append(chosen, overflow)
	}
	return chosen
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
	detailsWidth := ansi.StringWidth(detailsLabel)
	closeWidth := ansi.StringWidth(closeLabel)
	showDetails := item.Details != "" || item.Context != ""
	controlWidth := closeWidth
	if compact && showDetails {
		// Keep the heading visible when fixed focus gutters consume a large
		// share of a compact card. Full details remain one click away.
		if detailsWidth+closeWidth+1 >= inner {
			detailsLabel = notificationButton(detailsID, "⋯", inner, o)
			detailsWidth = ansi.StringWidth(detailsLabel)
		}
		controlWidth += detailsWidth
	}
	heading := notificationHeading(item, o.Locale, o.Spinner)
	message := strings.Split(wrapText(notificationText(item.Message), inner), "\n")
	lines := make([]string, 0, len(message)+3)
	var targets []hitRegion
	if !compact {
		lines = append(lines, themed("╭"+strings.Repeat("─", width-2)+"╮", color, raised, false))
	}
	wrap := func(value string) string {
		if compact {
			return themed(cellPad(value, width), text, raised, false)
		}
		return themed("│ ", color, raised, false) + themed(cellPad(value, inner), text, raised, false) + themed(" │", color, raised, false)
	}
	value := themed(cellCrop(heading, max(1, inner-controlWidth-1)), color, raised, false)
	value = cellPad(value, max(1, inner-controlWidth))
	if compact && showDetails {
		targets = append(targets, hitRegion{ID: detailsID, X: padding + inner - controlWidth, Y: len(lines), W: detailsWidth, H: 1})
		value += detailsLabel
	}
	value += closeLabel
	targets = append(targets, hitRegion{ID: closeID, X: padding + inner - closeWidth, Y: len(lines), W: closeWidth, H: 1})
	lines = append(lines, wrap(value))
	for _, line := range message {
		lines = append(lines, wrap(line))
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
			label := detailsLabel
			x := width - ansi.StringWidth(label) - 2
			edge = ansi.Cut(edge, 0, x) + label + "─╯"
			targets = append(targets, hitRegion{ID: detailsID, X: x, Y: len(lines), W: ansi.StringWidth(label), H: 1})
		}
		lines = append(lines, themed(edge, color, raised, false))
	}
	return notificationOverlay{
		Content: strings.Join(lines, "\n"),
		Bounds:  hitRegion{ID: "notification", W: width, H: len(lines)},
		Targets: targets,
		IDs:     []uint64{item.ID},
	}
}
