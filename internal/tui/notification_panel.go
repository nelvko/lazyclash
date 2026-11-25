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
	listAction  string
	listOffset  int
}

func (p *notificationPanel) show(id uint64) {
	if !p.initialized {
		p.viewport = viewport.New()
		p.initialized = true
	}
	if id != 0 && p.open && p.detailID == 0 {
		p.listAction = fmt.Sprintf("notification.panel.details.%d", id)
		p.listOffset = p.viewport.YOffset()
	} else if !p.open {
		p.listAction = ""
	}
	p.open, p.detailID, p.focus = true, id, 0
	p.viewport.GotoTop()
}

// Match the item when rebuilding the list; other results can change while
// the user reads details. Restore the offset after the new content is sized.
func (p *notificationPanel) restoreListSelection(previous string) {
	if p.listAction != "" {
		previous = p.listAction
	}
	if previous == "" {
		return
	}
	for i, action := range p.actions {
		if action.ID == previous {
			p.focus = i
			return
		}
	}
	// Resizing the list can remove the Details button. Keep selection
	// on the same card if it still has a control.
	id := previous[strings.LastIndex(previous, ".")+1:]
	for i, action := range p.actions {
		if strings.HasSuffix(action.ID, "."+id) {
			p.focus = i
			return
		}
	}
	if p.listAction == "" {
		return
	}
	// The viewed notice was dismissed. Focus the next visible control
	// at that list position, or the final control if it was the last.
	for i, action := range p.actions {
		p.focus = i
		if action.Y >= p.listOffset {
			break
		}
	}
}

func (p *notificationPanel) restoreListScroll() {
	if p.detailID == 0 && p.listAction != "" {
		p.viewport.SetYOffset(p.listOffset)
		p.revealAction()
		p.listAction = ""
	}
}

func (p *notificationPanel) dismiss() {
	p.open, p.detailID, p.listAction = false, 0, ""
	p.actions, p.targets = nil, nil
}

func (p notificationPanel) backAction() string {
	if p.detailID != 0 && p.listAction != "" {
		return "notification.panel.back"
	}
	return "notification.panel.close"
}

func (p *notificationPanel) contentFits() bool {
	return p.bounds.W >= 20 && p.bounds.H >= 5
}

func (p *notificationPanel) key(msg tea.KeyPressMsg) string {
	key := msg.String()
	if key == "ctrl+n" {
		return "notification.panel.close"
	}
	if !p.contentFits() {
		switch key {
		case "esc", "q", "enter", "space", " ":
			return "notification.panel.close"
		}
		return ""
	}
	switch key {
	case "esc", "q":
		return p.backAction()
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
		if action, visible := p.focusedAction(); visible {
			return action.ID
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

func (p notificationPanel) focusedAction() (hitRegion, bool) {
	if p.focus < 0 || p.focus >= len(p.actions) {
		return hitRegion{}, false
	}
	action := p.actions[p.focus]
	top := p.viewport.YOffset()
	return action, action.Y >= top && action.Y < top+p.viewport.Height()
}

func (p *notificationPanel) selectedID() uint64 {
	if p.detailID != 0 {
		return p.detailID
	}
	action, visible := p.focusedAction()
	if !visible {
		return 0
	}
	parts := strings.Split(action.ID, ".")
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
	_, click := msg.(tea.MouseClickMsg)
	if !inside {
		if click && point.Button == tea.MouseLeft {
			return "notification.panel.close"
		}
		return ""
	}
	if _, wheel := msg.(tea.MouseWheelMsg); wheel {
		p.viewport, _ = p.viewport.Update(msg)
		p.reconcileViewportFocus()
		return ""
	}
	if !click || point.Button != tea.MouseLeft {
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
	var selected string
	if p.focus >= 0 && p.focus < len(p.actions) {
		selected = p.actions[p.focus].ID
	}
	p.actions, p.targets = nil, nil
	if !p.open || len(items) == 0 || width <= 0 || height <= 0 {
		p.bounds = hitRegion{}
		p.viewport.GotoTop()
		return "", nil
	}

	// Match the Toast right edge. Compact layouts expand below the page
	// title, keeping the menu and footer available as points of reference.
	right := 2
	if sidebarVisible(width, height) {
		right = 3
	}
	if width < 24 {
		right = 0
	}
	w := min(54, width-2*right)
	x, y := width-right-w, 0
	if width < 100 || height < 24 {
		y = min(2, max(1, height-6), height-1)
	}
	maxHeight := min(18, max(8, height/2))
	if p.detailID == 0 {
		// Give a stack room for two complete cards on compact terminals.
		maxHeight = min(22, max(12, 2*height/3))
	}
	maxHeight = min(maxHeight, height-y-1)
	p.bounds = hitRegion{X: x, Y: y, W: w, H: maxHeight}
	options := dashboardOptions{PlainButtons: len(plain) > 0 && plain[0]}
	if !p.contentFits() {
		options.Focus = "notification.panel.close"
		button := notificationButton(options.Focus, locale.T("notification.collapse"), w, options)
		if w < 8 {
			button = themed("×", text, raised, false)
		}
		buttonWidth := ansi.StringWidth(button)
		p.bounds = hitRegion{X: width - right - buttonWidth, Y: y, W: buttonWidth, H: 1}
		p.targets = []hitRegion{{ID: options.Focus, X: p.bounds.X, Y: y, W: buttonWidth, H: 1}}
		return button, p.targets
	}
	if p.detailID == 0 {
		return p.renderList(items, locale, options, selected)
	}
	for _, item := range items {
		if item.ID == p.detailID {
			return p.renderDetail(item, locale, options)
		}
	}
	return "", nil
}

func (p *notificationPanel) renderList(items []notificationItem, locale i18n.Localizer, options dashboardOptions, previous string) (string, []hitRegion) {
	x, y, w := p.bounds.X, p.bounds.Y, p.bounds.W
	inner := w - 4
	var lines, labels []string
	for _, item := range items {
		body := notificationBody(item, locale, "")
		first, _, more := strings.Cut(body, "\n")
		dismissID := fmt.Sprintf("notification.panel.dismiss.%d", item.ID)
		close := notificationButton(dismissID, "×", inner, options)
		closeWidth := ansi.StringWidth(close)
		space := inner - closeWidth
		summary := first
		if more {
			summary += "…"
		}
		summary = cellCrop(summary, space)
		card := []string{themed(cellPad(summary, space), text, raised, false) + close}
		bodyRow := len(lines) + 1
		if more || summary != first || notificationExtra(item) != "" {
			id := fmt.Sprintf("notification.panel.details.%d", item.ID)
			label := locale.T("notification.details")
			button := notificationButton(id, label, inner, options)
			p.actions = append(p.actions, hitRegion{ID: id, X: 2, Y: bodyRow + 1, W: ansi.StringWidth(button), H: 1})
			labels = append(labels, label)
			card = append(card, button)
		}
		p.actions = append(p.actions, hitRegion{ID: dismissID, X: 2 + space, Y: bodyRow, W: closeWidth, H: 1})
		labels = append(labels, "×")
		if item.Kind == "working" {
			id := fmt.Sprintf("notification.cancel.%d", item.ID)
			label := locale.T("notification.cancel")
			button := notificationButton(id, label, inner, options)
			p.actions = append(p.actions, hitRegion{ID: id, X: 2, Y: bodyRow + len(card), W: ansi.StringWidth(button), H: 1})
			labels = append(labels, label)
			card = append(card, button)
		}
		lines = append(lines, inputFrame(card, w, notificationColor(item.Kind), raised)...)
	}

	// The collection has a shared header and footer. Only individual cards
	// have borders, so each result stays associated with its own actions.
	p.bounds.H = min(p.bounds.H, len(lines)+2)
	p.viewport.SetWidth(w)
	p.viewport.SetHeight(p.bounds.H - 2)
	p.restoreListSelection(previous)
	p.focus = max(0, min(p.focus, len(p.actions)-1))
	selected := p.actions[p.focus]
	options.Focus = selected.ID
	focused := notificationButton(selected.ID, labels[p.focus], selected.W, options)
	lines[selected.Y] = ansi.Cut(lines[selected.Y], 0, selected.X) + focused + ansi.Cut(lines[selected.Y], selected.X+selected.W, w)
	p.viewport.SetContentLines(lines)
	p.restoreListScroll()

	p.contentY = y + 1
	rows := []string{themed(cellPad("  "+locale.T("notification.listTitle"), w), text, raised, false)}
	rows = append(rows, strings.Split(p.viewport.View(), "\n")...)
	rows = append(rows, p.footer(locale, options, w, 0))
	for _, action := range p.actions {
		action.X += x
		action.Y += p.contentY - p.viewport.YOffset()
		if target, ok := clipRegion(action, x, p.contentY, w, p.viewport.Height()); ok {
			p.targets = append(p.targets, target)
		}
	}
	return strings.Join(rows, "\n"), p.targets
}

func (p *notificationPanel) renderDetail(item notificationItem, locale i18n.Localizer, options dashboardOptions) (string, []hitRegion) {
	w := p.bounds.W
	inner := w - 4
	lines := strings.Split(wrapText(notificationBody(item, locale, ""), inner), "\n")
	if extra := notificationExtra(item); extra != "" {
		lines = append(lines, "")
		lines = append(lines, strings.Split(wrapText(extra, inner), "\n")...)
	}
	p.bounds.H = min(p.bounds.H, max(5, 3+len(lines)))
	p.contentY = p.bounds.Y + 1
	p.viewport.SetWidth(inner)
	p.viewport.SetHeight(p.bounds.H - 3)
	p.viewport.SetContentLines(lines)

	rows := strings.Split(p.viewport.View(), "\n")
	id := fmt.Sprintf("notification.panel.dismiss.%d", item.ID)
	close := notificationButton(id, "×", inner, options)
	closeWidth := ansi.StringWidth(close)
	footer := p.footer(locale, options, inner-closeWidth, 2)
	p.targets = append(p.targets, hitRegion{ID: id, X: p.bounds.X + w - 2 - closeWidth, Y: p.contentY + p.viewport.Height(), W: closeWidth, H: 1})
	rows = append(rows, footer+close)
	for i, line := range rows {
		rows[i] = themed(cellPad(line, inner), text, raised, false)
	}
	return strings.Join(inputFrame(rows, w, notificationColor(item.Kind), raised), "\n"), p.targets
}

// inset is the horizontal border and padding of the detail card, or zero
// for the footer below the stack. Both keep their controls outside the viewport.
func (p *notificationPanel) footer(locale i18n.Localizer, options dashboardOptions, width, inset int) string {
	id, label := p.backAction(), locale.T("notification.collapse")
	if id == "notification.panel.back" {
		label = locale.T("notification.back")
	}
	button := notificationButton(id, label, width, options)
	footerY := p.contentY + p.viewport.Height()
	p.targets = append(p.targets, hitRegion{ID: id, X: p.bounds.X + inset, Y: footerY, W: ansi.StringWidth(button), H: 1})
	arrow := ""
	switch {
	case !p.viewport.AtTop() && !p.viewport.AtBottom():
		arrow = "↕"
	case !p.viewport.AtBottom():
		arrow = "↓"
	case !p.viewport.AtTop():
		arrow = "↑"
	}
	return themed(cellPad(button, width-ansi.StringWidth(arrow))+arrow, muted, raised, false)
}
