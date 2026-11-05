package tui

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type connectionsState struct {
	search                      textinput.Model
	editing                     bool
	selected                    string
	selectedIndex, detailScroll int
	detail                      *mihomo.Connection
	confirming                  bool
	sort                        connectionSort
	rates                       connectionRatesState
	paused                      bool
	frozen                      *mihomo.ConnectionSnapshot
	frozenRates                 connectionRatesState
	frozenAt                    time.Time
	frozenPID                   int
	frozenVersion               string
}

func (m *Model) observeConnections(at time.Time) {
	snapshot := m.runtime.snapshot
	if m.connections.paused {
		stale := snapshot.State != "ready" || snapshot.Connections == nil || m.connections.frozenPID != snapshot.Service.PID || m.connections.frozenVersion != snapshot.RunningVersion
		if !stale && m.connections.frozen != nil {
			stale = snapshot.Connections.UploadTotal < max(m.connections.frozen.UploadTotal, m.connections.rates.uploadTotal) || snapshot.Connections.DownloadTotal < max(m.connections.frozen.DownloadTotal, m.connections.rates.downloadTotal)
		}
		if stale {
			m.connections.paused, m.connections.frozen = false, nil
			m.connections.frozenRates = connectionRatesState{}
		}
	}
	m.connections.rates.observe(snapshot, at)
	m.reconcileConnections()
}

// Pause freezes this page's observation; runtime monitoring and exact-ID close
// checks continue to use the live core snapshot.
func (m Model) connectionSnapshot() *mihomo.ConnectionSnapshot {
	if !m.runtime.received || m.runtime.snapshot.State != "ready" || m.runtime.snapshot.Connections == nil {
		return nil
	}
	if m.connections.paused {
		return m.connections.frozen
	}
	return m.runtime.snapshot.Connections
}

func (m Model) displayedConnectionRates() connectionRatesState {
	if m.connections.paused {
		return m.connections.frozenRates
	}
	return m.connections.rates
}

func (m Model) filteredConnections() []mihomo.Connection {
	var out []mihomo.Connection
	snapshot := m.connectionSnapshot()
	if snapshot == nil {
		return out
	}
	query := strings.ToLower(strings.TrimSpace(m.connections.search.Value()))
	for _, c := range snapshot.Connections {
		md := c.Metadata
		haystack := strings.Join([]string{c.ID, md.Host, md.SourceIP, md.SourcePort, md.DestinationIP, md.DestinationPort, md.Network, md.Type, md.Process, md.ProcessPath, c.Rule, c.RulePayload, strings.Join(c.Chains, " ")}, " ")
		if query == "" || strings.Contains(strings.ToLower(haystack), query) {
			out = append(out, c)
		}
	}
	sortConnections(out, m.connections.sort, m.displayedConnectionRates())
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
		if k.String() == "tab" || k.String() == "shift+tab" {
			m.focus = "conn.search"
			direction := 1
			if k.String() == "shift+tab" {
				direction = -1
			}
			m.cycleFocus(direction)
			return nil
		}
		m.focus = "connection." + m.connections.selected
		if m.connections.selected == "" {
			m.focus = "conn.search"
		}
		m.revealConnection(0)
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
	case "p", "s":
		if m.connections.detail == nil {
			id := "conn.pause"
			if key == "s" {
				id = "conn.sort"
			}
			return true, m.connectionActivate(id)
		}
		return true, nil
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
			m.revealConnection(0)
			return true, nil
		}
	case "down", "j", "up", "k", "home", "end", "pgup", "pgdown":
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
			g := dashboardGeometry(m.dashboardOptions())
			page := max(1, g.height)
			if g.width < 100 {
				page = max(1, page/2)
			}
			switch key {
			case "up", "k":
				delta = -1
			case "pgup":
				delta = -page
			case "pgdown":
				delta = page
			case "home":
				delta = -len(list)
			case "end":
				delta = len(list)
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
	case id == "conn.clear":
		m.connections.search.Reset()
		m.scroll = 0
		m.reconcileConnections()
		return m.focusConnectionSearch()
	case id == "conn.sort":
		m.connections.sort = (m.connections.sort + 1) % connectionSortCount
		m.reconcileConnections()
		m.revealConnection(0)
	case id == "conn.pause":
		if m.connections.paused {
			m.connections.paused, m.connections.frozen = false, nil
			m.connections.frozenRates = connectionRatesState{}
		} else if snapshot := m.connectionSnapshot(); snapshot != nil {
			copy := *snapshot
			copy.Connections = append([]mihomo.Connection(nil), snapshot.Connections...)
			for i := range copy.Connections {
				copy.Connections[i].Chains = append([]string(nil), copy.Connections[i].Chains...)
			}
			m.connections.frozen = &copy
			m.connections.frozenRates = m.connections.rates
			m.connections.frozenAt = m.connectionNow()
			m.connections.frozenPID = m.runtime.snapshot.Service.PID
			m.connections.frozenVersion = m.runtime.snapshot.RunningVersion
			m.connections.paused = true
		}
		m.reconcileConnections()
		m.revealConnection(0)
	case id == "conn.retry":
		return m.retryRuntime()
	case id == "conn.back":
		m.connections.detail = nil
		m.connections.confirming = false
		m.scroll = m.connections.detailScroll
		m.focus = "connection." + m.connections.selected
		m.reconcileConnections()
		m.revealConnection(0)
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
