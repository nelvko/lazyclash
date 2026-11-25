package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m *Model) renderConnectionsCloseAll(base string) (string, []hitRegion) {
	width := min(64, max(1, m.width-2))
	inner := max(1, width-2*modalPaddingX)
	button := func(id, label string, kind buttonKind) string {
		return renderButton(label, buttonOptions{Kind: kind, Surface: modalBackground, Compact: true,
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
		surface := m.newModalSurface(width, 1)
		surface.put(0, 0, cellCrop(m.tr("profiles.enlarge"), width))
		return surface.render(base), nil
	}
	surface := m.newModalSurface(width, height)
	for i, line := range rows {
		surface.put(modalPaddingX, 1+i, line)
	}
	actionX, actionY := modalPaddingX+inner-ansi.StringWidth(actions), height-2
	surface.put(actionX, actionY, actions)
	hits := []hitRegion{
		surface.screen(hitRegion{ID: "conn.cancel", X: actionX, Y: actionY, W: ansi.StringWidth(cancel), H: 1}),
		surface.screen(hitRegion{ID: "conn.confirm", X: actionX + ansi.StringWidth(cancel) + 1, Y: actionY, W: ansi.StringWidth(confirm), H: 1}),
	}
	return surface.render(base), hits
}
