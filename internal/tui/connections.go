package tui

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type connectionsState struct {
	search                      textinput.Model
	editing                     bool
	selected                    string
	selectedIndex, detailScroll int
	detail                      *mihomo.Connection
	confirming                  bool
}

func (m Model) filteredConnections() []mihomo.Connection {
	var out []mihomo.Connection
	if !m.runtime.received || m.runtime.snapshot.State != "ready" || m.runtime.snapshot.Connections == nil {
		return out
	}
	query := strings.ToLower(strings.TrimSpace(m.connections.search.Value()))
	for _, c := range m.runtime.snapshot.Connections.Connections {
		md := c.Metadata
		haystack := strings.Join([]string{c.ID, md.Host, md.SourceIP, md.SourcePort, md.DestinationIP, md.DestinationPort, md.Network, md.Type, md.Process, md.ProcessPath, c.Rule, c.RulePayload, strings.Join(c.Chains, " ")}, " ")
		if query == "" || strings.Contains(strings.ToLower(haystack), query) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start.Equal(out[j].Start) {
			return out[i].ID < out[j].ID
		}
		return out[i].Start.After(out[j].Start)
	})
	return out
}
func (m *Model) reconcileConnections() {
	previousIndex := m.connections.selectedIndex
	list := m.filteredConnections()
	found := false
	for index, c := range list {
		if c.ID == m.connections.selected {
			m.connections.selectedIndex, found = index, true
			break
		}
	}
	if !found {
		m.connections.selected = ""
		if len(list) > 0 {
			m.connections.selectedIndex = min(m.connections.selectedIndex, len(list)-1)
			m.connections.selected = list[m.connections.selectedIndex].ID
		}
	}
	if m.page == "connections" && m.connections.detail == nil && strings.HasPrefix(m.focus, "connection.") {
		previous := m.focus
		m.focus = "content"
		if m.connections.selected != "" {
			m.focus = "connection." + m.connections.selected
			if previous != m.focus || previousIndex != m.connections.selectedIndex {
				m.revealConnection(0)
			}
		}
	}
	if m.connections.detail != nil && m.runtime.snapshot.Connections != nil {
		for _, c := range m.runtime.snapshot.Connections.Connections {
			if c.ID == m.connections.detail.ID {
				copy := c
				m.connections.detail = &copy
				break
			}
		}
	}
}
func (m Model) detailActive() bool {
	if m.connections.detail == nil || m.runtime.snapshot.State != "ready" || m.runtime.snapshot.Connections == nil || !m.runtime.received {
		return false
	}
	for _, c := range m.runtime.snapshot.Connections.Connections {
		if c.ID == m.connections.detail.ID {
			return true
		}
	}
	return false
}
func (m *Model) focusConnectionSearch() tea.Cmd {
	m.connections.detail = nil
	m.connections.confirming = false
	m.connections.editing = true
	m.focus = "conn.search"
	m.scroll = 0
	return m.connections.search.Focus()
}
func (m *Model) connectionSearchKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "enter", "esc", "tab", "shift+tab":
		m.connections.editing = false
		m.connections.search.Blur()
		m.reconcileConnections()
		m.focus = "connection." + m.connections.selected
		if m.connections.selected == "" {
			m.focus = "conn.search"
		}
		return nil
	}
	var cmd tea.Cmd
	m.connections.search, cmd = m.connections.search.Update(k)
	m.scroll = 0
	m.reconcileConnections()
	return cmd
}
func (m *Model) connectionKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	key := k.String()
	if m.connections.confirming {
		switch key {
		case "esc", "n":
			m.connections.confirming = false
			m.focus = "conn.close"
		case "y":
			return true, m.connectionActivate("conn.confirm")
		case "tab", "shift+tab":
			if m.focus == "conn.cancel" {
				m.focus = "conn.confirm"
			} else {
				m.focus = "conn.cancel"
			}
		case "enter", "space", " ":
			return true, m.connectionActivate(m.focus)
		}
		return true, nil
	}
	switch key {
	case "/":
		return true, m.focusConnectionSearch()
	case "x":
		if m.connections.detail == nil {
			m.openSelectedConnection()
		}
		return true, m.connectionActivate("conn.close")
	case "esc":
		if m.connections.detail != nil {
			m.connections.detail = nil
			m.scroll = m.connections.detailScroll
			m.focus = "connection." + m.connections.selected
			m.reconcileConnections()
			return true, nil
		}
	case "down", "j", "up", "k":
		if m.connections.detail == nil {
			list := m.filteredConnections()
			if len(list) == 0 {
				return true, nil
			}
			index := 0
			for i, c := range list {
				if c.ID == m.connections.selected {
					index = i
					break
				}
			}
			delta := 1
			if key == "up" || key == "k" {
				delta = -1
			}
			index = max(0, min(len(list)-1, index+delta))
			m.connections.selectedIndex = index
			m.connections.selected = list[index].ID
			m.focus = "connection." + list[index].ID
			m.revealConnection(index)
			return true, nil
		}
	case "g", "ctrl+r":
		return true, m.retryRuntime()
	}
	return false, nil
}
func (m *Model) revealConnection(_ int) {
	g := dashboardGeometry(m.dashboardOptions())
	_, hits := m.connectionsBody(g.width)
	height := max(1, g.height)
	for _, hit := range hits {
		if hit.ID != "connection."+m.connections.selected {
			continue
		}
		if hit.Y < m.scroll {
			m.scroll = hit.Y
		}
		if hit.Y+hit.H > m.scroll+height {
			m.scroll = max(0, hit.Y+hit.H-height)
		}
		break
	}
}
func (m *Model) openSelectedConnection() {
	for index, c := range m.filteredConnections() {
		if c.ID == m.connections.selected {
			m.connections.selectedIndex, m.connections.detailScroll = index, m.scroll
			copy := c
			m.connections.detail = &copy
			m.connections.confirming = false
			m.scroll = 0
			m.focus = "conn.back"
			return
		}
	}
}
func (m *Model) connectionActivate(id string) tea.Cmd {
	if id != "conn.search" {
		m.connections.editing = false
		m.connections.search.Blur()
	}
	switch {
	case strings.HasPrefix(id, "connection."):
		m.connections.selected = strings.TrimPrefix(id, "connection.")
		m.openSelectedConnection()
	case id == "conn.search":
		return m.focusConnectionSearch()
	case id == "conn.retry":
		return m.retryRuntime()
	case id == "conn.back":
		m.connections.detail = nil
		m.connections.confirming = false
		m.scroll = m.connections.detailScroll
		m.focus = "connection." + m.connections.selected
		m.reconcileConnections()
	case id == "conn.close":
		if m.detailActive() {
			m.connections.confirming = true
			m.scroll = 0
			m.focus = "conn.cancel"
		}
	case id == "conn.cancel":
		m.connections.confirming = false
		m.focus = "conn.close"
	case id == "conn.confirm":
		if !m.connections.confirming || m.connections.detail == nil {
			return nil
		}
		if !m.detailActive() {
			id := m.connections.detail.ID
			m.prepareNotification(notificationKey("connection.close", id), m.tr("connections.closing")+" · "+id)
			m.connections.confirming = false
			m.focus = "conn.back"
			m.failure = m.tr("connections.no_longer_active")
			return nil
		}
		id := m.connections.detail.ID
		m.connections.confirming = false
		m.focus = "conn.back"
		return m.begin(m.tr("connections.closing"), m.tr("connections.close_requested"), func(ctx context.Context) error { return m.backend.CloseConnection(ctx, id) }, notificationOperation{Key: notificationKey("connection.close", id), Context: m.tr("connections.closing") + " · " + id})
	}
	return nil
}
func (m Model) connectionsBody(width int) (string, []hitRegion) {
	var lines []string
	var hits []hitRegion
	addText := func(s string, color string) {
		lines = append(lines, strings.Split(wrapText(themed(clean(s), color, bg, false), max(1, width)), "\n")...)
	}
	addButton := func(id, label string) {
		options := dashboardButtonOptions(id, m.navigationOptions(), false, bg)
		options.Disabled = m.busy
		if id == "conn.close" {
			options.Kind = buttonDanger
		} else if id == "conn.confirm" {
			options.Kind = buttonDangerConfirm
		}
		s := renderButton(label, options)
		if !options.Disabled {
			hits = append(hits, hitRegion{ID: id, Y: len(lines), W: min(width, ansi.StringWidth(s)), H: 1})
		}
		lines = append(lines, cellCrop(s, width))
	}
	if c := m.connections.detail; c != nil {
		addText(m.tr("connections.details"), text)
		if m.detailActive() {
			addText(m.tr("connections.active"), text)
		} else {
			addText(m.tr("connections.inactive_detail"), warning)
		}
		lines = append(lines, "")
		if m.connections.confirming {
			addText(m.tr("connections.confirm_title"), warning)
			addText(m.tr("connections.confirm_hint"), muted)
			addButton("conn.cancel", m.tr("connections.cancel"))
			addButton("conn.confirm", m.tr("connections.close"))
		} else {
			addButton("conn.back", m.tr("connections.back"))
			if m.detailActive() {
				addButton("conn.close", m.tr("connections.close_shortcut"))
			}
		}

		md := c.Metadata
		fields := [][2]string{{m.tr("connections.id"), c.ID}, {m.tr("connections.destination"), connectionDestination(*c)}, {m.tr("connections.resolved_ip"), md.DestinationIP}, {m.tr("connections.source"), joinAddress(md.SourceIP, md.SourcePort)}, {m.tr("connections.network_inbound"), md.Network + " / " + md.Type}, {m.tr("connections.process"), md.Process}, {m.tr("connections.process_path"), md.ProcessPath}, {m.tr("connections.rule"), c.Rule + " " + c.RulePayload}, {m.tr("connections.chain"), strings.Join(c.Chains, " → ")}, {m.tr("connections.started"), c.Start.Local().Format(time.RFC3339)}, {m.tr("connections.transfer"), formatBytes(c.Upload) + " / " + formatBytes(c.Download)}}
		for _, f := range fields {
			value := strings.TrimSpace(f[1])
			if value == "" {
				value = "--"
			}
			addText(f[0]+": "+value, text)
		}
		return strings.Join(lines, "\n"), hits
	}
	addText(m.tr("connections.title"), text)
	status := runtimeStatus(m.runtime.snapshot, m.runtime.received, m.runtime.pending, m.snapshot.Installed, m.locale)
	addText(status, muted)
	if m.connections.editing {
		m.connections.search.Placeholder = m.tr("connections.search_placeholder")
		m.connections.search.SetWidth(max(1, width-2))
		addText(m.tr("connections.search_edit"), accent)
		hits = append(hits, hitRegion{ID: "conn.search", Y: len(lines), W: width, H: 1})
		lines = append(lines, cellCrop(m.connections.search.View(), width))
	} else {
		addButton("conn.search", m.tr("connections.search", clean(m.connections.search.Value())))
	}
	addText(m.tr("connections.shortcuts"), muted)
	lines = append(lines, "")
	if err := m.runtime.snapshot.Error; err != "" {
		addText(m.locale.Diagnostic(err, m.runtime.snapshot.Diagnostic), errorColor)
	}
	if err := m.runtime.snapshot.ConnectionsError; err != "" {
		addText(m.locale.Diagnostic(err, m.runtime.snapshot.ConnectionsDiagnostic), errorColor)
	}
	list := m.filteredConnections()
	if len(list) == 0 {
		empty := m.tr("connections.empty")
		if m.connections.search.Value() != "" {
			empty = m.tr("connections.no_matches")
		}
		if m.runtime.snapshot.Connections == nil {
			empty = m.tr("connections.unavailable_hint")
		}
		addText(empty, muted)
	}
	for _, c := range list {
		prefix := "  "
		if c.ID == m.connections.selected {
			prefix = "› "
		}
		row := len(lines)
		foreground := text
		if m.focus == "connection."+c.ID {
			foreground = focus
		}
		// Each item is exactly two clipped rows; detailed long values remain available in Details.
		lines = append(lines, themed(cellCrop(prefix+connectionDestination(c)+" · "+clean(c.Metadata.Network), width), foreground, bg, false))
		process := c.Metadata.Process
		if process == "" {
			process = "--"
		}
		lines = append(lines, themed(cellCrop("  "+clean(process)+" · ↑ "+formatBytes(c.Upload)+" ↓ "+formatBytes(c.Download)+" · "+clean(c.Rule), width), muted, bg, false))
		hits = append(hits, hitRegion{ID: "connection." + c.ID, Y: row, W: width, H: 2})
	}
	return strings.Join(lines, "\n"), hits
}
func connectionDestination(c mihomo.Connection) string {
	host := c.Metadata.Host
	if host == "" {
		host = c.Metadata.DestinationIP
	}
	return joinAddress(host, c.Metadata.DestinationPort)
}
func joinAddress(host, port string) string {
	if host == "" {
		return "--"
	}
	if port == "" || port == "0" {
		return clean(host)
	}
	return clean(net.JoinHostPort(host, port))
}
func formatBytes(n int64) string {
	if n < 0 {
		return "--"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
