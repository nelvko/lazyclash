package tui

import (
	"slices"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func historyObservation(at time.Time, pid int, total int64) app.RuntimeSnapshot {
	return app.RuntimeSnapshot{
		State: "ready", ObservedAt: at, Service: service.Status{Active: true, State: "active", PID: pid},
		Traffic:     &mihomo.Traffic{Up: 12, Down: 34, UpTotal: total, DownTotal: total},
		Connections: &mihomo.ConnectionSnapshot{UploadTotal: total, DownloadTotal: total},
	}
}

func TestTrafficHistoryKeepsObservedRatesAndMissingSamplesAsGaps(t *testing.T) {
	base := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	var history trafficHistory
	for index, sample := range []struct {
		change func(*app.RuntimeSnapshot)
		valid  bool
		up     int64
	}{
		{valid: true, up: 12},
		{change: func(s *app.RuntimeSnapshot) { s.Traffic.Up, s.Traffic.Down = 0, 0 }, valid: true},
		{change: func(s *app.RuntimeSnapshot) { s.Traffic = nil }},
		{change: func(s *app.RuntimeSnapshot) { s.TrafficError = "Traffic unavailable" }},
		{change: func(s *app.RuntimeSnapshot) { s.State, s.Error = "unavailable", "API unavailable" }},
		{change: func(s *app.RuntimeSnapshot) { s.Traffic.Up = -1 }},
		{change: func(s *app.RuntimeSnapshot) { s.MemoryError = "Memory unavailable" }, valid: true, up: 12},
	} {
		at := base.Add(time.Duration(index) * time.Second)
		observation := historyObservation(at, 1, int64(index))
		if sample.change != nil {
			sample.change(&observation)
		}
		history.observe(observation, base.Add(30*time.Second))
		point := history.points[index]
		if !point.At.Equal(at) || point.Valid != sample.valid || point.Up != sample.up || !point.Valid && point.Down != 0 {
			t.Fatalf("sample %d lost its observation timestamp/rate/gap: %+v", index, point)
		}
	}
	// Paused sampling leaves time empty rather than fabricating a zero-filled
	// series; the renderer can see the timestamp gap on resumption.
	history.observe(historyObservation(base.Add(20*time.Second), 1, 20), base.Add(30*time.Second))
	if len(history.points) != 8 || history.points[7].At.Sub(history.points[6].At) != 14*time.Second {
		t.Fatal("sampling pause inserted synthetic observations")
	}
	fallback := base.Add(31 * time.Second)
	history.observe(historyObservation(time.Time{}, 1, 31), fallback)
	if !history.points[len(history.points)-1].At.Equal(fallback) {
		t.Fatal("zero observation time did not use receipt time")
	}
}

func TestTrafficHistoryBoundsMemoryAndPrunesTheActualTimeWindow(t *testing.T) {
	base := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	var history trafficHistory
	for index := 0; index < 200; index++ {
		at := base.Add(time.Duration(index) * 100 * time.Millisecond)
		history.observe(historyObservation(at, 1, int64(index)), at)
	}
	if len(history.points) != trafficHistoryLimit || cap(history.points) > trafficHistoryLimit || !history.points[0].At.Equal(base.Add(7200*time.Millisecond)) {
		t.Fatalf("dense observations exceeded the bounded memory: len=%d cap=%d", len(history.points), cap(history.points))
	}
	now := base.Add(80 * time.Second)
	history.observe(historyObservation(now, 1, 200), now)
	if len(history.points) != 1 || !history.points[0].At.Equal(now) {
		t.Fatal("history retained observations outside the last sixty seconds")
	}
}

func TestTrafficHistoryRejectsOldDuplicateAndFutureObservationsBeforeResettingIdentity(t *testing.T) {
	base := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	var history trafficHistory
	history.observe(historyObservation(base, 10, 100), base)
	for _, at := range []time.Time{base, base.Add(-time.Second), base.Add(-2 * time.Minute), base.Add(time.Hour)} {
		history.observe(historyObservation(at, 20, 1), base.Add(time.Second))
		if len(history.points) != 1 || history.pid != 10 || history.points[0].Up != 12 {
			t.Fatal("stale or future observation reset the known process or changed its history")
		}
	}
	history.observe(historyObservation(base.Add(time.Second), 10, 101), base.Add(time.Second))
	if len(history.points) != 2 {
		t.Fatal("rejected future timestamp poisoned the next actual observation")
	}
}

func TestTrafficHistoryResetsOnlyOnObservedProcessOrCounterChanges(t *testing.T) {
	base := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	for _, sample := range []struct {
		name       string
		beforePID  int
		afterPID   int
		afterState string
		want       int
	}{
		{name: "same process", beforePID: 10, afterPID: 10, afterState: "ready", want: 2},
		{name: "changed process", beforePID: 10, afterPID: 20, afterState: "ready", want: 1},
		{name: "unknown process counters reset", afterState: "ready", want: 1},
		{name: "stopped", beforePID: 10, afterState: "stopped"},
		{name: "uninstalled", beforePID: 10, afterState: "uninstalled"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var history trafficHistory
			history.observe(historyObservation(base, sample.beforePID, 100), base)
			after := historyObservation(base.Add(time.Second), sample.afterPID, 1)
			after.State = sample.afterState
			history.observe(after, after.ObservedAt)
			if len(history.points) != sample.want {
				t.Fatalf("got %d points, want %d", len(history.points), sample.want)
			}
			if sample.want == 0 {
				history.observe(historyObservation(base, sample.beforePID, 100), after.ObservedAt)
				if len(history.points) != 0 {
					t.Fatal("delayed pre-stop sample returned after a reset")
				}
			}
		})
	}
}

func TestTrafficHistoryFullSnapshotsPreserveStableProcessesAndResetConfirmedChanges(t *testing.T) {
	base := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	for _, sample := range []struct {
		name    string
		change  func(*app.Snapshot)
		cleared bool
	}{
		{name: "ordinary refresh", change: func(*app.Snapshot) {}},
		{name: "configuration generation same process", change: func(s *app.Snapshot) { s.Generation = "two" }},
		{name: "different process", change: func(s *app.Snapshot) { s.Service.PID = 20 }, cleared: true},
		{name: "generation without known process", change: func(s *app.Snapshot) { s.Generation, s.Service.PID = "two", 0 }, cleared: true},
		{name: "confirmed stop", change: func(s *app.Snapshot) { s.Service.Active, s.Service.State = false, "inactive" }, cleared: true},
		{name: "uninstalled", change: func(s *app.Snapshot) { s.Installed = false }, cleared: true},
		{name: "unknown service", change: func(s *app.Snapshot) {
			s.Service.Active, s.Service.State, s.ServiceError = false, "inactive", "Status unavailable"
		}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var history trafficHistory
			snapshot := app.Snapshot{Installed: true, Generation: "one", Service: service.Status{Active: true, State: "active", PID: 10}}
			history.observeSnapshot(snapshot)
			history.observe(historyObservation(base, 10, 100), base)
			before := slices.Clone(history.points)
			sample.change(&snapshot)
			history.observeSnapshot(snapshot)
			if sample.cleared {
				if len(history.points) != 0 {
					t.Fatal("confirmed process/installation change retained old history")
				}
			} else if !slices.Equal(before, history.points) {
				t.Fatal("same process or unknown service observation destroyed valid history")
			}
		})
	}
}
