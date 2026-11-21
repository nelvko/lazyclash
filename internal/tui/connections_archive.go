package tui

import (
	"sort"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
)

func (m *Model) connectionClosedEntries() []closedConnection {
	if m.connections.paused {
		return m.connections.frozenClosed
	}
	return m.connections.history.closed
}

func (m *Model) closedEntry(c mihomo.Connection) (closedConnection, bool) {
	for _, entry := range m.connectionClosedEntries() {
		if entry.Connection.ID == c.ID && entry.Connection.Start.Equal(c.Start) {
			return entry, true
		}
	}
	return closedConnection{}, false
}

func (m *Model) connectionRowKey(c mihomo.Connection) string {
	if m.connections.closedView {
		return "closed." + c.ID + "." + c.Start.UTC().Format(time.RFC3339Nano)
	}
	return c.ID
}

func (m *Model) connectionRowTime(c mihomo.Connection) time.Time {
	if m.connections.detail != nil && c.ID == m.connections.detail.ID && c.Start.Equal(m.connections.detail.Start) && !m.connections.detailClosedAt.IsZero() {
		return m.connections.detailClosedAt
	}
	if m.connections.closedView {
		if entry, ok := m.closedEntry(c); ok {
			return entry.ClosedAt
		}
	}
	return m.connectionNow()
}

func (m *Model) connectionRowRate(c mihomo.Connection) connectionRate {
	if m.connections.closedView {
		return connectionRate{Upload: -1, Download: -1}
	}
	return m.displayedConnectionRates().get(c.ID)
}

func (m *Model) filteredConnections() []mihomo.Connection {
	match, err := m.connectionMatcher()
	if err != nil {
		return nil
	}
	var source, out []mihomo.Connection
	closedAt := map[string]time.Time{}
	if m.connections.closedView {
		for _, entry := range m.connectionClosedEntries() {
			source = append(source, entry.Connection)
			closedAt[m.connectionRowKey(entry.Connection)] = entry.ClosedAt
		}
	} else if snapshot := m.connectionSnapshot(); snapshot != nil {
		source = snapshot.Connections
	}
	for _, c := range source {
		if match(c) {
			out = append(out, c)
		}
	}
	if !m.connections.closedView {
		sortConnections(out, m.connections.sort, m.displayedConnectionRates())
		return out
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		aa, ba := closedAt[m.connectionRowKey(a)], closedAt[m.connectionRowKey(b)]
		var av, bv int64
		switch m.connections.sort {
		case connectionSortDownloadTotal:
			av, bv = a.Download, b.Download
		case connectionSortUploadTotal:
			av, bv = a.Upload, b.Upload
		case connectionSortDuration:
			av, bv = int64(aa.Sub(a.Start)), int64(ba.Sub(b.Start))
		}
		if av != bv {
			return av > bv
		}
		if !aa.Equal(ba) {
			return aa.After(ba)
		}
		return m.connectionRowKey(a) < m.connectionRowKey(b)
	})
	return out
}
