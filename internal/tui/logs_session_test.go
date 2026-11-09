package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func sessionLogsModel(t *testing.T, backend *fakeBackend) *Model {
	t.Helper()
	m := logsModel()
	m.ctx = t.Context()
	m.backend = backend
	m.snapshotReady, m.snapshot.Installed = true, true
	m.snapshot.Service = service.Status{Active: true, State: "active", PID: 4242}
	m.width, m.height = 120, 32
	t.Cleanup(func() {
		m.stopRuntime()
		m.stopCoreStatus()
		if m.cancel != nil {
			m.cancel()
		}
	})
	return &m
}

func primeLogRuntime(m *Model) {
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeStatus, Instance: 1, ObservedAt: time.Now(), Snapshot: app.RuntimeSnapshot{
		State: "ready", Service: m.snapshot.Service,
	}})
}

func logSessionEvent(message string) app.RuntimeEvent {
	at := time.Now()
	return app.RuntimeEvent{Source: app.RuntimeLogs, Instance: 1, ObservedAt: at,
		Log: &mihomo.LogEntry{Time: at, Level: "info", Message: message}}
}

func acceptLogFixture(m *Model, generation uint64, entries ...mihomo.LogEntry) {
	events := make([]app.RuntimeEvent, 0, len(entries))
	for _, entry := range entries {
		events = append(events, app.RuntimeEvent{Source: app.RuntimeLogs, Instance: m.runtime.instance, ObservedAt: time.Now(), Log: &entry})
	}
	m.acceptRuntime(runtimeResultMsg{generation: generation, events: events})
}

func runLogSession(t *testing.T, cmd tea.Cmd) (tea.Cmd, <-chan struct{}) {
	t.Helper()
	commands := commandBatch(t, cmd)
	if len(commands) != 2 {
		t.Fatalf("expected one shared session owner and one message reader; got %d commands", len(commands))
	}
	done := make(chan struct{})
	go func() {
		commands[0]()
		close(done)
	}()
	return commands[1], done
}

func readLogSessionMessage(t *testing.T, cmd tea.Cmd) runtimeResultMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing shared runtime message reader")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case raw := <-result:
		message, ok := raw.(runtimeResultMsg)
		if !ok {
			t.Fatalf("expected a shared runtime result; got %T", raw)
		}
		return message
	case <-time.After(3 * time.Second):
		t.Fatal("shared session did not deliver its next result")
		return runtimeResultMsg{}
	}
}

func waitLogSessionExit(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shared session did not release its streams after cancellation")
	}
}

func TestLogsSessionStartsFromRunningSnapshotBeforeOpeningLogs(t *testing.T) {
	m := New(t.Context(), &fakeBackend{}, "en")
	defer m.stopRuntime()
	if m.page != "overview" || m.syncRuntime() != nil || m.logs.active {
		t.Fatal("an unobserved installation started background subscriptions")
	}
	running := app.Snapshot{Installed: true, Service: service.Status{Active: true, State: "active", PID: 4242}}
	m, cmd := update(m, resultMsg{id: m.id, snapshot: running, hasSnapshot: true})
	if cmd == nil || m.page != "overview" || !m.runtime.active || !m.logs.active || !m.logs.started || m.runtime.ctx == nil {
		t.Fatal("the first running snapshot did not start shared collection from Overview")
	}
	ctx, generation := m.runtime.ctx, m.runtime.generation
	m.browsePage("logs")
	if m.syncRuntime() != nil || m.runtime.ctx != ctx || m.runtime.generation != generation {
		t.Fatal("opening Logs replaced the already collecting shared session")
	}

	unavailable := sessionLogsModel(t, &fakeBackend{})
	unavailable.snapshot.Installed = false
	if unavailable.syncRuntime() == nil || !unavailable.logs.active {
		t.Fatal("opening Logs did not start a diagnostic session for an unavailable core")
	}
}

func TestLogsSessionKeepsCollectingAcrossNavigationOverlaysAndOrdinaryJobs(t *testing.T) {
	started := make(chan context.Context, 1)
	input := make(chan app.RuntimeEvent, 1)
	backend := &fakeBackend{streamRuntime: func(ctx context.Context, emit func(app.RuntimeEvent)) error {
		started <- ctx
		for {
			select {
			case event := <-input:
				emit(event)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}}
	m := sessionLogsModel(t, backend)
	cmd := m.syncRuntime()
	primeLogRuntime(m)
	reader, done := runLogSession(t, cmd)
	ctx := <-started
	generation := m.runtime.generation
	stages := []struct {
		name string
		act  func()
	}{
		{"navigation", func() { m.browsePage("profiles") }},
		{"help", func() { m.helpOpen = true }},
		{"ports", func() { m.ports.open = true }},
		{"form", func() { m.form = "fixture" }},
		{"wizard", func() { m.wizard.open = true }},
		{"network", func() { m.wizard.networkOpen = true }},
		{"ordinary job", func() { m.newJob("refresh") }},
		{"ordinary cancellation", func() { m.cancelTask(false) }},
	}
	for index, stage := range stages {
		stage.act()
		if m.syncRuntime() != nil || !m.logs.active || m.runtime.ctx != ctx || ctx.Err() != nil || m.runtime.generation != generation {
			t.Fatalf("%s interrupted or replaced shared session collection", stage.name)
		}
		message := fmt.Sprintf("off-page event %d", index)
		input <- logSessionEvent(message)
		reader = m.acceptRuntime(readLogSessionMessage(t, reader))
		if m.logs.entries[len(m.logs.entries)-1].Message != message {
			t.Fatalf("%s discarded a live event outside the Logs page", stage.name)
		}
	}
	if m.page != "profiles" || len(m.logs.entries) != len(stages) {
		t.Fatal("background capture navigated the page or failed to keep the session cache")
	}
	m.stopRuntime()
	waitLogSessionExit(t, done)
}

func TestLogsSessionPauseFreezesIndependentHistoryWhileLiveCacheRemainsBounded(t *testing.T) {
	m := sessionLogsModel(t, &fakeBackend{})
	m.syncRuntime()
	primeLogRuntime(m)
	ctx, generation := m.runtime.ctx, m.runtime.generation
	for index := 0; index < retainedLogLimit; index++ {
		m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: fmt.Sprintf("before-%03d", index)}})
	}
	m.pauseLogs()
	frozen := strings.Join(m.logDocument(90).lines, "\n")
	for index := 0; index < retainedLogLimit+50; index++ {
		m.acceptRuntimeEvent(logSessionEvent(fmt.Sprintf("after-%03d", index)))
	}
	if got := strings.Join(m.logDocument(90).lines, "\n"); got != frozen {
		t.Fatal("live-cache eviction overwrote the paused display snapshot")
	}
	if len(m.logs.entries) != retainedLogLimit || len(m.logs.frozen) != retainedLogLimit || m.logs.entries[0].Message != "after-050" || m.logs.frozen[0].Message != "before-000" {
		t.Fatal("pause retained unbounded storage or shared the mutable live-cache slice")
	}
	if !m.logs.active || ctx.Err() != nil || m.runtime.ctx != ctx || m.runtime.generation != generation || m.logs.unread != retainedLogLimit {
		t.Fatal("pausing display interrupted collection or lost its bounded new-event count")
	}
	if cmd := m.resumeLogs(); cmd != nil || m.logs.paused || m.logs.frozen != nil || m.logs.holding || m.logs.unread != 0 || m.runtime.generation != generation {
		t.Fatal("resuming display replaced the stream or retained the frozen reading state")
	}
	if got := logReadingView(*m); !strings.Contains(got, "after-249") || strings.Contains(got, "before-") {
		t.Fatalf("resume did not show the newest continuously collected events:\n%s", got)
	}
	for _, entry := range m.logs.entries {
		if entry.Level == "gap" {
			t.Fatal("display pause fabricated a collection-gap log entry")
		}
	}
}

func TestLogsSessionSourceFailureAndRecoveryLeaveTheSharedGenerationIntact(t *testing.T) {
	m := sessionLogsModel(t, &fakeBackend{})
	m.syncRuntime()
	primeLogRuntime(m)
	ctx, generation := m.runtime.ctx, m.runtime.generation
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeLogs, Instance: 1, ObservedAt: time.Now(), Err: errors.New("fixture stream disconnected")})
	if !m.logs.active || !m.logs.reconnecting || m.logs.err == nil || ctx.Err() != nil || m.syncRuntime() != nil {
		t.Fatal("a disconnected log source lost the shared session or started a duplicate session")
	}
	m.acceptRuntimeEvent(logSessionEvent("restored event"))
	if !m.logs.active || m.logs.reconnecting || !m.logs.recovered || m.logs.err != nil || m.runtime.generation != generation || m.runtime.ctx != ctx {
		t.Fatal("source recovery replaced the shared session")
	}
	if len(m.logs.entries) != 1 || m.logs.entries[0].Message != "restored event" {
		t.Fatal("source recovery inserted synthetic records or lost the resumed real event")
	}
}

func TestLogsQuietHandshakeRecoversWithoutInventingAnEvent(t *testing.T) {
	m := sessionLogsModel(t, &fakeBackend{})
	m.syncRuntime()
	primeLogRuntime(m)
	generation := m.runtime.generation
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeLogs, Instance: 1, ObservedAt: time.Now(), Err: errors.New("fixture stream disconnected")})
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeLogs, Instance: 1, ObservedAt: time.Now(), Connected: true})
	if m.logs.err != nil || m.logs.reconnecting || !m.logs.recovered || !m.logs.active || len(m.logs.entries) != 0 || m.runtime.generation != generation {
		t.Fatal("quiet successful handshake waited for a log event, invented one, or replaced the session")
	}
}

func TestLogsSessionManualReconnectRejectsAllOldMessagesAndPreservesPausedView(t *testing.T) {
	backend := &fakeBackend{streamRuntime: func(ctx context.Context, _ func(app.RuntimeEvent)) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	m := sessionLogsModel(t, backend)
	cmd := m.syncRuntime()
	primeLogRuntime(m)
	reader, done := runLogSession(t, cmd)
	ctx, generation := m.runtime.ctx, m.runtime.generation
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "retained before reconnect"}})
	m.pauseLogs()
	frozen := strings.Join(m.logDocument(80).lines, "\n")
	if m.reconnectLogs() == nil || ctx.Err() == nil || m.runtime.ctx == ctx || m.runtime.generation == generation || !m.logs.active {
		t.Fatal("manual reconnect did not replace and invalidate the shared session")
	}
	waitLogSessionExit(t, done)
	if reader() != nil {
		t.Fatal("cancelled shared reader did not release promptly")
	}
	newGeneration := m.runtime.generation
	for _, event := range []app.RuntimeEvent{
		logSessionEvent("stale old event"),
		{Source: app.RuntimeLogs, Instance: 1, ObservedAt: time.Now(), Err: errors.New("stale old failure")},
	} {
		if m.acceptRuntime(runtimeResultMsg{generation: generation, events: []app.RuntimeEvent{event}}) != nil || m.runtime.generation != newGeneration || len(m.logs.entries) != 1 {
			t.Fatal("an old source event or failure changed the replacement session")
		}
	}
	if got := strings.Join(m.logDocument(80).lines, "\n"); !m.logs.paused || got != frozen {
		t.Fatal("manual reconnect discarded the paused reading snapshot")
	}
}

func TestLogsSessionQuitAndParentCancellationReleaseQuietSharedStreams(t *testing.T) {
	for _, mode := range []string{"quit", "parent"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{})
			backend := &fakeBackend{streamRuntime: func(ctx context.Context, _ func(app.RuntimeEvent)) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}}
			m := sessionLogsModel(t, backend)
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			m.ctx = parent
			reader, done := runLogSession(t, m.syncRuntime())
			<-started
			if mode == "quit" {
				m.requestQuit()
			} else {
				cancel()
				m.syncRuntime()
			}
			waitLogSessionExit(t, done)
			if m.runtime.active || m.runtime.pending || m.logs.active || m.runtime.ctx.Err() == nil || m.syncRuntime() != nil || reader() != nil {
				t.Fatal("exit left a quiet shared stream active, restarted it, or blocked its reader")
			}
		})
	}
}
