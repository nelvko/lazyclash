package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
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
	m.backend = backend
	m.width, m.height = 120, 32
	t.Cleanup(func() {
		m.stopLogs()
		m.stopCoreStatus()
		if m.cancel != nil {
			m.cancel()
		}
	})
	return &m
}

func runLogSession(t *testing.T, cmd tea.Cmd) (tea.Cmd, <-chan struct{}) {
	t.Helper()
	commands := commandBatch(t, cmd)
	if len(commands) != 2 {
		t.Fatalf("expected one session owner and one message reader; got %d commands", len(commands))
	}
	done := make(chan struct{})
	go func() {
		commands[0]()
		close(done)
	}()
	return commands[1], done
}

func readLogSessionMessage(t *testing.T, cmd tea.Cmd) logsResultMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing log message reader")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case raw := <-result:
		message, ok := raw.(logsResultMsg)
		if !ok {
			t.Fatalf("expected a log result; got %T", raw)
		}
		return message
	case <-time.After(3 * time.Second):
		t.Fatal("session did not deliver its next log result")
		return logsResultMsg{}
	}
}

func waitLogSessionExit(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("log session did not release its stream or retry timer after cancellation")
	}
}

func TestLogsSessionStartsFromRunningSnapshotBeforeOpeningLogs(t *testing.T) {
	m := New(t.Context(), &fakeBackend{}, "en")
	defer m.stopLogs()
	if m.page != "overview" || m.syncLogs() != nil || m.logs.active {
		t.Fatal("an unobserved installation started background log polling")
	}
	running := app.Snapshot{Installed: true, Service: service.Status{Active: true, State: "active", PID: 4242}}
	m, cmd := update(m, resultMsg{id: m.id, snapshot: running, hasSnapshot: true})
	if cmd == nil || m.page != "overview" || !m.logs.active || !m.logs.started || m.logs.ctx == nil {
		t.Fatal("the first running snapshot did not start session log collection from Overview")
	}
	ctx, generation := m.logs.ctx, m.logs.generation
	m.browsePage("logs")
	if m.syncLogs() != nil || m.logs.ctx != ctx || m.logs.generation != generation {
		t.Fatal("opening Logs replaced the already collecting session")
	}

	unavailable := sessionLogsModel(t, &fakeBackend{})
	if unavailable.syncLogs() == nil || !unavailable.logs.active {
		t.Fatal("opening Logs did not start a diagnostic attempt for an unavailable core")
	}
}

func TestLogsSessionKeepsCollectingAcrossNavigationOverlaysAndOrdinaryJobs(t *testing.T) {
	started := make(chan context.Context, 1)
	input := make(chan mihomo.LogEntry, 1)
	backend := &fakeBackend{streamLogs: func(ctx context.Context, emit func(mihomo.LogEntry)) error {
		started <- ctx
		for {
			select {
			case entry := <-input:
				emit(entry)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}}
	m := sessionLogsModel(t, backend)
	reader, done := runLogSession(t, m.syncLogs())
	ctx := <-started
	generation := m.logs.generation
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
		if m.syncLogs() != nil || !m.logs.active || m.logs.ctx != ctx || ctx.Err() != nil || m.logs.generation != generation {
			t.Fatalf("%s interrupted or replaced session collection", stage.name)
		}
		message := fmt.Sprintf("off-page event %d", index)
		input <- mihomo.LogEntry{Level: "info", Message: message}
		reader = m.acceptLogs(readLogSessionMessage(t, reader))
		if m.logs.entries[len(m.logs.entries)-1].Message != message {
			t.Fatalf("%s discarded a live event outside the Logs page", stage.name)
		}
	}
	if m.page != "profiles" || len(m.logs.entries) != len(stages) {
		t.Fatal("background capture navigated the page or failed to keep the session cache")
	}
	m.stopLogs()
	waitLogSessionExit(t, done)
}

func TestLogsSessionPauseFreezesIndependentHistoryWhileLiveCacheRemainsBounded(t *testing.T) {
	m := sessionLogsModel(t, &fakeBackend{})
	m.syncLogs()
	ctx, generation := m.logs.ctx, m.logs.generation
	for index := 0; index < retainedLogLimit; index++ {
		m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: fmt.Sprintf("before-%03d", index)}})
	}
	m.pauseLogs()
	frozen := strings.Join(m.logDocument(90).lines, "\n")
	for index := 0; index < retainedLogLimit+50; index++ {
		m.acceptLogs(logsResultMsg{generation: generation, entries: []mihomo.LogEntry{{Level: "info", Message: fmt.Sprintf("after-%03d", index)}}})
	}
	if got := strings.Join(m.logDocument(90).lines, "\n"); got != frozen {
		t.Fatal("live-cache eviction overwrote the paused display snapshot")
	}
	if len(m.logs.entries) != retainedLogLimit || len(m.logs.frozen) != retainedLogLimit || m.logs.entries[0].Message != "after-050" || m.logs.frozen[0].Message != "before-000" {
		t.Fatal("pause retained unbounded storage or shared the mutable live-cache slice")
	}
	if !m.logs.active || ctx.Err() != nil || m.logs.ctx != ctx || m.logs.generation != generation || m.logs.unread != retainedLogLimit {
		t.Fatal("pausing display interrupted collection or lost its bounded new-event count")
	}
	if cmd := m.resumeLogs(); cmd != nil || m.logs.paused || m.logs.frozen != nil || m.logs.holding || m.logs.unread != 0 || m.logs.generation != generation {
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

func TestLogsSessionRetriesAfterDisconnectAndRecoversWithoutChangingGeneration(t *testing.T) {
	attempted := make(chan time.Time, 2)
	var calls atomic.Int32
	backend := &fakeBackend{streamLogs: func(ctx context.Context, emit func(mihomo.LogEntry)) error {
		attempted <- time.Now()
		if calls.Add(1) == 1 {
			return errors.New("fixture stream disconnected")
		}
		emit(mihomo.LogEntry{Level: "info", Message: "restored event"})
		<-ctx.Done()
		return ctx.Err()
	}}
	m := sessionLogsModel(t, backend)
	reader, done := runLogSession(t, m.syncLogs())
	first := <-attempted
	ctx, generation := m.logs.ctx, m.logs.generation
	reader = m.acceptLogs(readLogSessionMessage(t, reader))
	if !m.logs.active || !m.logs.reconnecting || m.logs.err == nil || ctx.Err() != nil || m.syncLogs() != nil {
		t.Fatal("a disconnected stream lost the session owner or started a duplicate retry loop")
	}
	reader = m.acceptLogs(readLogSessionMessage(t, reader))
	second := <-attempted
	if second.Sub(first) < 900*time.Millisecond {
		t.Fatal("failed stream retried without the initial backoff")
	}
	if calls.Load() != 2 || !m.logs.active || m.logs.reconnecting || !m.logs.recovered || m.logs.err != nil || m.logs.generation != generation || m.logs.ctx != ctx {
		t.Fatal("automatic recovery failed to restore observation within the existing session")
	}
	if len(m.logs.entries) != 1 || m.logs.entries[0].Message != "restored event" {
		t.Fatal("recovery inserted synthetic records or lost the resumed real event")
	}
	m.stopLogs()
	waitLogSessionExit(t, done)
	if reader() != nil {
		t.Fatal("cancelled quiet-session reader produced a new result")
	}
}

func TestLogsSessionManualReconnectCancelsBackoffAndRejectsAllOldMessages(t *testing.T) {
	backend := &fakeBackend{streamLogs: func(context.Context, func(mihomo.LogEntry)) error {
		return errors.New("fixture cannot connect")
	}}
	m := sessionLogsModel(t, backend)
	reader, done := runLogSession(t, m.syncLogs())
	ctx, generation := m.logs.ctx, m.logs.generation
	reader = m.acceptLogs(readLogSessionMessage(t, reader))
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "retained before reconnect"}})
	m.pauseLogs()
	frozen := strings.Join(m.logDocument(80).lines, "\n")
	if m.reconnectLogs() == nil || ctx.Err() == nil || m.logs.ctx == ctx || m.logs.generation == generation || !m.logs.active {
		t.Fatal("manual reconnect did not replace and invalidate the old session")
	}
	waitLogSessionExit(t, done)
	if reader() != nil {
		t.Fatal("cancelled retry reader did not release promptly")
	}
	newGeneration := m.logs.generation
	for _, old := range []logsResultMsg{
		{generation: generation, entries: []mihomo.LogEntry{{Level: "error", Message: "stale old event"}}},
		{generation: generation, done: true, err: errors.New("stale old failure")},
	} {
		if m.acceptLogs(old) != nil || m.logs.generation != newGeneration || m.logs.err != nil || len(m.logs.entries) != 1 {
			t.Fatal("an old stream event or failure changed the replacement session")
		}
	}
	if got := strings.Join(m.logDocument(80).lines, "\n"); !m.logs.paused || got != frozen {
		t.Fatal("manual reconnect discarded the paused reading snapshot")
	}
}

func TestLogsSessionQuitAndParentCancellationReleaseQuietStreams(t *testing.T) {
	for _, mode := range []string{"quit", "parent"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{})
			backend := &fakeBackend{streamLogs: func(ctx context.Context, _ func(mihomo.LogEntry)) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}}
			m := sessionLogsModel(t, backend)
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			m.ctx = parent
			reader, done := runLogSession(t, m.syncLogs())
			<-started
			if mode == "quit" {
				m.requestQuit()
			} else {
				cancel()
				m.syncLogs()
			}
			waitLogSessionExit(t, done)
			if m.logs.active || m.logs.pending || m.logs.ctx.Err() == nil || m.syncLogs() != nil || reader() != nil {
				t.Fatal("exit left a quiet stream active, restarted it, or blocked its reader")
			}
		})
	}
}

func TestLogsSessionRetryDelayIsBoundedAndNeverSpins(t *testing.T) {
	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{{-1, time.Second}, {0, time.Second}, {1, 2 * time.Second}, {2, 4 * time.Second}, {3, 8 * time.Second}, {4, 16 * time.Second}, {5, 30 * time.Second}, {1000, 30 * time.Second}} {
		if got := logRetryDelay(tc.attempt); got != tc.want {
			t.Fatalf("attempt %d: got %s; want %s", tc.attempt, got, tc.want)
		}
	}
}
