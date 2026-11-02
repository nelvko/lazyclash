package tui

import (
	"fmt"
	"strings"
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
	g := dashboardGeometry(o)
	compact := o.Width < 100 || o.Height < 24
	right := 2
	if g.sidebar {
		right = 3
	}
	width := min(54, o.Width-right-g.x)
	y := 0
	capacity := 2
	if compact {
		width = o.Width - right - 2
		y, capacity = g.headerY+1, 1
	} else {
		// The card may replace right-side header actions, but the page title
		// and compact navigation remain usable while notifications arrive.
		titleEnd := g.x + ansi.StringWidth(pageTitle(o.Page, o.Locale)) + 2
		width = min(width, o.Width-right-titleEnd)
	}
	x := o.Width - right - width
	bottom := min(dashboardContentBottom(o), max(g.y, min(15, o.Height/2)))
	inputTargets := notificationInputs(o, g)
	for _, input := range inputTargets {
		if input.X < x+width && input.X+input.W > x && input.Y >= y {
			bottom = min(bottom, input.Y)
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
		label := cellCrop(o.Locale.T("notification.more", remaining), width)
		overflow := notificationOverlay{
			Content: themed(cellPad(label, width), text, raised, false),
			Bounds:  hitRegion{ID: "notification", X: x, Y: row, W: width, H: 1},
			Targets: []hitRegion{{ID: "notification.expand", X: x, Y: row, W: width, H: 1}},
		}
		if o.Focus == "notification.expand" || o.Hover == "notification.expand" {
			overflow.Content = themed(cellPad(label, width), focus, raised, false)
		}
		chosen = append(chosen, overflow)
	}
	return chosen
}

func notificationInputs(o dashboardOptions, g dashboardLayout) []hitRegion {
	var targets []hitRegion
	total := len(strings.Split(o.Body, "\n"))
	scroll := max(0, min(o.Scroll, max(0, total-g.height)))
	for _, target := range o.BodyTargets {
		switch target.ID {
		case "profiles.url", "conn.search", "rules.search", "logs.search":
			target.X += g.x
			target.Y += g.y - scroll
			if target, ok := clipRegion(target, g.x, g.y, g.width, g.height); ok {
				targets = append(targets, target)
			}
		}
	}
	return targets
}

func notificationCard(item notificationItem, o dashboardOptions, width int, compact bool) notificationOverlay {
	color := notificationColor(item.Kind)
	padding := 2
	if compact {
		padding = 0
	}
	inner := max(1, width-2*padding)
	closeLabel := "[×]"
	detailsLabel := o.Locale.T("notification.details")
	detailsWidth := ansi.StringWidth(detailsLabel)
	closeWidth := ansi.StringWidth(closeLabel)
	showDetails := item.Details != "" || item.Context != ""
	controlWidth := closeWidth
	if compact && showDetails {
		controlWidth += detailsWidth + 1
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
		id := fmt.Sprintf("notification.details.%d", item.ID)
		labelColor := muted
		if o.Focus == id || o.Hover == id {
			labelColor = focus
		}
		targets = append(targets, hitRegion{ID: id, X: padding + inner - controlWidth, Y: len(lines), W: detailsWidth, H: 1})
		value += themed(detailsLabel+" ", labelColor, raised, false)
	}
	closeColor := muted
	id := fmt.Sprintf("notification.close.%d", item.ID)
	if o.Focus == id || o.Hover == id {
		closeColor = focus
	}
	value += themed(closeLabel, closeColor, raised, false)
	targets = append(targets, hitRegion{ID: id, X: padding + inner - closeWidth, Y: len(lines), W: closeWidth, H: 1})
	lines = append(lines, wrap(value))
	for _, line := range message {
		lines = append(lines, wrap(line))
	}
	if item.Kind == "working" && o.Busy {
		id := fmt.Sprintf("notification.cancel.%d", item.ID)
		label := cellCrop(o.Locale.T("notification.cancel"), inner)
		color := muted
		if o.Focus == id || o.Hover == id {
			color = focus
		}
		targets = append(targets, hitRegion{ID: id, X: padding, Y: len(lines), W: ansi.StringWidth(label), H: 1})
		lines = append(lines, wrap(themed(label, color, raised, false)))
	}
	if !compact {
		edge := "╰" + strings.Repeat("─", width-2) + "╯"
		if showDetails {
			label := detailsLabel
			id := fmt.Sprintf("notification.details.%d", item.ID)
			x := width - ansi.StringWidth(label) - 2
			labelColor := muted
			if o.Focus == id || o.Hover == id {
				labelColor = focus
			}
			edge = ansi.Cut(edge, 0, x) + themed(label, labelColor, raised, false) + "─╯"
			targets = append(targets, hitRegion{ID: id, X: x, Y: len(lines), W: ansi.StringWidth(label), H: 1})
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
