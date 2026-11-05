package tui

import (
	"math"
	"sort"
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
	default:
		return "connections.sort_newest"
	}
}

func sortConnections(list []mihomo.Connection, order connectionSort, rates connectionRatesState) {
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		var av, bv int64
		switch order {
		case connectionSortDownloadRate:
			av, bv = rates.get(a.ID).Download, rates.get(b.ID).Download
		case connectionSortUploadRate:
			av, bv = rates.get(a.ID).Upload, rates.get(b.ID).Upload
		case connectionSortDownloadTotal:
			av, bv = a.Download, b.Download
		case connectionSortUploadTotal:
			av, bv = a.Upload, b.Upload
		default:
			if a.Start.IsZero() != b.Start.IsZero() {
				return !a.Start.IsZero()
			}
			if !a.Start.Equal(b.Start) {
				if order == connectionSortDuration {
					return a.Start.Before(b.Start)
				}
				return a.Start.After(b.Start)
			}
		}
		if av != bv {
			return av > bv
		}
		return a.ID < b.ID
	})
}
