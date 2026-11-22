package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Only the inspector handles input while it is open. Its viewport scrolls
// independently of the list, which continues to receive runtime observations.
func (m *Model) connectionDetailKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "left", "h", "q":
		return m.connectionActivate("conn.back")
	case "x":
		return m.connectionActivate("conn.close")
	case "enter", "space", " ":
		return m.connectionActivate(m.focus)
	case "tab", "shift+tab":
		if m.focus == "conn.back" && m.detailActive() && !m.busy {
			m.focus = "conn.close"
		} else {
			m.focus = "conn.back"
		}
	case "up", "down", "j", "k", "home", "end", "pgup", "pgdown":
		v := m.connections.detailViewport
		if v != nil {
			switch k.String() {
			case "home":
				v.GotoTop()
			case "end":
				v.GotoBottom()
			default:
				*v, _ = v.Update(k)
			}
		}
	}
	return nil
}

func (m *Model) connectionDialogMouse(event tea.MouseMsg) tea.Cmd {
	point := event.Mouse()
	inside := func(h hitRegion) bool {
		return point.X >= h.X && point.X < h.X+h.W && point.Y >= h.Y && point.Y < h.Y+h.H
	}
	_, clicked := event.(tea.MouseClickMsg)
	m.hover = ""
	if m.connections.detail != nil && !inside(m.connections.detailBounds) {
		if clicked && point.Button == tea.MouseLeft && !m.connections.confirming {
			return m.connectionActivate("conn.back")
		}
		return nil
	}
	if _, wheel := event.(tea.MouseWheelMsg); wheel {
		if v := m.connections.detailViewport; m.connections.detail != nil && v != nil {
			*v, _ = v.Update(event)
		}
		return nil
	}
	for _, hit := range m.targets {
		if inside(hit) {
			m.hover = hit.ID
			if clicked && point.Button == tea.MouseLeft {
				m.focus = hit.ID
				return m.connectionActivate(hit.ID)
			}
			break
		}
	}
	return nil
}

func (m *Model) connectionDetailLines(width int, active bool) []string {
	c := *m.connections.detail
	tr := m.tr
	status, statusColor := tr("connections.inactive_detail"), muted
	rate := connectionRate{Upload: -1, Download: -1}
	now := m.connectionObservedAt()
	if m.connections.detailHistorical {
		status = tr("connections.archived")
		now = m.connections.detailClosedAt
	} else if active {
		status, statusColor = tr("connections.active"), success
		rate = m.connections.rates.get(c.ID)
	}
	lines := strings.Split(wrapText(connectionDestination(c), width), "\n")
	lines = append(lines, strings.Split(wrapText(themed(status, statusColor, panel, false), width), "\n")...)
	if m.failure != "" && m.notification.operation.Key == notificationKey("connection.close", c.ID) {
		lines = append(lines, strings.Split(wrapText(themed(clean(m.failure), errorColor, panel, false), width), "\n")...)
	}
	if m.connections.confirming {
		lines = append(lines, "")
		lines = append(lines, strings.Split(wrapText(tr("connections.confirm_hint"), width), "\n")...)
	}
	lines = append(lines, "")
	labelWidth := max(ansi.StringWidth(tr("connections.network_inbound")), ansi.StringWidth(tr("connections.closed_observed")))
	field := func(key, value string) {
		if width-labelWidth < 10 {
			lines = append(lines, strings.Split(wrapText(themed(tr(key), muted, panel, false), width), "\n")...)
			lines = append(lines, strings.Split(wrapText(connectionValue(value), width), "\n")...)
			return
		}
		label := cellPad(tr(key), labelWidth)
		for i, part := range strings.Split(wrapText(connectionValue(value), width-labelWidth-2), "\n") {
			if i != 0 {
				label = strings.Repeat(" ", labelWidth)
			}
			lines = append(lines, themed(label, muted, panel, false)+"  "+part)
		}
	}
	md := c.Metadata
	field("connections.chain", connectionChain(c))
	field("connections.rule", connectionRule(c))
	field("connections.network_inbound", md.Network+" / "+md.Type)
	field("connections.process", md.Process)
	field("connections.source", joinAddress(md.SourceIP, md.SourcePort))
	field("connections.resolved_ip", md.DestinationIP)
	lines = append(lines, "")
	field("connections.download_speed", connectionSpeed(rate.Download))
	field("connections.upload_speed", connectionSpeed(rate.Upload))
	field("connections.download_total", formatBytes(c.Download))
	field("connections.upload_total", formatBytes(c.Upload))
	if m.connections.detailHistorical {
		lines = append(lines, strings.Split(wrapText(themed(tr("connections.archived_hint"), muted, panel, false), width), "\n")...)
	}
	lines = append(lines, "")
	field("connections.duration", connectionDuration(c, now))
	started := ""
	if !c.Start.IsZero() {
		started = c.Start.Local().Format(time.RFC3339)
	}
	field("connections.started", started)
	if !m.connections.detailClosedAt.IsZero() {
		field("connections.closed_observed", m.connections.detailClosedAt.Local().Format(time.RFC3339))
	}
	field("connections.process_path", md.ProcessPath)
	field("connections.id", c.ID)
	return lines
}

func (m *Model) renderConnectionDetail(base string) (string, []hitRegion) {
	if m.connections.detailViewport == nil {
		v := viewport.New()
		m.connections.detailViewport = &v
	}
	v := m.connections.detailViewport
	active := m.detailActive()
	if m.focus == "conn.close" && (!active || m.busy) {
		m.focus = "conn.back"
	}
	w := min(66, max(1, m.width-2))
	x := (m.width - w) / 2
	wide := m.width >= 100
	if wide {
		w = min(w, m.width/2)
		x = m.width - w - 2
	}
	if w < 18 || m.height < 9 {
		id, label := "conn.back", "×"
		if m.connections.confirming {
			id, label = "conn.cancel", m.tr("connections.cancel")
		}
		body := themed(cellPad(label, w), text, panel, false)
		y := m.height / 2
		m.connections.detailBounds = hitRegion{X: x, Y: y, W: w, H: 1}
		return overlay(base, body, x, y, m.width, m.height), []hitRegion{{ID: id, X: x, Y: y, W: w, H: 1}}
	}
	inner := w - 4
	document := m.connectionDetailLines(inner, active)
	// Six rows frame the scrolling fields: borders, title, separator,
	// and a fixed action row with one row of breathing room.
	h := min(len(document)+6, m.height-2)
	y := (m.height - h) / 2
	if wide {
		y = m.height - h - 2
	}
	m.connections.detailBounds = hitRegion{X: x, Y: y, W: w, H: h}
	v.SetWidth(inner)
	v.SetHeight(h - 6)
	v.SetContentLines(document)
	closeID, closeLabel := "conn.back", "×"
	title := m.tr("connections.details")
	if m.connections.confirming {
		closeID, closeLabel, title = "conn.cancel", m.tr("connections.cancel"), m.tr("connections.confirm_title")
	}
	button := func(id, label string, kind buttonKind) string {
		return renderButton(label, buttonOptions{Kind: kind, Surface: panel, Compact: true,
			Focused: m.focus == id, Hovered: m.hover == id, Plain: m.buttonPlain()})
	}
	closeButton := button(closeID, closeLabel, buttonText)
	closeWidth := ansi.StringWidth(closeButton)
	heading := cellPad(title, inner-closeWidth) + closeButton
	rows := []string{heading, themed(strings.Repeat("─", inner), divider, panel, false)}
	rows = append(rows, strings.Split(v.View(), "\n")...)
	rows = append(rows, "")
	hits := []hitRegion{{ID: closeID, X: x + w - 2 - closeWidth, Y: y + 1, W: closeWidth, H: 1}}
	arrow := ""
	if !v.AtTop() || !v.AtBottom() {
		arrow = "↓"
		if v.AtBottom() {
			arrow = "↑"
		} else if !v.AtTop() {
			arrow = "↕"
		}
	}
	footerWidth := inner - ansi.StringWidth(arrow)
	actionID, footer := "", ""
	if m.connections.confirming {
		actionID = "conn.confirm"
		footer = button(actionID, m.tr("connections.close"), buttonDangerConfirm)
	} else if active && !m.busy {
		actionID = "conn.close"
		footer = button(actionID, m.tr("connections.close_shortcut"), buttonDanger)
	}
	if actionID != "" {
		hits = append(hits, hitRegion{ID: actionID, X: x + 2, Y: y + h - 2, W: min(footerWidth, ansi.StringWidth(footer)), H: 1})
	}
	rows = append(rows, cellPad(footer, footerWidth)+themed(arrow, muted, panel, false))
	box := panelFrame(rows, w, h, dashboardOptions{}, border)
	return overlay(base, strings.Join(box, "\n"), x, y, m.width, m.height), hits
}
