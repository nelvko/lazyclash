package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func logsModel() Model {
	m := ready(&fakeBackend{})
	m.page, m.focus = "logs", "logs.list"
	m.logs = newLogsState()
	return m
}

func TestLogsRetainBoundedEventsWithFixedControlsAndScrollableHistory(t *testing.T) {
	m := logsModel()
	for n := 0; n < 300; n++ {
		m.appendLogs([]mihomo.LogEntry{{Time: time.Unix(int64(n), 0), Level: "info", Message: fmt.Sprintf("event-%03d", n)}})
	}
	if len(m.logs.entries) != retainedLogLimit || m.logs.entries[0].Message != "event-100" {
		t.Fatal("log retention was unbounded or kept the oldest discarded entry")
	}
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {120, 18}} {
		m.width, m.height = size[0], size[1]
		m.resetLogsView()
		body, hits := m.logsBody(dashboardBodyWidth(dashboardOptions{Width: m.width, Height: m.height, Page: m.page}))
		plain := ansi.Strip(body)
		if !strings.Contains(plain, "event-299") || strings.Contains(plain, "event-100") {
			t.Fatalf("%v did not retain newest rows", size)
		}
		if len(strings.Split(body, "\n")) > dashboardGeometry(dashboardOptions{Width: m.width, Height: m.height, Page: m.page}).height {
			t.Fatalf("%v log viewport exceeded its available height", size)
		}
		assertRegions(t, hits, dashboardBodyWidth(dashboardOptions{Width: m.width, Height: m.height, Page: m.page}), m.height)
	}
	for n := 0; n < 64; n++ {
		m.logsKey(special(tea.KeyHome))
		m.logsKey(special(tea.KeyUp))
		if m.logs.anchor.entry != m.logs.first || m.logs.anchor.row != 0 {
			t.Fatal("repeated Home and Up escaped the bounded history")
		}
		m.logsBody(80)
	}
	body, _ := m.logsBody(80)
	if !m.logs.holding || m.logs.paused || !strings.Contains(ansi.Strip(body), "event-100") {
		t.Fatal("Home did not freeze the viewport and expose the oldest retained rows")
	}
}

func TestLogsStreamUsesTheSharedRuntimeAndRejectsLateMessages(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	b := &fakeBackend{streamRuntime: func(ctx context.Context, emit func(app.RuntimeEvent)) error {
		close(started)
		emit(logSessionEvent("captured event"))
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}}
	m := sessionLogsModel(t, b)
	cmd := m.syncRuntime()
	primeLogRuntime(m)
	reader, done := runLogSession(t, cmd)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("shared stream did not start")
	}
	reader = m.acceptRuntime(readLogSessionMessage(t, reader))
	if reader == nil || len(m.logs.entries) != 1 || m.logs.entries[0].Message != "captured event" {
		t.Fatal("shared stream event did not enter the retained log view")
	}
	id, ctx := m.runtime.generation, m.runtime.ctx
	m.pauseLogs()
	select {
	case <-stopped:
		t.Fatal("pausing display canceled capture")
	default:
	}
	m.acceptRuntime(runtimeResultMsg{generation: id, events: []app.RuntimeEvent{logSessionEvent("while paused")}})
	if len(m.logs.entries) != 2 || len(m.logs.frozen) != 1 || !m.logs.active || ctx.Err() != nil {
		t.Fatal("paused display did not keep collecting into its independent live buffer")
	}
	if m.resumeLogs() != nil || m.runtime.generation != id || len(m.logs.entries) != 2 {
		t.Fatal("resuming display replaced capture or injected a synthetic record")
	}
	m.stopRuntime()
	waitLogSessionExit(t, done)
	m.acceptRuntime(runtimeResultMsg{generation: id, events: []app.RuntimeEvent{logSessionEvent("late event")}})
	if len(m.logs.entries) != 2 || m.logs.active {
		t.Fatal("late event changed a stopped shared session")
	}
}

func TestLogsSearchDigitsAndPasteStayWithinInput(t *testing.T) {
	m := logsModel()
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "q1234567 needle"}, {Level: "error", Message: "other"}})
	m.logsActivate("logs.search")
	for _, value := range []string{"q", "1", "2", "3", "4", "5", "6", "7"} {
		m, _ = update(m, key(value))
	}
	if m.page != "logs" || m.quitting || m.logs.search.Value() != "q1234567" || m.logs.paused {
		t.Fatal("search input was stolen by global commands")
	}
	m.logs.search.Reset()
	m, _ = update(m, tea.PasteMsg{Content: "NEEDLE"})
	m, _ = update(m, special(tea.KeyEnter))
	if m.logs.editing || m.focus != "logs.list" {
		t.Fatal("search did not return focus to the log viewport")
	}
	body, _ := m.logsBody(80)
	if !strings.Contains(ansi.Strip(body), "needle") || strings.Contains(ansi.Strip(body), "other") {
		t.Fatal("search did not filter retained events case-insensitively")
	}
}

func TestLogsFailureKeepsSharedSessionAndOtherSourcesAvailable(t *testing.T) {
	m := sessionLogsModel(t, &fakeBackend{})
	m.syncRuntime()
	primeLogRuntime(m)
	m.acceptRuntimeEvent(logSessionEvent("observed earlier"))
	m.runtime.snapshot.Traffic = &mihomo.Traffic{Up: 123, Down: 456}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeLogs, Instance: 1, ObservedAt: time.Now(), Err: errors.New("Mihomo log stream interrupted")})
	if m.syncRuntime() != nil || !m.logs.reconnecting || !m.logs.active || !m.runtime.active || m.runtime.snapshot.Traffic.Down != 456 {
		t.Fatal("log failure stopped the shared session or erased another source")
	}
	body, _ := m.logsBody(80)
	for _, part := range []string{m.tr("logs.reconnecting"), "Mihomo log stream interrupted", "observed earlier"} {
		if !strings.Contains(ansi.Strip(body), part) {
			t.Fatalf("missing failure observation %q", part)
		}
	}
	m.acceptRuntimeEvent(logSessionEvent("restored"))
	if !m.logs.recovered || m.logs.reconnecting || m.logs.err != nil || len(m.logs.entries) != 2 {
		t.Fatal("recovery did not clear only the log error or injected a synthetic record")
	}
}

func TestLogsResizeHelpAndDepartureKeepTheSharedSessionAndHistory(t *testing.T) {
	m := sessionLogsModel(t, &fakeBackend{})
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "kept"}})
	m.syncRuntime()
	id, ctx := m.runtime.generation, m.runtime.ctx
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {120, 18}} {
		updated, _ := update(*m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		*m = updated
		if m.runtime.generation != id || ctx.Err() != nil || len(m.logs.entries) != 1 {
			t.Fatal("resize replaced the shared task or its retained data")
		}
		assertCanvas(t, m.screen, size[0], size[1])
		assertRegions(t, m.targets, size[0], size[1])
	}
	m.helpOpen = true
	if m.syncRuntime() != nil || ctx.Err() != nil || !m.logs.active {
		t.Fatal("help interrupted the shared session")
	}
	m.helpOpen = false
	m.browsePage("overview")
	if m.syncRuntime() != nil || ctx.Err() != nil || m.runtime.generation != id {
		t.Fatal("page departure interrupted the shared session")
	}
	m.browsePage("logs")
	if m.syncRuntime() != nil || len(m.logs.entries) != 1 || m.runtime.generation != id {
		t.Fatal("returning to logs restarted capture or inserted a synthetic entry")
	}
}
