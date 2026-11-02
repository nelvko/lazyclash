package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m Model) helpLines(width int) []string {
	tr := m.tr
	lines := []string{tr("help.navigation"), tr("help.pages.first"), tr("help.pages.second"), "", tr("help.navigation.tab"), tr("help.navigation.arrows"), tr("help.navigation.scroll"), tr("help.navigation.cancel"), tr("help.keyHints.shortcut"), "", tr("help.currentPage", pageTitle(m.page, m.locale))}
	switch m.page {
	case "proxies":
		lines = append(lines, tr("help.proxies.expand"), tr("help.proxies.options"))
	case "profiles":
		lines = append(lines, tr("help.profiles.add"), tr("help.profiles.import"), tr("help.profiles.rename"), tr("help.profiles.click"), tr("help.profiles.menu"), tr("help.profiles.menuControls"), tr("help.profiles.grid"), tr("help.profiles.actions"), tr("help.profiles.update"), tr("help.profiles.overrides"), tr("help.profiles.delete"))
	case "connections":
		lines = append(lines, tr("help.connections.keys"), tr("help.connections.confirm"))
	case "rules":
		lines = append(lines, tr("help.rules.keys"), tr("help.rules.readOnly"))
	case "settings":
		lines = append(lines, tr("help.settings.ports"), tr("help.settings.review"), tr("help.settings.persistence"), tr("help.settings.tun"))
	case "logs":
		lines = append(lines, tr("help.logs.keys"), tr("help.logs.liveOnly"), tr("help.logs.retention"))
	default:
		lines = append(lines, tr("help.overview.install"), tr("help.overview.import"), tr("help.overview.controls"), tr("help.overview.refresh"), tr("help.overview.unavailable"))
	}
	lines = append(lines, "", tr("help.core.title"), tr("help.core.actions"), tr("help.core.refresh"), tr("help.core.quit"), "", tr("help.forms.title"), tr("help.forms.keys"), tr("help.forms.input"), tr("help.forms.modal"), "", tr("help.messages.title"), tr("help.messages.toast"), tr("help.messages.dismiss"), tr("help.messages.shared"))
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(wrapText(line, max(1, width)), "\n")...)
	}
	return wrapped
}

func (m *Model) helpKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "?":
		_, cmd := m.keyHintsAction("help.close")
		return cmd
	case "tab", "shift+tab":
		if m.keyHints.helpFocus == "key-hints.toggle" {
			m.keyHints.helpFocus = "help.close"
		} else {
			m.keyHints.helpFocus = "key-hints.toggle"
		}
	case "enter":
		if m.keyHints.helpFocus == "key-hints.toggle" {
			return m.toggleKeyHints()
		}
		_, cmd := m.keyHintsAction("help.close")
		return cmd
	case "q":
		return m.requestQuit()
	case "down", "j":
		m.helpScroll++
	case "up", "k":
		m.helpScroll = max(0, m.helpScroll-1)
	case "pgdown", "space", " ":
		m.helpScroll += max(1, min(28, m.height-2)-7)
	case "pgup":
		m.helpScroll = max(0, m.helpScroll-max(1, min(28, m.height-2)-7))
	case "home":
		m.helpScroll = 0
	case "end":
		m.helpScroll = len(m.helpLines(max(1, min(82, m.width)-6)))
	}
	return nil
}

func (m *Model) helpMouse(event tea.MouseMsg) tea.Cmd {
	if _, wheel := event.(tea.MouseWheelMsg); wheel {
		switch event.Mouse().Button {
		case tea.MouseWheelUp:
			m.helpScroll = max(0, m.helpScroll-3)
		case tea.MouseWheelDown:
			m.helpScroll += 3
		}
	}
	if _, clicked := event.(tea.MouseClickMsg); clicked && event.Mouse().Button == tea.MouseLeft {
		for _, hit := range m.targets {
			k := event.Mouse()
			if k.X >= hit.X && k.X < hit.X+hit.W && k.Y >= hit.Y && k.Y < hit.Y+hit.H {
				m.keyHints.helpFocus = hit.ID
				_, cmd := m.keyHintsAction(hit.ID)
				return cmd
			}
		}
	}
	return nil
}

func (m *Model) renderHelp(base string) (string, []hitRegion) {
	tr := m.tr
	w, h := min(82, m.width), min(28, m.height)
	x, y := (m.width-w)/2, (m.height-h)/2
	button := func(id, label string, width int) string {
		label = strings.Trim(label, "[] ")
		return cellCrop(renderButton(cellCrop(label, max(1, width-4)), buttonOptions{
			Kind: buttonText, Surface: raised, Focused: m.keyHints.helpFocus == id || id == "help.close" && m.keyHints.helpFocus == "", Hovered: m.hover == id, Plain: m.buttonPlain(),
		}), width)
	}
	if w < 26 || h < 8 {
		rows := make([]string, max(1, m.height))
		for i := range rows {
			rows[i] = themed(strings.Repeat(" ", max(0, m.width)), text, raised, false)
		}
		if m.width < 1 || m.height < 1 {
			return "", nil
		}
		putRows(rows, 0, 0, themed(cellCrop(tr("help.small"), m.width), text, raised, false), m.width)
		label := button("help.close", tr("help.close"), m.width)
		if ansi.StringWidth(label) == 0 {
			label = "×"
		}
		putRows(rows, 0, m.height-1, label, m.width)
		hits := []hitRegion{{ID: "help.close", X: 0, Y: m.height - 1, W: max(1, ansi.StringWidth(label)), H: 1}}
		if m.height >= 3 {
			label = button("key-hints.toggle", m.keyHintsLabel(), m.width)
			putRows(rows, 0, m.height-2, label, m.width)
			hits = append(hits, hitRegion{ID: "key-hints.toggle", X: 0, Y: m.height - 2, W: max(1, ansi.StringWidth(label)), H: 1})
		}
		return strings.Join(rows, "\n"), hits
	}
	inner, visible := w-6, h-7
	lines := m.helpLines(inner)
	m.helpScroll = min(max(0, m.helpScroll), max(0, len(lines)-visible))
	rows := make([]string, h)
	for i := range rows {
		rows[i] = themed(strings.Repeat(" ", w), text, raised, false)
	}
	putRows(rows, 0, 0, themed("┌"+strings.Repeat("─", w-2)+"┐", border, raised, false), w)
	putRows(rows, 0, h-1, themed("└"+strings.Repeat("─", w-2)+"┘", border, raised, false), w)
	for i := 1; i < h-1; i++ {
		putRows(rows, 0, i, themed("│", border, raised, false), w)
		putRows(rows, w-1, i, themed("│", border, raised, false), w)
	}
	putRows(rows, 3, 1, themed(tr("help.title", pageTitle(m.page, m.locale)), text, raised, false), w)
	toggle := button("key-hints.toggle", m.keyHintsLabel(), inner)
	putRows(rows, 3, 2, toggle, w)
	for i := 0; i < visible && i+m.helpScroll < len(lines); i++ {
		putRows(rows, 3, i+4, themed(cellPad(lines[i+m.helpScroll], inner), text, raised, false), w)
	}
	label := button("help.close", tr("help.close"), inner)
	putRows(rows, 3, h-2, label, w)
	footerX := 3 + ansi.StringWidth(label) + 3
	if footerX < w-3 {
		putRows(rows, footerX, h-2, helpText(cellCrop(tr("help.footer"), w-footerX-3), raised, false), w)
	}
	return overlay(base, strings.Join(rows, "\n"), x, y, m.width, m.height), []hitRegion{
		{ID: "help.close", X: x + 3, Y: y + h - 2, W: ansi.StringWidth(label), H: 1},
		{ID: "key-hints.toggle", X: x + 3, Y: y + 2, W: ansi.StringWidth(toggle), H: 1},
	}
}

func (m Model) keyHintsLabel() string {
	if m.keyHints.hidden {
		return m.tr("help.keyHints.show")
	}
	return m.tr("help.keyHints.hide")
}
