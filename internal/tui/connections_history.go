package tui

import (
	"sort"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

const connectionHistoryLimit = 500

type closedConnection struct {
	Connection mihomo.Connection
	ClosedAt   time.Time
}

// History contains only connections observed ending in this TUI session. The
// core does not return closed connections or their final counters.
type connectionHistoryState struct {
	active                     map[string]mihomo.Connection
	closed                     []closedConnection
	at                         time.Time
	observed                   bool
	instance                   uint64
	installation               string
	pid                        int
	version                    string
	generation                 uint64
	hasTotals                  bool
	uploadTotal, downloadTotal int64
}

func (s *connectionHistoryState) observe(snapshot app.RuntimeSnapshot, receivedAt time.Time, instance uint64, installation string) {
	at := snapshot.ObservedAt
	if at.IsZero() {
		at = receivedAt
	}
	identityChanged := (instance != 0 && s.instance != 0 && instance != s.instance) ||
		(installation != "" && s.installation != "" && installation != s.installation) ||
		(snapshot.Service.PID > 0 && s.pid > 0 && snapshot.Service.PID != s.pid) ||
		(snapshot.RunningVersion != "" && s.version != "" && snapshot.RunningVersion != s.version)
	available := snapshot.State == "ready" && snapshot.Connections != nil
	if available && !identityChanged && s.observed && !at.After(s.at) {
		return
	}
	totalsReset := available && s.hasTotals && (snapshot.Connections.UploadTotal < s.uploadTotal || snapshot.Connections.DownloadTotal < s.downloadTotal)
	if identityChanged || totalsReset {
		s.clear()
	}
	if instance != 0 {
		s.instance = instance
	}
	if installation != "" {
		s.installation = installation
	}
	if snapshot.Service.PID > 0 {
		s.pid = snapshot.Service.PID
	}
	if snapshot.RunningVersion != "" {
		s.version = snapshot.RunningVersion
	}
	if !available {
		// A failed observation cannot establish that any connection ended.
		s.active = nil
		if !s.observed || at.After(s.at) {
			s.at, s.observed = at, true
		}
		return
	}
	next := make(map[string]mihomo.Connection, len(snapshot.Connections.Connections))
	for _, c := range snapshot.Connections.Connections {
		next[c.ID] = copyHistoryConnection(c)
	}
	var ended []closedConnection
	for id, c := range s.active {
		if current, present := next[id]; !present || !current.Start.Equal(c.Start) {
			ended = append(ended, closedConnection{Connection: c, ClosedAt: at})
		}
	}
	// Connections ending in one observation share a timestamp. Give them a
	// deterministic order independent of map iteration.
	sort.Slice(ended, func(i, j int) bool { return ended[i].Connection.ID < ended[j].Connection.ID })
	if len(ended) > 0 {
		endedStarts := make(map[string]time.Time, len(ended))
		for _, entry := range ended {
			endedStarts[entry.Connection.ID] = entry.Connection.Start
		}
		retained := make([]closedConnection, 0, min(connectionHistoryLimit, len(ended)+len(s.closed)))
		retained = append(retained, ended[:min(connectionHistoryLimit, len(ended))]...)
		for _, entry := range s.closed {
			if len(retained) == connectionHistoryLimit {
				break
			}
			// A lifetime that reappears updates its existing history entry.
			if start, repeated := endedStarts[entry.Connection.ID]; repeated && start.Equal(entry.Connection.Start) {
				continue
			}
			retained = append(retained, entry)
		}
		s.closed = retained
	}
	s.active, s.at, s.observed = next, at, true
	s.uploadTotal, s.downloadTotal, s.hasTotals = snapshot.Connections.UploadTotal, snapshot.Connections.DownloadTotal, true
}

func (s *connectionHistoryState) clear() {
	*s = connectionHistoryState{generation: s.generation + 1}
}

func (s connectionHistoryState) entries() []closedConnection {
	out := make([]closedConnection, len(s.closed))
	for i, entry := range s.closed {
		out[i] = closedConnection{Connection: copyHistoryConnection(entry.Connection), ClosedAt: entry.ClosedAt}
	}
	return out
}

// find returns a read-only value; callers freezing or mutating entries use
// entries instead so that chain slices remain owned by the history cache.
func (s connectionHistoryState) find(c mihomo.Connection) (closedConnection, bool) {
	for _, entry := range s.closed {
		if entry.Connection.ID == c.ID && entry.Connection.Start.Equal(c.Start) {
			return entry, true
		}
	}
	return closedConnection{}, false
}

func copyHistoryConnection(c mihomo.Connection) mihomo.Connection {
	c.Chains = append([]string(nil), c.Chains...)
	return c
}
