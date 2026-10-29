package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m Model) helpLines(width int) []string {
	lines := []string{"Navigation", "1 Overview · 2 Proxies · 3 Profiles", "4 Connections · 5 Rules · 6 Logs · 7 Settings", "", "Tab / Shift+Tab: move between navigation and visible controls", "Arrows: choose within navigation or a list; Enter activates", "PgUp / PgDn / Home / End: scroll the content", "Esc: back or close; Ctrl+C: cancel safely and quit", "", "Current page: " + pageTitle(m.page)}
	switch m.page {
	case "proxies":
		lines = append(lines, "Enter a proxy group to expand its choices. Enter an option in a Selector group to apply it.", "DIRECT and REJECT are valid choices; automatic groups choose their own option.")
	case "profiles":
		lines = append(lines, "a: add a subscription URL or import a local YAML file", "Enter a saved profile to validate and apply it.", "Profiles includes subscriptions, local files and the built-in default.")
	case "connections":
		lines = append(lines, "/: search · Enter: details · x: review closing one connection", "Closing requires confirmation of the displayed connection; other connections stay active.")
	case "rules":
		lines = append(lines, "/: search · Enter: details · g / Ctrl+R: refresh", "Rules are read only and remain in the core's matching order.")
	case "settings":
		lines = append(lines, "Runtime settings / Ports: edit local proxy and control API ports.", "Review changed listeners and restart impact before applying; existing API credentials remain private.", "Routing mode and autostart use the same persistent settings as the CLI.", "TUN requires privileged system integration and is unavailable in this user service.")
	case "logs":
		lines = append(lines, "f: pause/resume · End: follow · /: search · g: reconnect", "Only live logs are available. Pausing disconnects; events during a pause are not retained.", "The view retains at most 200 redacted entries in memory. No raw log history is written.")
	default:
		lines = append(lines, "i: install/manage core · b: install a local executable", "n: download network settings · a: add/import profile")
	}
	lines = append(lines, "", "Core and configuration", "s: start · x: stop · r: restart (Connections uses x for one connection)", "g / Ctrl+R: refresh current data", "Quitting closes this interface; systemd continues to own Mihomo.", "", "Forms and dialogs", "Tab / Shift+Tab: change controls · Enter: activate · Esc: back", "Printable keys, including q and 1–7, belong to the active input.", "Modal dialogs capture input and restore the previous page on close.", "", "Messages", "Progress stays visible while work runs. Errors remain until the next action or refresh.", "Installation and network configuration use the same application use cases as the CLI.")
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(wrapText(line, max(1, width)), "\n")...)
	}
	return wrapped
}

func (m *Model) helpKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "?":
		m.helpOpen = false
	case "q":
		m.quitting = true
		return tea.Quit
	case "down", "j":
		m.helpScroll++
	case "up", "k":
		m.helpScroll = max(0, m.helpScroll-1)
	case "pgdown", "space", " ":
		m.helpScroll += max(1, min(28, m.height-2)-6)
	case "pgup":
		m.helpScroll = max(0, m.helpScroll-max(1, min(28, m.height-2)-6))
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
			if hit.ID == "help.close" && k.X >= hit.X && k.X < hit.X+hit.W && k.Y == hit.Y {
				m.helpOpen = false
			}
		}
	}
	return nil
}

func (m *Model) renderHelp(base string) (string, []hitRegion) {
	w, h := min(82, m.width), min(28, m.height)
	x, y := (m.width-w)/2, (m.height-h)/2
	if w < 26 || h < 8 {
		return overlay("", themed(cellCrop("Help · Esc Close · Ctrl+C Quit", w), text, raised, false), 0, 0, m.width, m.height), nil
	}
	inner, visible := w-6, h-6
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
	putRows(rows, 3, 1, themed("Help / "+pageTitle(m.page), text, raised, false), w)
	for i := 0; i < visible && i+m.helpScroll < len(lines); i++ {
		putRows(rows, 3, i+3, themed(cellPad(lines[i+m.helpScroll], inner), text, raised, false), w)
	}
	label := "[ Esc Close ]"
	putRows(rows, 3, h-2, themed(label, focus, raised, false), w)
	putRows(rows, 19, h-2, helpText(cellCrop("↑↓ Scroll · q Quit", max(1, w-22)), raised, false), w)
	return overlay(base, strings.Join(rows, "\n"), x, y, m.width, m.height), []hitRegion{{ID: "help.close", X: x + 3, Y: y + h - 2, W: len(label), H: 1}}
}
