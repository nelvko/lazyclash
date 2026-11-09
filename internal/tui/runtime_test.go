package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func runtimeModel(t *testing.T, b *fakeBackend) Model {
	t.Helper()
	b.snapshot.Installed, b.snapshot.ConfigReady = true, true
	b.snapshot.Service = service.Status{Active: true, State: "active"}
	m := New(t.Context(), b, "en")
	m.snapshot, m.snapshotReady = b.snapshot, true
	m.busy, m.job = false, ""
	m.message, m.messageTransient = "State refreshed", true
	m.syncNotification()
	m.width, m.height = 120, 32
	t.Cleanup(func() { m.stopRuntime() })
	m.layout()
	return m
}

func liveRuntime() app.RuntimeSnapshot {
	memory := uint64(16 << 20)
	return app.RuntimeSnapshot{State: "ready", Service: service.Status{Active: true, State: "active"}, RunningVersion: "v1.19.33", ObservedAt: time.Now(), Traffic: &mihomo.Traffic{Up: 2048, Down: 4096, UpTotal: 1 << 20, DownTotal: 2 << 20}, Connections: &mihomo.ConnectionSnapshot{UploadTotal: 1 << 20, DownloadTotal: 2 << 20, Connections: []mihomo.Connection{}}, Memory: &memory}
}

func TestRuntimeSharedSessionSurvivesPagesResizeOverlaysAndForegroundJobs(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	if m.syncRuntime() == nil || !m.runtime.active || !m.runtime.pending || m.syncRuntime() != nil {
		t.Fatal("shared session did not start exactly once")
	}
	defer m.stopRuntime()
	generation, ctx := m.runtime.generation, m.runtime.ctx
	for _, page := range navPages() {
		m.browsePage(page)
		for _, size := range [][2]int{{160, 45}, {80, 24}, {40, 16}} {
			m.width, m.height = size[0], size[1]
			m.helpOpen, m.ports.open, m.wizard.open, m.wizard.networkOpen = true, true, true, true
			if m.syncRuntime() != nil || m.runtime.generation != generation || m.runtime.ctx != ctx || ctx.Err() != nil {
				t.Fatal("page/size/modal replaced shared sources")
			}
		}
	}
	m.newJob("refresh")
	m.cancelTask(false)
	if m.syncRuntime() != nil || ctx.Err() != nil || m.runtime.generation != generation {
		t.Fatal("foreground work interrupted shared sources")
	}
}

func TestRuntimeSourcesUpdateIndependentlyAndConnectionRatesUseTheirOwnClock(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	m.runtime.active = true
	base := time.Now().Add(-5 * time.Second)
	sample := liveRuntime()
	sample.Service.PID = 42
	sample.ObservedAt = base
	m = acceptRuntimeFixture(m, sample)
	// Traffic can advance without waiting for memory, connections or version.
	for i := 1; i <= 3; i++ {
		traffic := &mihomo.Traffic{Up: int64(i) * 100, Down: 200, UpTotal: 1 << 20, DownTotal: 2 << 20}
		m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeTraffic, Instance: 1, ObservedAt: base.Add(time.Duration(i) * time.Second), Traffic: traffic})
	}
	if len(m.runtime.history.points) != 4 || m.runtime.snapshot.Traffic.Up != 300 || *m.runtime.snapshot.Memory != 16<<20 {
		t.Fatal("source updates were coupled or frames were discarded")
	}
	connections := &mihomo.ConnectionSnapshot{Connections: []mihomo.Connection{{ID: "one", Download: 100}}, UploadTotal: 1 << 20, DownloadTotal: 2 << 20}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeConnections, Instance: 1, ObservedAt: base.Add(time.Second), Connections: connections})
	connections = &mihomo.ConnectionSnapshot{Connections: []mihomo.Connection{{ID: "one", Download: 500}}, UploadTotal: 1 << 20, DownloadTotal: 2 << 20}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeConnections, Instance: 1, ObservedAt: base.Add(3 * time.Second), Connections: connections})
	if got := m.connections.rates.get("one"); !got.Valid || got.Download != 200 {
		t.Fatalf("connection source used another source clock: %+v", got)
	}
	if len(m.runtime.snapshot.Connections.Connections) != 1 || len(m.runtime.history.points) != 4 {
		t.Fatal("shared count or graph changed on an unrelated event")
	}
}

func TestRuntimeSourceFailureLeavesGapAndPreservesOtherSources(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	sample := liveRuntime()
	sample.ObservedAt = time.Now().Add(-3 * time.Second)
	m = acceptRuntimeFixture(m, sample)
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeTraffic, Instance: 1, ObservedAt: sample.ObservedAt.Add(time.Second), Err: errors.New("stream interrupted")})
	if m.runtime.snapshot.Traffic != nil || m.runtime.snapshot.Memory == nil || m.runtime.snapshot.Connections == nil || m.runtime.snapshot.State != "ready" || m.runtime.history.points[1].Valid {
		t.Fatal("source failure erased unrelated metrics or appeared as zero")
	}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeTraffic, Instance: 1, ObservedAt: sample.ObservedAt.Add(2 * time.Second), Traffic: sample.Traffic})
	if m.runtime.snapshot.TrafficError != "" || !m.runtime.history.points[2].Valid {
		t.Fatal("recovered source retained failure or missing sample")
	}
}

func TestRuntimeRejectsOldInstancesGenerationsAndPerSourceTimestamps(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	m.runtime.generation = 5
	sample := liveRuntime()
	sample.ObservedAt = time.Now().Add(-2 * time.Second)
	sample.Service.PID = 42
	m = acceptRuntimeFixture(m, sample)
	before := len(m.runtime.history.points)
	for _, at := range []time.Time{sample.ObservedAt, sample.ObservedAt.Add(-time.Second), time.Now().Add(time.Hour)} {
		m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeTraffic, Instance: 1, ObservedAt: at, Traffic: &mihomo.Traffic{Up: 999}})
	}
	if len(m.runtime.history.points) != before || m.runtime.snapshot.Traffic.Up != 2048 {
		t.Fatal("old/duplicate/future source data changed history")
	}
	stale := app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 0, ObservedAt: time.Now(), Snapshot: app.RuntimeSnapshot{State: "stopped"}}
	m.acceptRuntimeEvent(stale)
	if m.runtime.snapshot.State != "ready" {
		t.Fatal("old instance stopped current core")
	}
	if m.acceptRuntime(runtimeResultMsg{generation: 4, events: []app.RuntimeEvent{{Source: app.RuntimeTraffic, Instance: 1, ObservedAt: time.Now(), Traffic: &mihomo.Traffic{Up: 999}}}}) != nil || m.runtime.snapshot.Traffic.Up != 2048 {
		t.Fatal("old session message changed shared cache")
	}
}

func TestRuntimeBatchPreservesEveryTrafficFrame(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	updates := make(chan app.RuntimeEvent, 64)
	base := time.Now().Add(-60 * time.Second)
	for i := range 60 {
		updates <- app.RuntimeEvent{Source: app.RuntimeTraffic, Instance: 1, ObservedAt: base.Add(time.Duration(i+1) * time.Second), Traffic: &mihomo.Traffic{Up: int64(i)}}
	}
	m := runtimeModel(t, &fakeBackend{})
	m.runtime.active, m.runtime.instance, m.runtime.ctx, m.runtime.updates = true, 1, ctx, updates
	m.runtime.snapshot.State = "ready"
	for i := 0; i < 2; i++ {
		msg := waitRuntime(ctx, m.runtime.generation, updates)().(runtimeResultMsg)
		m.acceptRuntime(msg)
	}
	if len(m.runtime.history.points) != 60 || m.runtime.history.points[59].Up != 59 {
		t.Fatal("batched notification dropped graph samples")
	}
}

func TestRuntimeProcessTransitionClearsMetricsAndHistoryButKeepsLogHistory(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	s := liveRuntime()
	s.Service.PID = 42
	s.ObservedAt = time.Now().Add(-time.Second)
	m = acceptRuntimeFixture(m, s)
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "retained"}})
	changed := app.RuntimeSnapshot{State: "ready", Service: service.Status{Active: true, State: "active", PID: 43}}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 2, ObservedAt: time.Now(), Snapshot: changed})
	if len(m.runtime.history.points) != 0 || m.runtime.snapshot.Traffic != nil || m.runtime.snapshot.Memory != nil || m.runtime.snapshot.Connections != nil || len(m.logs.entries) != 1 {
		t.Fatal("new core inherited old observations or erased retained logs")
	}
}

func TestRuntimeUnknownStatusDoesNotInventAStoppedService(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	s := liveRuntime()
	s.ObservedAt = time.Now().Add(-time.Second)
	m = acceptRuntimeFixture(m, s)
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 2, ObservedAt: time.Now(), Snapshot: app.RuntimeSnapshot{State: "unavailable", Error: "manager unavailable"}})
	if m.snapshot.ServiceError == "" || !m.snapshot.Service.Active || m.runtime.snapshot.Traffic != nil {
		t.Fatal("unknown service was presented as stopped or kept stale values")
	}
}

func TestRuntimeQuietSessionCancelsAndRejectsLateFrames(t *testing.T) {
	started, done := make(chan struct{}), make(chan struct{})
	b := &fakeBackend{streamRuntime: func(ctx context.Context, emit func(app.RuntimeEvent)) error {
		close(started)
		<-ctx.Done()
		close(done)
		return ctx.Err()
	}}
	m := runtimeModel(t, b)
	commands := commandBatch(t, m.syncRuntime())
	go commands[0]()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("session did not start")
	}
	id := m.runtime.generation
	m.requestQuit()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("quiet source did not cancel")
	}
	if commands[1]() != nil || m.runtime.active || m.acceptRuntime(runtimeResultMsg{generation: id, events: fixtureRuntimeEvents(liveRuntime(), 1)}) != nil || m.runtime.received {
		t.Fatal("late canceled-session event was accepted")
	}
}

func TestRuntimeTransitionRefreshesPersistentIntentAndActiveProfile(t *testing.T) {
	b := &fakeBackend{}
	m := runtimeModel(t, b)
	m.syncRuntime()
	defer m.stopRuntime()
	m.runtime.snapshot = liveRuntime()
	b.snapshot.Desired = app.DesiredState{Known: true, Running: false}
	b.snapshot.Applied = "changed-from-cli"
	b.snapshot.Service = service.Status{State: "inactive"}
	msg := runtimeResultMsg{generation: m.runtime.generation, events: fixtureRuntimeEvents(app.RuntimeSnapshot{State: "stopped", ObservedAt: time.Now(), Service: b.snapshot.Service}, 1)}
	commands := commandBatch(t, m.acceptRuntime(msg))
	if len(commands) != 2 {
		t.Fatal("service transition did not schedule exactly one snapshot refresh")
	}
	m, _ = update(m, commands[1]())
	if !m.snapshot.Desired.Known || m.snapshot.Desired.Running || m.snapshot.Applied != "changed-from-cli" || m.snapshot.Service.Active {
		t.Fatal("runtime transition retained another client's intent")
	}
}

func TestRuntimeCardDistinguishesMissingValuesFromZero(t *testing.T) {
	o := dashboardOptions{Snapshot: app.Snapshot{Installed: true}, Runtime: liveRuntime(), RuntimeReceived: true}
	card := func() string { return ansi.Strip(strings.Join(overviewTrafficCard(o, 72).lines, "\n")) }
	if value := card(); !strings.Contains(value, "Since core start") || !strings.Contains(value, "1.0 MiB") || !strings.Contains(value, "2.0 KiB/s") || !strings.Contains(value, "16.0 MiB") {
		t.Fatalf("actual runtime metrics missing: %s", value)
	}
	o.Runtime.Traffic, o.Runtime.Memory = nil, nil
	if value := card(); !strings.Contains(value, "Partially available") || strings.Contains(value, "0 B/s") || strings.Contains(value, "16.0 MiB") {
		t.Fatalf("missing metric appeared as zero or stale data: %s", value)
	}
	o.Runtime.State = "unavailable"
	if value := card(); strings.Contains(value, "1.0 MiB") || !strings.Contains(value, "API unavailable") {
		t.Fatalf("unavailable runtime displayed old counters: %s", value)
	}
}

func TestRuntimeHandshakeDoesNotAdvanceMetricClocksOrClearValues(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	s := liveRuntime()
	s.ObservedAt = time.Now().Add(-time.Second)
	m = acceptRuntimeFixture(m, s)
	at := m.runtime.observed[app.RuntimeConnections]
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeConnections, Instance: 1, ObservedAt: time.Now(), Connected: true})
	if m.connectionNow() != at || m.runtime.snapshot.Connections != s.Connections || len(m.runtime.history.points) != 1 {
		t.Fatal("handshake fabricated a metric timestamp or cleared a value")
	}
}

func TestNewerLiveStatusSurvivesAnOlderFullSnapshotWithoutClearingHistory(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	observed := time.Now().Add(-time.Second)
	s := liveRuntime()
	s.ObservedAt = observed
	s.Service.PID = 43
	m = acceptRuntimeFixture(m, s)
	previous := m.snapshot
	previous.Service.PID = 42
	previous.ServiceObservedAt = observed.Add(-time.Second)
	previous.Applied = "fresh-profile-metadata"
	m.adoptSnapshot(previous)
	m.syncRuntime()
	if m.snapshot.Service.PID != 43 || len(m.runtime.history.points) != 1 || m.snapshot.Applied != "fresh-profile-metadata" {
		t.Fatal("old full status replaced current process, discarded history or lost profile metadata")
	}
}
