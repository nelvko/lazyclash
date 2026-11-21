package tui

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type connectionsState struct {
	search                          textinput.Model
	editing                         bool
	caseSensitive, wholeWord, regex bool

	selected                    string
	selectedIndex, detailScroll int
	detail                      *mihomo.Connection
	confirming                  bool
	closeAll                    bool
	closeAllScroll              int
	closedView                  bool
	history                     connectionHistoryState
	frozenClosed                []closedConnection
	detailHistorical            bool
	detailClosedAt              time.Time
	detailGeneration            uint64
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
	if observed := m.runtime.observed[app.RuntimeConnections]; !observed.IsZero() {
		snapshot.ObservedAt = observed
	}
	generation := m.connections.history.generation
	m.connections.history.observe(snapshot, at, 0, m.runtime.installation)
	if m.connections.paused {
		stale := generation != m.connections.history.generation || snapshot.State != "ready" || snapshot.Connections == nil || m.connections.frozenPID != snapshot.Service.PID || m.connections.frozenVersion != snapshot.RunningVersion
		if !stale && m.connections.frozen != nil {
			stale = snapshot.Connections.UploadTotal < max(m.connections.frozen.UploadTotal, m.connections.rates.uploadTotal) || snapshot.Connections.DownloadTotal < max(m.connections.frozen.DownloadTotal, m.connections.rates.downloadTotal)
		}
		if stale {
			m.connections.paused, m.connections.frozen = false, nil
			m.connections.frozenClosed = nil
			m.connections.frozenRates = connectionRatesState{}
		}
	}
	m.connections.rates.observe(snapshot, at)
	m.reconcileConnections()
}

// Pause freezes this page's observation; runtime monitoring and exact-ID close
// checks continue to use the live core snapshot.
func (m *Model) connectionSnapshot() *mihomo.ConnectionSnapshot {
	if !m.runtime.received || m.runtime.snapshot.State != "ready" || m.runtime.snapshot.Connections == nil {
		return nil
	}
	if m.connections.paused {
		return m.connections.frozen
	}
	return m.runtime.snapshot.Connections
}

func (m *Model) displayedConnectionRates() connectionRatesState {
	if m.connections.paused {
		return m.connections.frozenRates
	}
	return m.connections.rates
}

func (m *Model) reconcileConnections() {
	previousIndex := m.connections.selectedIndex
	list := m.filteredConnections()
	found := false
	for index, c := range list {
		if m.connectionRowKey(c) == m.connections.selected {
			m.connections.selectedIndex, found = index, true
			break
		}
	}
	if !found {
		m.connections.selected = ""
		if len(list) > 0 {
			m.connections.selectedIndex = min(m.connections.selectedIndex, len(list)-1)
			m.connections.selected = m.connectionRowKey(list[m.connections.selectedIndex])
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
		if m.connections.detailHistorical || m.connections.detailGeneration != m.connections.history.generation {
			return
		}
		for _, c := range m.runtime.snapshot.Connections.Connections {
			if c.ID == m.connections.detail.ID && c.Start.Equal(m.connections.detail.Start) {
				copy := c
				m.connections.detail = &copy
				break
			}
		}
		if entry, ok := m.connections.history.find(*m.connections.detail); ok {
			m.connections.detailHistorical = true
			m.connections.detailClosedAt = entry.ClosedAt
		}
	}
}
func (m *Model) detailActive() bool {
	return m.connections.detail != nil && !m.connections.detailHistorical && m.connections.detailGeneration == m.connections.history.generation && m.connectionActive(*m.connections.detail)
}

func (m *Model) connectionActive(connection mihomo.Connection) bool {
	if m.runtime.snapshot.State != "ready" || m.runtime.snapshot.Connections == nil || !m.runtime.received {
		return false
	}
	for _, c := range m.runtime.snapshot.Connections.Connections {
		if c.ID == connection.ID && c.Start.Equal(connection.Start) {
			return true
		}
	}
	return false
}
func (m *Model) canCloseAllConnections() bool {
	return !m.busy && !m.quitting && m.runtime.received && m.runtime.snapshot.State == "ready" && m.runtime.snapshot.Connections != nil && len(m.runtime.snapshot.Connections.Connections) > 0
}

func (m *Model) canPauseConnections() bool {
	return !m.busy && !m.quitting && !m.connections.confirming && m.connections.detail == nil && (m.connections.paused || m.connectionSnapshot() != nil)
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
	if m.connectionFilterKey(k) {
		return nil
	}
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
			return true, m.connectionActivate("conn.cancel")
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
	if m.connectionFilterKey(k) {
		return true, nil
	}
	switch key {
	case "v":
		if m.connections.detail == nil {
			id := "conn.closed"
			if m.connections.closedView {
				id = "conn.active"
			}
			return true, m.connectionActivate(id)
		}
		return true, nil
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
		if m.connections.closedView || (m.connections.detail != nil && m.connections.detailHistorical) {
			return true, nil
		}
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
				switch key {
				case "home", "end", "pgup", "pgdown":
					// A short terminal may put the toolbar and empty state in
					// one scrollable document. Let the shell page through it.
					return false, nil
				}
				return true, nil
			}
			index := 0
			for i, c := range list {
				if m.connectionRowKey(c) == m.connections.selected {
					index = i
					break
				}
			}
			delta := 1
			g := dashboardGeometry(m.dashboardOptions())
			page := max(1, g.height)
			if g.width < connectionTableMinWidth {
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
			m.connections.selected = m.connectionRowKey(list[index])
			m.focus = "connection." + m.connectionRowKey(list[index])
			m.revealConnection(index)
			return true, nil
		}
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
		if m.connectionRowKey(c) == m.connections.selected {
			m.connections.selectedIndex, m.connections.detailScroll = index, m.scroll
			copy := c
			m.connections.detail = &copy
			m.connections.detailHistorical = m.connections.closedView
			m.connections.detailGeneration = m.connections.history.generation
			m.connections.detailClosedAt = time.Time{}
			if entry, ok := m.closedEntry(c); m.connections.closedView && ok {
				m.connections.detailClosedAt = entry.ClosedAt
			}
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
	case id == "conn.active" || id == "conn.closed":
		if m.connections.confirming || m.busy {
			return nil
		}
		m.connections.closedView = id == "conn.closed"
		m.connections.detail, m.connections.confirming = nil, false
		m.connections.selected, m.connections.selectedIndex = "", 0
		m.scroll, m.focus = 0, id
		m.reconcileConnections()
	case strings.HasPrefix(id, "connection."):
		m.connections.selected = strings.TrimPrefix(id, "connection.")
		m.openSelectedConnection()
	case id == "conn.search":
		return m.focusConnectionSearch()
	case id == "conn.reset-filter":
		m.connections.caseSensitive, m.connections.wholeWord, m.connections.regex = false, false, false
		return m.connectionActivate("conn.clear")
	case id == "conn.case" || id == "conn.word" || id == "conn.regex":
		if !m.busy && !m.quitting && m.connections.detail == nil && !m.connections.confirming {
			m.toggleConnectionFilter(id)
		}
	case id == "conn.retry":
		if !m.busy && !m.quitting && !m.runtime.pending && !m.connections.closedView && m.connections.detail == nil && !m.connections.confirming && len(m.filteredConnections()) == 0 && m.connectionsEmptyState().ActionID == id {
			return tea.Batch(m.retryRuntime(), m.refreshCoreStatus())
		}
		return nil
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
		if !m.canPauseConnections() {
			return nil
		}
		if m.connections.paused {
			m.connections.paused, m.connections.frozen = false, nil
			m.connections.frozenClosed = nil
			m.connections.frozenRates = connectionRatesState{}
		} else if snapshot := m.connectionSnapshot(); snapshot != nil {
			copy := *snapshot
			copy.Connections = append([]mihomo.Connection(nil), snapshot.Connections...)
			for i := range copy.Connections {
				copy.Connections[i].Chains = append([]string(nil), copy.Connections[i].Chains...)
			}
			m.connections.frozen = &copy
			m.connections.frozenRates = m.connections.rates
			m.connections.frozenClosed = m.connections.history.entries()
			m.connections.frozenAt = m.connectionNow()
			m.connections.frozenPID = m.runtime.snapshot.Service.PID
			m.connections.frozenVersion = m.runtime.snapshot.RunningVersion
			m.connections.paused = true
		}
		m.reconcileConnections()
		m.revealConnection(0)
	case id == "conn.close-all":
		if !m.connections.confirming && m.canCloseAllConnections() {
			m.connections.closeAllScroll = m.scroll
			m.connections.confirming, m.connections.closeAll = true, true
			m.scroll, m.focus = 0, "conn.cancel"
		}
	case id == "conn.back":
		m.connections.detail = nil
		m.connections.confirming = false
		m.scroll = m.connections.detailScroll
		m.focus = "connection." + m.connections.selected
		m.reconcileConnections()
		m.revealConnection(0)
	case id == "conn.close":
		if !m.busy && !m.connections.confirming && m.detailActive() {
			m.connections.confirming = true
			m.connections.closeAll = false
			m.scroll = 0
			m.focus = "conn.cancel"
		}
	case id == "conn.cancel":
		m.connections.confirming = false
		if m.connections.closeAll {
			m.connections.closeAll = false
			m.scroll, m.focus = m.connections.closeAllScroll, "conn.close-all"
		} else {
			m.focus = "conn.close"
		}
	case id == "conn.confirm":
		if !m.connections.confirming || m.busy {
			return nil
		}
		if m.connections.closeAll {
			available := m.canCloseAllConnections()
			m.connectionActivate("conn.cancel")
			if !available {
				m.prepareNotification("connections.close-all", m.tr("connections.closing_all"))
				m.failure = m.tr("connections.close_all_unavailable")
				return nil
			}
			// The operation affects the live core, including connections hidden by
			// filtering or pause. Resume the live view to observe its result.
			m.connections.paused, m.connections.frozen = false, nil
			m.connections.frozenClosed = nil
			m.connections.frozenRates = connectionRatesState{}
			return m.begin(m.tr("connections.closing_all"), m.tr("connections.close_all_requested"), m.backend.CloseAllConnections, notificationOperation{Key: "connections.close-all", Context: m.tr("connections.closing_all")})
		}
		if m.connections.detail == nil {
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
