package tui

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type connectionRate struct {
	Upload, Download int64
	Valid            bool
}

type connectionSample struct {
	upload, download int64
	start            time.Time
}

// Rates are derived only from consecutive successful observations of the same
// connection. They are never integrated into the core's cumulative totals.
type connectionRatesState struct {
	samples                    map[string]connectionSample
	values                     map[string]connectionRate
	at                         time.Time
	pid                        int
	version                    string
	uploadTotal, downloadTotal int64
}

func (s *connectionRatesState) observe(snapshot app.RuntimeSnapshot, receivedAt time.Time) {
	if snapshot.State != "ready" || snapshot.Connections == nil {
		*s = connectionRatesState{}
		return
	}
	at := snapshot.ObservedAt
	if at.IsZero() {
		at = receivedAt
	}
	elapsed := at.Sub(s.at)
	continuous := !s.at.IsZero() && elapsed > 0 && s.pid == snapshot.Service.PID && s.version == snapshot.RunningVersion && snapshot.Connections.UploadTotal >= s.uploadTotal && snapshot.Connections.DownloadTotal >= s.downloadTotal
	samples := make(map[string]connectionSample, len(snapshot.Connections.Connections))
	values := make(map[string]connectionRate, len(snapshot.Connections.Connections))
	for _, c := range snapshot.Connections.Connections {
		previous, found := s.samples[c.ID]
		if continuous && found && previous.start.Equal(c.Start) && previous.upload >= 0 && previous.download >= 0 && c.Upload >= previous.upload && c.Download >= previous.download {
			values[c.ID] = connectionRate{Upload: connectionBytesPerSecond(c.Upload-previous.upload, elapsed), Download: connectionBytesPerSecond(c.Download-previous.download, elapsed), Valid: true}
		}
		samples[c.ID] = connectionSample{upload: c.Upload, download: c.Download, start: c.Start}
	}
	s.samples, s.values, s.at, s.pid = samples, values, at, snapshot.Service.PID
	s.version = snapshot.RunningVersion
	s.uploadTotal, s.downloadTotal = snapshot.Connections.UploadTotal, snapshot.Connections.DownloadTotal
}

func (s connectionRatesState) get(id string) connectionRate {
	if value, found := s.values[id]; found && value.Valid {
		return value
	}
	return connectionRate{Upload: -1, Download: -1}
}

func connectionBytesPerSecond(delta int64, elapsed time.Duration) int64 {
	value := float64(delta) / elapsed.Seconds()
	if value >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(value)
}

type connectionSort uint8

const (
	connectionSortNewest connectionSort = iota
	connectionSortDownloadRate
	connectionSortUploadRate
	connectionSortDownloadTotal
	connectionSortUploadTotal
	connectionSortDuration
	connectionSortDestination
	connectionSortProtocol
	connectionSortProcess
	connectionSortChain
	connectionSortRule
	connectionSortCount
)

func (s connectionSort) key() string {
	switch s {
	case connectionSortDownloadRate:
		return "connections.sort_download_rate"
	case connectionSortUploadRate:
		return "connections.sort_upload_rate"
	case connectionSortDownloadTotal:
		return "connections.sort_download_total"
	case connectionSortUploadTotal:
		return "connections.sort_upload_total"
	case connectionSortDuration:
		return "connections.sort_duration"
	case connectionSortDestination:
		return "connections.destination"
	case connectionSortProtocol:
		return "connections.protocol"
	case connectionSortProcess:
		return "connections.process"
	case connectionSortChain:
		return "connections.chain"
	case connectionSortRule:
		return "connections.rule"
	default:
		return "connections.sort_newest"
	}
}

func (s connectionSort) textField() bool {
	switch s {
	case connectionSortDestination, connectionSortProtocol, connectionSortProcess, connectionSortChain, connectionSortRule:
		return true
	default:
		return false
	}
}

func (s connectionSort) descending(reversed bool) bool {
	if s.textField() {
		return reversed
	}
	return !reversed
}

// Compute the displayed column's key once per match. In particular, sorting
// chains should not rebuild the route or shorten it to the current cell width
// during each comparison.
type connectionSortRow struct {
	connection *mihomo.Connection
	text       string
	number     int64
	recent     time.Time
	missing    bool
}

func (s connectionSort) row(c *mihomo.Connection, now, closedAt time.Time, rates connectionRatesState) connectionSortRow {
	row := connectionSortRow{connection: c, recent: closedAt}
	switch s {
	case connectionSortDestination:
		row.text = connectionDestination(*c)
	case connectionSortProtocol:
		row.text = clean(c.Metadata.Network)
	case connectionSortProcess:
		row.text = clean(c.Metadata.Process)
	case connectionSortChain:
		row.text = connectionChain(*c)
	case connectionSortRule:
		row.text = connectionRule(*c)
	case connectionSortDownloadRate:
		row.number = rates.get(c.ID).Download
	case connectionSortUploadRate:
		row.number = rates.get(c.ID).Upload
	case connectionSortDownloadTotal:
		row.number = c.Download
	case connectionSortUploadTotal:
		row.number = c.Upload
	case connectionSortDuration:
		if !closedAt.IsZero() {
			now = closedAt
		}
		row.number = int64(now.Sub(c.Start))
		row.missing = c.Start.IsZero()
	case connectionSortNewest:
		if row.recent.IsZero() {
			row.recent = c.Start
		}
		row.missing = row.recent.IsZero()
	}
	if s.textField() {
		row.text = strings.ToLower(strings.TrimSpace(row.text))
		row.missing = row.text == "" || row.text == "--"
	} else {
		row.missing = row.missing || row.number < 0
	}
	return row
}

func (s connectionSort) sorted(rows []connectionSortRow, reversed bool) []mihomo.Connection {
	descending := s.descending(reversed)
	slices.SortFunc(rows, func(a, b connectionSortRow) int {
		if a.missing != b.missing {
			if a.missing {
				return 1
			}
			return -1
		}
		if !a.missing {
			primary := cmp.Or(cmp.Compare(a.number, b.number), strings.Compare(a.text, b.text))
			if s == connectionSortNewest {
				primary = a.recent.Compare(b.recent)
			}
			if primary != 0 {
				if descending {
					return -primary
				}
				return primary
			}
		}
		// Preserve the archive's newest-closure tie break, then identity,
		// independently of the selected field's direction.
		return cmp.Or(b.recent.Compare(a.recent), strings.Compare(a.connection.ID, b.connection.ID),
			a.connection.Start.Compare(b.connection.Start))
	})
	list := make([]mihomo.Connection, len(rows))
	for i := range rows {
		list[i] = *rows[i].connection
	}
	return list
}
