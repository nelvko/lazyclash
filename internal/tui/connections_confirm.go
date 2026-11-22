package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m *Model) renderConnectionsCloseAll(base string) (string, []hitRegion) {
	width := min(64, max(1, m.width-2))
	inner := max(1, width-4)
	button := func(id, label string, kind buttonKind) string {
		return renderButton(label, buttonOptions{Kind: kind, Surface: panel, Compact: true,
			Focused: m.focus == id, Hovered: m.hover == id, Plain: m.buttonPlain()})
	}
	cancel := button("conn.cancel", m.tr("connections.cancel"), buttonSecondary)
	confirm := button("conn.confirm", m.tr("connections.close_all"), buttonDangerConfirm)
	actions := cancel + " " + confirm
	rows := strings.Split(wrapText(m.tr("connections.confirm_all_title"), inner), "\n")
	rows = append(rows, "")
	rows = append(rows, strings.Split(wrapText(m.tr("connections.confirm_all_hint"), inner), "\n")...)
	rows = append(rows, "")
	height := len(rows) + 3
	if height > m.height || ansi.StringWidth(actions) > inner {
		notice := themed(cellPad(m.tr("profiles.enlarge"), width), text, panel, false)
		return overlay(base, notice, (m.width-width)/2, m.height/2, m.width, m.height), nil
	}
	left := inner - ansi.StringWidth(actions)
	rows = append(rows, strings.Repeat(" ", left)+actions)
	box := panelFrame(rows, width, height, dashboardOptions{}, border)
	x, y := (m.width-width)/2, (m.height-height)/2
	actionX, actionY := x+2+left, y+height-2
	hits := []hitRegion{
		{ID: "conn.cancel", X: actionX, Y: actionY, W: ansi.StringWidth(cancel), H: 1},
		{ID: "conn.confirm", X: actionX + ansi.StringWidth(cancel) + 1, Y: actionY, W: ansi.StringWidth(confirm), H: 1},
	}
	return overlay(base, strings.Join(box, "\n"), x, y, m.width, m.height), hits
}
