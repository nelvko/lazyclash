package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// renderTooltip paints beside the visible anchor, leaving its own row exposed
// so the pointer can stay on the icon. Notifications keep their foreground.
func (m *Model) renderTooltip(content string, anchor hitRegion, bottom int, notifications []notificationOverlay) (string, hitRegion) {
	edge, background := 0, tooltipBackground
	if m.lowColor() {
		edge, background = 1, raised
	}
	inset := 1 + edge // Always leave one padding cell between the text and either side.
	contentWidth := min(lipgloss.Width(content), 48, m.width-2*inset)
	bottom = min(bottom, m.height)
	anchor, visible := clipRegion(anchor, 0, 0, m.width, bottom)
	if !visible || contentWidth < 1 {
		return m.screen, hitRegion{}
	}

	width := contentWidth + 2*inset
	x := min(anchor.X, m.width-width)
	anchorBottom := anchor.Y + anchor.H
	top := 0
	for _, card := range notifications {
		r := card.Bounds
		if regionsOverlap(anchor, r) {
			return m.screen, hitRegion{}
		}
		if r.X >= x+width || r.X+r.W <= x {
			continue
		}
		if r.Y < anchor.Y {
			top = max(top, min(anchor.Y, r.Y+r.H))
		}
		if r.Y+r.H > anchorBottom {
			bottom = min(bottom, max(anchorBottom, r.Y))
		}
	}

	lines := strings.Split(lipgloss.Wrap(content, contentWidth, "/"), "\n")
	above, below := anchor.Y-top, bottom-anchorBottom
	bodyRows := min(len(lines), max(above, below)-2*edge)
	if bodyRows < 1 {
		return m.screen, hitRegion{}
	}
	height := bodyRows + 2*edge
	y := anchorBottom
	if height > below {
		y = anchor.Y - height
	}
	bounds := hitRegion{X: x, Y: y, W: width, H: height}
	surface := m.modalSurfaceAt(bounds, background, edge != 0)
	for i, line := range lines[:bodyRows] {
		if i == bodyRows-1 && len(lines) > bodyRows {
			line += "…"
		}
		surface.put(inset, i+edge, cellCrop(line, contentWidth))
	}
	return surface.render(m.screen), bounds
}
