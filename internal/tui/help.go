package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) helpLines(width int) []string {
	tr := m.tr
	lines := []string{tr("help.currentPage", pageTitle(m.page, m.locale))}
	switch m.page {
	case "proxies":
		if m.proxiesDirect() {
			lines = append(lines, tr("proxies.direct.description"), tr("navigation.proxies.directBrowse"))
		} else {
			lines = append(lines, tr("help.proxies.expand"), tr("help.proxies.options"), tr("help.proxies.tools"))
		}
	case "profiles":
		lines = append(lines, tr("help.profiles.add"), tr("help.profiles.import"), tr("help.profiles.rename"), tr("help.profiles.click"), tr("help.profiles.menu"), tr("help.profiles.menuControls"), tr("help.profiles.grid"), tr("help.profiles.actions"), tr("help.profiles.update"), tr("help.profiles.overrides"), tr("help.profiles.delete"))
	case "connections":
		lines = append(lines, tr("help.connections.keys"), tr("help.connections.search"), tr("help.connections.close"), tr("help.connections.confirm"), tr("help.search.input"))
	case "rules":
		lines = append(lines, tr("help.rules.keys"), tr("help.rules.proxy"), tr("help.rules.search"), tr("help.search.input"), tr("help.rules.readOnly"))
	case "settings":
		lines = append(lines, tr("help.settings.keys"), tr("help.settings.ports"), tr("help.settings.review"), tr("help.settings.persistence"), tr("help.settings.tun"))
	case "logs":
		lines = append(lines, tr("help.logs.keys"), tr("help.logs.levelMenu"), tr("help.logs.search"), tr("help.search.input"), tr("help.logs.liveOnly"), tr("help.logs.retention"))
	default:
		lines = append(lines, tr("help.overview.keys"), tr("help.overview.controls"), tr("help.overview.install"), tr("help.overview.import"), tr("help.overview.refresh"), tr("help.overview.unavailable"))
	}
	lines = append(lines, "", tr("help.core.title"))
	switch m.page {
	case "connections":
		lines = append(lines, tr("help.hints.connectionsCore"))
	case "proxies":
		lines = append(lines, tr("help.core.proxies"))
	case "overview":
		lines = append(lines, tr("help.core.overview"), tr("help.core.refresh"))
	default:
		lines = append(lines, tr("help.core.actions"))
		if m.page != "rules" && m.page != "logs" {
			lines = append(lines, tr("help.core.refresh"))
		}
	}
	lines = append(lines, "", tr("help.navigation"), tr("help.navigation.keys"), tr("help.pages.first"), tr("help.pages.second"), "", tr("help.navigation.tab"), tr("help.navigation.arrows"), tr("help.navigation.scroll"), tr("help.navigation.cancel"), tr("help.keyHints.shortcut"), tr("help.controls"), tr("help.core.quit"), "", tr("help.forms.title"), tr("help.forms.keys"), tr("help.forms.input"), tr("help.forms.modal"), "", tr("help.messages.title"), tr("help.messages.toast"), tr("help.messages.dismiss"), tr("help.messages.controls"))
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(wrapText(line, max(1, width)), "\n")...)
	}
	return wrapped
}

func (m *Model) helpKey(k tea.KeyPressMsg) tea.Cmd {
	if m.keyHints.helpFocus == "key-hints.toggle" {
		switch k.String() {
		case "enter", "space", " ":
			if k.IsRepeat {
				return nil
			}
			return m.toggleKeyHints()
		}
	}
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
	k := event.Mouse()
	m.hover = ""
	for _, hit := range m.targets {
		if k.X >= hit.X && k.X < hit.X+hit.W && k.Y >= hit.Y && k.Y < hit.Y+hit.H {
			m.hover = hit.ID
			if _, clicked := event.(tea.MouseClickMsg); clicked && k.Button == tea.MouseLeft {
				m.keyHints.helpFocus = hit.ID
				_, cmd := m.keyHintsAction(hit.ID)
				return cmd
			}
			break
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
			hits = m.putHelpSwitch(rows, hits, 0, m.height-2, m.width)
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
	hits := m.putHelpSwitch(rows, nil, 3, 2, inner)
	for i := 0; i < visible && i+m.helpScroll < len(lines); i++ {
		putRows(rows, 3, i+4, themed(cellPad(lines[i+m.helpScroll], inner), text, raised, false), w)
	}
	label := button("help.close", tr("help.close"), inner)
	putRows(rows, 3, h-2, label, w)
	footerX := 3 + ansi.StringWidth(label) + 3
	if footerX < w-3 {
		putRows(rows, footerX, h-2, helpText(cellCrop(tr("help.footer"), w-footerX-3), raised, false), w)
	}
	for i := range hits {
		hits[i].X += x
		hits[i].Y += y
	}
	closeHit := hitRegion{ID: "help.close", X: x + 3, Y: y + h - 2, W: ansi.StringWidth(label), H: 1}
	return overlay(base, strings.Join(rows, "\n"), x, y, m.width, m.height), append([]hitRegion{closeHit}, hits...)
}

func (m *Model) putHelpSwitch(rows []string, hits []hitRegion, x, y, width int) []hitRegion {
	const id = "key-hints.toggle"
	o := m.dashboardOptions()
	o.Dim = false
	control := o.settingsToggle(id)
	focused, hovered := m.keyHints.helpFocus == id, m.hover == id
	face := o.switchFace(id, control, toggleOptions{Surface: raised, Focused: focused, Hovered: hovered})
	gutter := ""
	if width >= switchWidth+2 {
		gutter = switchGutter(focused, hovered, o.Monochrome, raised, false)
	}
	putRows(rows, x, y, cellCrop(gutter+face+" "+themed(m.tr("dashboard.interface.keyHints"), text, raised, false), width), m.width)
	if control.canToggle() {
		left := ansi.StringWidth(gutter)
		hits = append(hits, hitRegion{ID: id, X: x + left, Y: y, W: min(switchWidth, width-left), H: 1})
	}
	return hits
}
