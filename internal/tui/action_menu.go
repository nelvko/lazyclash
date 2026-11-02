package tui

import (
	"strings"

	bubbleskey "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type menuItem struct {
	ID, Label, DisabledReason string
	Danger                    bool
}

type menuResult struct {
	Action string
	Closed bool
}

// actionMenu owns presentation and input only. Its caller resolves action IDs
// against the current application state before performing an operation.
type actionMenu struct {
	open             bool
	items            []menuItem
	selected, offset int
	x, y             int
	bounds           hitRegion
	framed           bool
}

var menuKeys = struct{ up, down, first, last, activate, close bubbleskey.Binding }{
	up:       bubbleskey.NewBinding(bubbleskey.WithKeys("up", "k")),
	down:     bubbleskey.NewBinding(bubbleskey.WithKeys("down", "j")),
	first:    bubbleskey.NewBinding(bubbleskey.WithKeys("home")),
	last:     bubbleskey.NewBinding(bubbleskey.WithKeys("end")),
	activate: bubbleskey.NewBinding(bubbleskey.WithKeys("enter")),
	close:    bubbleskey.NewBinding(bubbleskey.WithKeys("esc", "tab", "shift+tab")),
}

func (m *actionMenu) show(items []menuItem, x, y int) {
	*m = actionMenu{open: len(items) > 0, items: append([]menuItem(nil), items...), selected: -1, x: x, y: y}
	m.move(1)
}

func (m *actionMenu) dismiss() {
	m.open = false
	m.bounds = hitRegion{}
}

func (m *actionMenu) setItems(items []menuItem) {
	id := ""
	if m.selected >= 0 && m.selected < len(m.items) {
		id = m.items[m.selected].ID
	}
	m.items = append([]menuItem(nil), items...)
	m.selected = -1
	if len(items) == 0 {
		m.dismiss()
		return
	}
	for i, item := range m.items {
		if item.ID == id {
			m.selected = i
			break
		}
	}
	if m.selected < 0 || m.items[m.selected].DisabledReason != "" {
		m.move(1)
	}
}

func (m *actionMenu) move(delta int) {
	count := len(m.items)
	if count == 0 {
		return
	}
	index := m.selected
	if index < 0 && delta < 0 {
		index = 0
	}
	for range count {
		index = (index + delta + count) % count
		if m.items[index].DisabledReason == "" {
			m.selected = index
			return
		}
	}
	m.selected = -1
}

func (m *actionMenu) choose() menuResult {
	if m.selected < 0 || m.selected >= len(m.items) || m.items[m.selected].DisabledReason != "" {
		return menuResult{}
	}
	id := m.items[m.selected].ID
	m.dismiss()
	return menuResult{Action: id, Closed: true}
}

func (m *actionMenu) key(msg tea.KeyPressMsg) menuResult {
	if !m.open {
		return menuResult{}
	}
	switch {
	case bubbleskey.Matches(msg, menuKeys.close):
		m.dismiss()
		return menuResult{Closed: true}
	case bubbleskey.Matches(msg, menuKeys.up):
		m.move(-1)
	case bubbleskey.Matches(msg, menuKeys.down):
		m.move(1)
	case bubbleskey.Matches(msg, menuKeys.first):
		m.selected = -1
		m.move(1)
	case bubbleskey.Matches(msg, menuKeys.last):
		m.selected = -1
		m.move(-1)
	case bubbleskey.Matches(msg, menuKeys.activate):
		return m.choose()
	}
	return menuResult{}
}

func (m *actionMenu) mouse(msg tea.MouseMsg) menuResult {
	if !m.open {
		return menuResult{}
	}
	mouse := msg.Mouse()
	b := m.bounds
	inside := mouse.X >= b.X && mouse.X < b.X+b.W && mouse.Y >= b.Y && mouse.Y < b.Y+b.H
	if _, click := msg.(tea.MouseClickMsg); click {
		if mouse.Button == tea.MouseRight || (!inside && mouse.Button == tea.MouseLeft) {
			m.dismiss()
			return menuResult{Closed: true}
		}
	}
	if !inside {
		return menuResult{}
	}
	if _, wheel := msg.(tea.MouseWheelMsg); wheel {
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.move(-1)
		case tea.MouseWheelDown:
			m.move(1)
		}
		return menuResult{}
	}
	padding := 0
	if m.framed {
		padding = 1
	}
	row := mouse.Y - b.Y - padding
	index := m.offset + row
	if mouse.X < b.X+padding || mouse.X >= b.X+b.W-padding || row < 0 || row >= b.H-2*padding || index >= len(m.items) {
		return menuResult{}
	}
	if m.items[index].DisabledReason != "" {
		return menuResult{}
	}
	switch msg.(type) {
	case tea.MouseMotionMsg:
		m.selected = index
	case tea.MouseClickMsg:
		if mouse.Button == tea.MouseLeft {
			m.selected = index
			return m.choose()
		}
	}
	return menuResult{}
}

func (m *actionMenu) render(base string, width, height int) string {
	if !m.open {
		return base
	}
	if width <= 0 || height <= 0 {
		m.bounds = hitRegion{}
		return ""
	}
	labels := make([]string, len(m.items))
	contentWidth := 0
	for i, item := range m.items {
		labels[i] = clean(item.Label)
		if item.DisabledReason != "" {
			labels[i] += " · " + clean(item.DisabledReason)
		}
		contentWidth = max(contentWidth, ansi.StringWidth(labels[i]))
	}
	menuWidth := min(width, max(18, min(48, contentWidth+4)))
	m.framed = menuWidth >= 4 && height >= 3
	padding := 0
	if m.framed {
		padding = 1
	}
	visible := min(len(m.items), height-2*padding)
	menuHeight := visible + 2*padding
	m.offset = min(m.offset, max(0, len(m.items)-visible))
	if m.selected >= 0 {
		m.offset = min(m.offset, m.selected)
		m.offset = max(m.offset, m.selected-visible+1)
	}
	m.bounds = hitRegion{X: max(0, min(m.x, width-menuWidth)), Y: max(0, min(m.y, height-menuHeight)), W: menuWidth, H: menuHeight}
	lines := make([]string, 0, menuHeight)
	if m.framed {
		lines = append(lines, m.edge(menuWidth, true, m.offset > 0))
	}
	for i := m.offset; i < m.offset+visible; i++ {
		item := m.items[i]
		prefix, foreground, background := "  ", text, raised
		if item.Danger {
			foreground = errorColor
		}
		if item.DisabledReason != "" {
			foreground = disabled
		} else if i == m.selected {
			prefix, background = "› ", selectionBackground
		}
		line := themed(cellPad(prefix+labels[i], menuWidth-2*padding), foreground, background, false)
		if m.framed {
			line = themed("│", border, raised, false) + line + themed("│", border, raised, false)
		}
		lines = append(lines, line)
	}
	if m.framed {
		lines = append(lines, m.edge(menuWidth, false, m.offset+visible < len(m.items)))
	}
	return overlay(base, strings.Join(lines, "\n"), m.bounds.X, m.bounds.Y, width, height)
}

func (m actionMenu) edge(width int, top, more bool) string {
	left, right, marker := "╰", "╯", "↓"
	if top {
		left, right, marker = "╭", "╮", "↑"
	}
	stroke := strings.Repeat("─", width-2)
	if more {
		stroke = strings.Repeat("─", width-3) + marker
	}
	return themed(left+stroke+right, border, raised, false)
}
