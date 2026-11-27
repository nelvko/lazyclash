package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

func (p *notificationPanel) key(msg tea.KeyPressMsg) string {
	key := msg.String()
	if key == "ctrl+n" {
		return "notification.panel.close"
	}
	if p.bounds.H == 1 { // Only the return button fits.
		switch key {
		case "esc", "q", "enter", "space", " ":
			return p.backAction()
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
	w := min(maxNotificationWidth, width-2*right)
	x, y := width-right-w, 0
	if width < 100 || height < 24 {
		y = min(2, max(1, height-6), height-1)
	}
	maxHeight := max(8, height/2)
	if p.detailID == 0 {
		// Give a stack room for two complete cards on compact terminals.
		maxHeight = max(12, 2*height/3)
	}
	maxHeight = min(maxHeight, height-y-1)
	p.bounds = hitRegion{X: x, Y: y, W: w, H: maxHeight}
	options := dashboardOptions{Locale: locale, PlainButtons: len(plain) > 0 && plain[0]}
	if w < 20 || maxHeight < 5 {
		options.Focus = p.backAction()
		button := notificationButton(options.Focus, "‹", buttonIcon, w, options)
		if w < 5 {
			button = themed("‹", text, raised, false)
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
	x, y, maxWidth := p.bounds.X, p.bounds.Y, p.bounds.W
	options.Busy = true              // Retained working cards expose their own Cancel control.
	viewportHeight := p.bounds.H - 1 // Leave the list heading visible.
	renderCard := func(item notificationItem, height int) notificationOverlay {
		return notificationCardWithControls(item, options, maxWidth, height, false,
			fmt.Sprintf("notification.panel.dismiss.%d", item.ID), fmt.Sprintf("notification.panel.details.%d", item.ID))
	}

	cards := make([]notificationOverlay, 0, len(items))
	w, height := 0, 0
	for i, item := range items {
		if i > 0 {
			height++ // A neutral row separates adjacent cards, even without color.
		}
		card := renderCard(item, viewportHeight)
		if len(items) > 1 && len(card.IDs) == 0 && viewportHeight > 4 {
			// Only an already-clipped preview gives up room for its neighbor.
			// Taller complete cards can be followed by scrolling the list.
			card = renderCard(item, max(4, viewportHeight-4))
		}
		card.position(maxWidth-card.Bounds.W, height)
		height += card.Bounds.H
		w = max(w, card.Bounds.W)
		cards = append(cards, card)
		p.actions = append(p.actions, card.Targets...)
	}
	x += maxWidth - w
	p.bounds.X, p.bounds.W, p.bounds.H = x, w, min(p.bounds.H, height+1)
	for i := range p.actions {
		p.actions[i].X -= maxWidth - w
	}

	p.viewport.SetWidth(w)
	p.viewport.SetHeight(p.bounds.H - 1)
	p.restoreListSelection(previous)
	p.focus = max(0, min(p.focus, len(p.actions)-1))
	selected := p.actions[p.focus]
	options.Focus = selected.ID
	lines := make([]string, 0, height)
	for i, card := range cards {
		if i > 0 {
			lines = append(lines, themed(strings.Repeat(" ", w), text, raised, false))
		}
		// Controls follow the focused notification after resizing or a retry.
		// Repaint that card with the same labels and geometry as its Toast.
		if selected.Y >= card.Bounds.Y && selected.Y < card.Bounds.Y+card.Bounds.H {
			card.Content = renderCard(items[i], card.Bounds.H).Content
		}
		indent := themed(strings.Repeat(" ", w-card.Bounds.W), text, raised, false)
		for _, line := range strings.Split(card.Content, "\n") {
			lines = append(lines, indent+line)
		}
	}
	p.viewport.SetContentLines(lines)
	p.restoreListScroll()

	p.contentY = y + 1
	arrow := p.scrollIndicator()
	heading := cellPad("  "+locale.T("notification.listTitle"), w-ansi.StringWidth(arrow)-1) + arrow + " "
	rows := []string{themed(heading, text, raised, false)}
	rows = append(rows, strings.Split(p.viewport.View(), "\n")...)
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
	paint := notificationPaint(item.Kind)
	body, extra := notificationBody(item, locale, ""), notificationExtra(item)
	prefix := ""
	if paint.marker != "" {
		prefix = themed(paint.marker+" ", paint.signal, paint.background, false)
	}
	prefixWidth := ansi.StringWidth(prefix)
	id := fmt.Sprintf("notification.panel.dismiss.%d", item.ID)
	close := paint.button(id, "×", buttonIcon, p.bounds.W, options)
	closeWidth := ansi.StringWidth(close)
	w := min(p.bounds.W, max(minNotificationWidth, 4+prefixWidth+max(lipgloss.Width(body), lipgloss.Width(extra))+closeWidth))
	p.bounds.X += p.bounds.W - w
	p.bounds.W = w
	inner := w - 4
	readingWidth := inner - closeWidth - prefixWidth
	if extra != "" {
		body += "\n\n" + extra
	}
	lines := notificationLines(body, readingWidth, readingWidth)
	for i := range lines {
		if i == 0 {
			lines[i] = prefix + lines[i]
		} else {
			lines[i] = strings.Repeat(" ", prefixWidth) + lines[i]
		}
	}
	p.bounds.H = min(p.bounds.H, 2+len(lines))
	p.contentY = p.bounds.Y + 1
	p.viewport.SetWidth(inner - closeWidth)
	p.viewport.SetHeight(p.bounds.H - 2)
	p.viewport.SetContentLines(lines)

	rows := strings.Split(p.viewport.View(), "\n")
	rows[0] = cellPad(rows[0], p.viewport.Width()) + close
	p.targets = append(p.targets, hitRegion{ID: id, X: p.bounds.X + 2 + p.viewport.Width(), Y: p.contentY, W: closeWidth, H: 1})
	blank := themed(strings.Repeat(" ", w), paint.foreground, paint.background, false)
	for i, row := range rows {
		rows[i] = themed("  "+cellPad(row, inner)+"  ", paint.foreground, paint.background, false)
	}
	arrow := p.scrollIndicator()
	bottom := themed(strings.Repeat(" ", w-ansi.StringWidth(arrow)-2)+arrow+"  ", paint.foreground, paint.background, false)
	return blank + "\n" + strings.Join(rows, "\n") + "\n" + bottom, p.targets
}

func (p *notificationPanel) scrollIndicator() string {
	switch {
	case !p.viewport.AtTop() && !p.viewport.AtBottom():
		return "↕"
	case !p.viewport.AtBottom():
		return "↓"
	case !p.viewport.AtTop():
		return "↑"
	}
	return ""
}
