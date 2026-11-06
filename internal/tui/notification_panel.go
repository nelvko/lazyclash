package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

// A viewport keeps variable-length messages intact in both the collection and
// technical details. Bubbles list delegates have a fixed row height.
type notificationPanel struct {
	open        bool
	detailID    uint64
	viewport    viewport.Model
	initialized bool
	focus       int
	actions     []hitRegion
	targets     []hitRegion
	bounds      hitRegion
	contentY    int
}

func (p *notificationPanel) show(id uint64) {
	if !p.initialized {
		p.viewport = viewport.New()
		p.initialized = true
	}
	p.open, p.detailID, p.focus = true, id, 0
	p.viewport.GotoTop()
}

func (p *notificationPanel) dismiss() {
	p.open = false
	p.actions, p.targets = nil, nil
}

func (p *notificationPanel) key(msg tea.KeyPressMsg) string {
	key := msg.String()
	switch key {
	case "esc", "q":
		if p.detailID != 0 {
			return "notification.panel.back"
		}
		return "notification.panel.close"
	case "tab", "shift+tab":
		if len(p.actions) > 0 {
			delta := 1
			if key == "shift+tab" {
				delta = -1
			}
			p.focus = (p.focus + delta + len(p.actions)) % len(p.actions)
			p.revealAction()
		}
	case "enter", "space", " ":
		if p.focus >= 0 && p.focus < len(p.actions) {
			return p.actions[p.focus].ID
		}
	case "x", "delete", "backspace":
		if id := p.selectedID(); id != 0 {
			return fmt.Sprintf("notification.panel.dismiss.%d", id)
		}
	case "up", "k", "down", "j":
		if p.detailID == 0 && len(p.actions) > 0 {
			delta := 1
			if key == "up" || key == "k" {
				delta = -1
			}
			p.focus = max(0, min(len(p.actions)-1, p.focus+delta))
			p.revealAction()
		} else {
			p.viewport, _ = p.viewport.Update(msg)
		}
	case "home":
		p.viewport.GotoTop()
		p.focus = 0
	case "end":
		p.viewport.GotoBottom()
		p.focus = max(0, len(p.actions)-1)
	default:
		p.viewport, _ = p.viewport.Update(msg)
		p.reconcileViewportFocus()
	}
	return ""
}

func (p *notificationPanel) reconcileViewportFocus() {
	if p.detailID != 0 || p.focus < 0 || p.focus >= len(p.actions) {
		return
	}
	top, bottom := p.viewport.YOffset(), p.viewport.YOffset()+p.viewport.Height()
	if y := p.actions[p.focus].Y; y >= top && y < bottom {
		return
	}
	for index, action := range p.actions {
		if action.Y >= top && action.Y < bottom {
			p.focus = index
			return
		}
	}
}

func (p *notificationPanel) selectedID() uint64 {
	if p.detailID != 0 {
		return p.detailID
	}
	if p.focus < 0 || p.focus >= len(p.actions) {
		return 0
	}
	parts := strings.Split(p.actions[p.focus].ID, ".")
	id, _ := strconv.ParseUint(parts[len(parts)-1], 10, 64)
	return id
}

func (p *notificationPanel) revealAction() {
	if p.focus < 0 || p.focus >= len(p.actions) {
		return
	}
	y := p.actions[p.focus].Y
	if y < p.viewport.YOffset() {
		p.viewport.SetYOffset(y)
	}
	if y >= p.viewport.YOffset()+p.viewport.Height() {
		p.viewport.SetYOffset(y - p.viewport.Height() + 1)
	}
}

func (p *notificationPanel) mouse(msg tea.MouseMsg) string {
	point := msg.Mouse()
	inside := point.X >= p.bounds.X && point.X < p.bounds.X+p.bounds.W && point.Y >= p.bounds.Y && point.Y < p.bounds.Y+p.bounds.H
	if !inside {
		return ""
	}
	if _, wheel := msg.(tea.MouseWheelMsg); wheel {
		p.viewport, _ = p.viewport.Update(msg)
		p.reconcileViewportFocus()
		return ""
	}
	if _, click := msg.(tea.MouseClickMsg); !click || point.Button != tea.MouseLeft {
		return ""
	}
	for _, target := range p.targets {
		if point.X >= target.X && point.X < target.X+target.W && point.Y >= target.Y && point.Y < target.Y+target.H {
			return target.ID
		}
	}
	return ""
}

func (p *notificationPanel) render(items []notificationItem, width, height int, locale i18n.Localizer, plain ...bool) (string, []hitRegion) {
	if !p.open || width <= 0 || height <= 0 {
		return "", nil
	}
	if !p.initialized {
		p.viewport = viewport.New()
		p.initialized = true
	}
	w, h := min(90, width), min(30, height)
	x, y := (width-w)/2, (height-h)/2
	p.bounds = hitRegion{X: x, Y: y, W: w, H: h}
	rows := make([]string, height)
	for i := range rows {
		rows[i] = themed(strings.Repeat(" ", width), muted, bg, true)
	}
	options := dashboardOptions{PlainButtons: len(plain) > 0 && plain[0]}
	if w < 20 || h < 6 {
		options.Focus = "notification.panel.close"
		label := notificationButton(options.Focus, locale.T("notification.collapse"), width, options)
		putRows(rows, 0, 0, label, width)
		p.bounds = hitRegion{W: width, H: height}
		p.targets = []hitRegion{{ID: "notification.panel.close", W: min(width, ansi.StringWidth(label)), H: 1}}
		return strings.Join(rows, "\n"), p.targets
	}
	inner := w - 6
	p.viewport.SetWidth(inner)
	p.viewport.SetHeight(h - 6)
	var lines []string
	p.actions = nil
	var actionLabels []string
	if p.detailID != 0 {
		for _, item := range items {
			if item.ID != p.detailID {
				continue
			}
			lines = append(lines, strings.Split(wrapText(notificationHeading(item, locale, ""), inner), "\n")...)
			lines = append(lines, "")
			lines = append(lines, strings.Split(wrapText(notificationText(item.Message), inner), "\n")...)
			if item.Details != "" && item.Details != item.Message {
				lines = append(lines, "")
				lines = append(lines, strings.Split(wrapText(notificationText(item.Details), inner), "\n")...)
			}
		}
	} else {
		for _, item := range items {
			lines = append(lines, strings.Split(wrapText(notificationHeading(item, locale, ""), inner), "\n")...)
			lines = append(lines, strings.Split(wrapText(notificationText(item.Message), inner), "\n")...)
			detailsID := fmt.Sprintf("notification.panel.details.%d", item.ID)
			dismissID := fmt.Sprintf("notification.panel.dismiss.%d", item.ID)
			detailsLabel, dismissLabel := locale.T("notification.details"), locale.T("notification.dismiss")
			details := notificationButton(detailsID, detailsLabel, inner, options)
			dismiss := notificationButton(dismissID, dismissLabel, inner, options)
			lineY := len(lines)
			p.actions = append(p.actions, hitRegion{ID: detailsID, Y: lineY, W: ansi.StringWidth(details), H: 1})
			actionLabels = append(actionLabels, detailsLabel, dismissLabel)
			if ansi.StringWidth(details)+ansi.StringWidth(dismiss) > inner {
				lines = append(lines, details, dismiss, "")
				p.actions = append(p.actions, hitRegion{ID: dismissID, Y: lineY + 1, W: ansi.StringWidth(dismiss), H: 1})
			} else {
				lines = append(lines, details+dismiss, "")
				p.actions = append(p.actions, hitRegion{ID: dismissID, X: ansi.StringWidth(details), Y: lineY, W: ansi.StringWidth(dismiss), H: 1})
			}
			if item.Kind == "working" {
				id := fmt.Sprintf("notification.cancel.%d", item.ID)
				label := notificationButton(id, locale.T("notification.cancel"), inner, options)
				p.actions = append(p.actions, hitRegion{ID: id, Y: len(lines) - 1, W: ansi.StringWidth(label), H: 1})
				actionLabels = append(actionLabels, locale.T("notification.cancel"))
				lines[len(lines)-1] = label
				lines = append(lines, "")
			}
		}
	}
	if len(lines) == 0 {
		lines = append(lines, locale.T("notification.empty"))
	}
	p.focus = max(0, min(p.focus, len(p.actions)-1))
	if p.detailID == 0 && len(p.actions) > 0 {
		selected := p.actions[p.focus]
		options.Focus = selected.ID
		value := notificationButton(selected.ID, actionLabels[p.focus], selected.W, options)
		lines[selected.Y] = ansi.Cut(lines[selected.Y], 0, selected.X) + value + ansi.Cut(lines[selected.Y], selected.X+selected.W, ansi.StringWidth(lines[selected.Y]))
	}
	p.viewport.SetContent(strings.Join(lines, "\n"))
	box := make([]string, h)
	for i := range box {
		box[i] = themed("│"+strings.Repeat(" ", w-2)+"│", border, raised, false)
	}
	box[0] = themed("╭"+strings.Repeat("─", w-2)+"╮", border, raised, false)
	box[h-1] = themed("╰"+strings.Repeat("─", w-2)+"╯", border, raised, false)
	putRows(box, 3, 1, themed(cellCrop(locale.T("notification.listTitle"), inner), text, raised, false), w)
	view := strings.Split(p.viewport.View(), "\n")
	for index, line := range view {
		if index >= p.viewport.Height() {
			break
		}
		putRows(box, 3, index+3, themed(cellPad(line, inner), text, raised, false), w)
	}
	backID, backLabel := "notification.panel.close", locale.T("notification.collapse")
	help := locale.T("notification.panelHelp")
	if p.detailID != 0 {
		backID, backLabel = "notification.panel.back", locale.T("notification.back")
		help = locale.T("notification.detailHelp")
	}
	back := notificationButton(backID, backLabel, inner, options)
	backWidth := ansi.StringWidth(back)
	putRows(box, 3, h-2, back, w)
	if left := 3 + backWidth + 2; left < w-3 {
		putRows(box, left, h-2, helpText(cellCrop(help, w-left-3), raised, false), w)
	}
	p.targets = []hitRegion{{ID: backID, X: x + 3, Y: y + h - 2, W: backWidth, H: 1}}
	if p.detailID != 0 {
		id := fmt.Sprintf("notification.panel.dismiss.%d", p.detailID)
		label := notificationButton(id, locale.T("notification.dismiss"), inner/2, options)
		labelWidth := ansi.StringWidth(label)
		buttonX := w - 3 - labelWidth
		putRows(box, buttonX, 1, label, w)
		p.targets = append(p.targets, hitRegion{ID: id, X: x + buttonX, Y: y + 1, W: labelWidth, H: 1})
	}
	p.contentY = y + 3
	for _, action := range p.actions {
		action.X += x + 3
		action.Y += p.contentY - p.viewport.YOffset()
		if target, ok := clipRegion(action, x+3, p.contentY, inner, p.viewport.Height()); ok {
			p.targets = append(p.targets, target)
		}
	}
	for index, line := range box {
		putRows(rows, x, y+index, line, width)
	}
	return strings.Join(rows, "\n"), p.targets
}
