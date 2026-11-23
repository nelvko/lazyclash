package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const proxyTypeTarget = "proxies.type."

// A motion event changes only hover styling. Existing clipped hit regions are
// already current, so it needs neither data reconciliation nor focus repair.
func (m *Model) updateProxyHover(event tea.MouseMotionMsg) bool {
	if m.page != "proxies" || m.menu.open || m.ports.open || m.helpOpen || m.wizard.open || m.wizard.networkOpen || m.form != "" || m.notification.panel.open {
		return false
	}
	previous := m.hover
	m.mouse(event)
	if m.hover != previous {
		m.renderLayout()
	}
	return true
}

// Anchor the hint to the visible label. A clipped group or a notification
// covering the label has no target in the finished layout.
func (m *Model) showProxyTypeHint(g dashboardLayout, notifications []notificationOverlay) {
	suffix, ok := strings.CutPrefix(m.hover, proxyTypeTarget)
	if !ok || m.page != "proxies" {
		return
	}
	gi, err := strconv.Atoi(suffix)
	if err != nil || gi < 0 || gi >= len(m.snapshot.Groups) {
		return
	}
	kind := proxyGroupConfigType(m.snapshot.Groups[gi].Type)
	if kind == "" {
		return
	}
	label := "type: " + kind
	if kind == "relay" {
		label = m.tr("proxies.type.deprecated", kind)
	}
	hint := " " + label + " "
	width := ansi.StringWidth(hint)
	if width > g.width {
		return
	}
	for _, hit := range m.targets {
		if hit.ID != m.hover {
			continue
		}
		x := min(hit.X, g.x+g.width-width)
		for _, y := range []int{hit.Y - 1, hit.Y + 1} {
			if y < g.y || y >= g.y+g.height {
				continue
			}
			bounds := hitRegion{X: x, Y: y, W: width, H: 1}
			covered := false
			for _, toast := range notifications {
				covered = covered || regionsOverlap(bounds, toast.Bounds)
			}
			if !covered {
				rows := strings.Split(m.screen, "\n")
				putRows(rows, x, y, themed(hint, text, raised, false), m.width)
				m.screen = strings.Join(rows, "\n")
				return
			}
		}
		return
	}
}
