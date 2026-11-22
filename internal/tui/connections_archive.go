package tui

import (
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
	now := m.connectionNow()
	rates := m.displayedConnectionRates()
	order := m.connections.sort
	var rows []connectionSortRow
	add := func(c *mihomo.Connection, closedAt time.Time) {
		if match(*c) {
			rows = append(rows, order.row(c, now, closedAt, rates))
		}
	}
	if m.connections.closedView {
		// A live rate for a reused ID does not belong to a historical row.
		rates = connectionRatesState{}
		entries := m.connectionClosedEntries()
		for i := range entries {
			add(&entries[i].Connection, entries[i].ClosedAt)
		}
	} else if snapshot := m.connectionSnapshot(); snapshot != nil {
		for i := range snapshot.Connections {
			add(&snapshot.Connections[i], time.Time{})
		}
	}
	return order.sorted(rows, m.connections.sortReversed)
}
