package tui

import (
	"time"

	"github.com/nelvko/lazyclash/internal/app"
)

const (
	trafficHistoryWindow = 60 * time.Second
	trafficHistoryLimit  = 128
	trafficHistoryMaxGap = 3 * time.Second
)

type trafficPoint struct {
	At       time.Time
	Up, Down int64
	Valid    bool
}

// Samples belong to one observed core process. Timestamps, including gaps,
// remain the authority for the chart; rates are never integrated into totals.
type trafficHistory struct {
	points                     []trafficPoint
	lastAt                     time.Time
	pid                        int
	generation                 string
	totalsKnown                bool
	uploadTotal, downloadTotal int64
}

func (h *trafficHistory) clear() {
	h.points = h.points[:0]
	h.pid, h.totalsKnown = 0, false
	// Keep the timestamp watermark so a delayed pre-reset sample cannot return.
}

// Full snapshots can confirm a stop or a changed installation even while the
// runtime sampler is paused. A configuration generation alone is not a restart
// when the same known process remains active.
func (h *trafficHistory) observeSnapshot(s app.Snapshot) {
	pid := 0
	if s.ServiceError == "" && s.Service.Active {
		pid = s.Service.PID
	}
	stablePID := pid > 0 && pid == h.pid
	changed := s.Generation != "" && h.generation != "" && s.Generation != h.generation
	stopped := s.ServiceError == "" && !s.Service.Active && (s.Service.State == "inactive" || s.Service.State == "failed")
	if !s.Installed || stopped || changed && !stablePID || pid > 0 && h.pid > 0 && pid != h.pid {
		h.clear()
	}
	if pid > 0 {
		h.pid = pid
	}
	if s.Generation != "" {
		h.generation = s.Generation
	}
}

func (h *trafficHistory) observe(s app.RuntimeSnapshot, now time.Time) bool {
	cutoff := now.Add(-trafficHistoryWindow)
	first := 0
	for first < len(h.points) && h.points[first].At.Before(cutoff) {
		first++
	}
	if first > 0 {
		copy(h.points, h.points[first:])
		h.points = h.points[:len(h.points)-first]
	}
	at := s.ObservedAt
	if at.IsZero() {
		at = now
	}
	if at.Before(cutoff) || at.After(now) || !h.lastAt.IsZero() && !at.After(h.lastAt) {
		return false
	}
	h.lastAt = at
	if s.State == "stopped" || s.State == "uninstalled" {
		h.clear()
		return true
	}
	if s.Service.PID > 0 {
		if h.pid > 0 && s.Service.PID != h.pid {
			h.clear()
		}
		h.pid = s.Service.PID
	}
	if s.State == "ready" && s.Traffic != nil && s.TrafficError == "" {
		up, down := s.Traffic.UpTotal, s.Traffic.DownTotal
		if up >= 0 && down >= 0 {
			if s.Service.PID <= 0 && h.totalsKnown && (up < h.uploadTotal || down < h.downloadTotal) {
				h.clear()
			}
			h.uploadTotal, h.downloadTotal, h.totalsKnown = up, down, true
		}
	}
	point := trafficPoint{At: at}
	if s.State == "ready" && s.Error == "" && s.TrafficError == "" && s.Traffic != nil && s.Traffic.Up >= 0 && s.Traffic.Down >= 0 {
		point.Up, point.Down, point.Valid = s.Traffic.Up, s.Traffic.Down, true
	}
	if h.points == nil {
		h.points = make([]trafficPoint, 0, trafficHistoryLimit)
	}
	if len(h.points) == trafficHistoryLimit {
		copy(h.points, h.points[1:])
		h.points = h.points[:trafficHistoryLimit-1]
	}
	h.points = append(h.points, point)
	return true
}
