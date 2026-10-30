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
		m.logs.offset = 0
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
		if m.logs.offset != maxLogScrollRows {
			t.Fatal("repeated Home and Up overflowed the bounded history offset")
		}
		m.logsBody(80)
	}
	body, _ := m.logsBody(80)
	if !m.logs.paused || !strings.Contains(ansi.Strip(body), "event-100") {
		t.Fatal("Home did not pause and expose the oldest retained rows")
	}
}

func TestLogsStreamIsSingleCancellableTaskAndRejectsLateMessages(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	b := &fakeBackend{streamLogs: func(ctx context.Context, emit func(mihomo.LogEntry)) error {
		close(started)
		emit(mihomo.LogEntry{Level: "info", Message: "captured event"})
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}}
	m := logsModel()
	m.backend = b
	commands := commandBatch(t, m.syncLogs())
	if len(commands) != 2 || m.syncLogs() != nil || m.busy {
		t.Fatal("log capture did not have an independent single-task lifecycle")
	}
	go commands[0]()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	msg := commands[1]().(logsResultMsg)
	next := m.acceptLogs(msg)
	if next == nil || len(m.logs.entries) != 1 || m.logs.entries[0].Message != "captured event" {
		t.Fatal("stream event did not enter the bounded retained view")
	}
	id := m.logs.generation
	m.pauseLogs()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("pause did not cancel the owned capture")
	}
	m.acceptLogs(logsResultMsg{generation: id, entries: []mihomo.LogEntry{{Message: "late event"}}})
	if len(m.logs.entries) != 1 || m.logs.active {
		t.Fatal("late event changed a paused capture")
	}
	m.resumeLogs()
	defer m.stopLogs()
	if len(m.logs.entries) != 2 || m.logs.entries[1].Level != "gap" {
		t.Fatal("resume did not disclose the uncollected time gap")
	}
}

func TestLogsSearchDigitsAndPasteStayWithinInput(t *testing.T) {
	m := logsModel()
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "q1234567 needle"}, {Level: "error", Message: "other"}})
	m.logsActivate("logs.search")
	for _, value := range []string{"q", "1", "2", "3", "4", "5", "6", "7"} {
		m, _ = update(m, key(value))
	}
	if m.page != "logs" || m.quitting || m.logs.search.Value() != "q1234567" || !m.logs.paused {
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

func TestLogsFailureDoesNotLoopAndKeepsObservedEntriesClearlyMarked(t *testing.T) {
	m := logsModel()
	m.logs.active, m.logs.pending, m.logs.generation = true, true, 8
	m.acceptLogs(logsResultMsg{generation: 8, entries: []mihomo.LogEntry{{Level: "info", Message: "observed earlier"}}, done: true, err: errors.New("Mihomo is not running")})
	if m.syncLogs() != nil || !m.logs.ended || m.logs.active {
		t.Fatal("failed stream immediately restarted in a tight loop")
	}
	body, _ := m.logsBody(80)
	for _, part := range []string{"last observed", "Mihomo is not running", "observed earlier"} {
		if !strings.Contains(ansi.Strip(body), part) {
			t.Fatalf("missing failure observation %q", part)
		}
	}
	m.resumeLogs()
	defer m.stopLogs()
	if m.logs.entries[len(m.logs.entries)-1].Level != "gap" {
		t.Fatal("reconnecting after failure did not mark its observation gap")
	}
}

func TestLogsResizeHelpAndDepartureCancelWithoutLosingRetainedState(t *testing.T) {
	m := logsModel()
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "kept"}})
	m.syncLogs()
	id, ctx := m.logs.generation, m.logs.ctx
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {120, 18}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if m.logs.generation != id || ctx.Err() != nil || len(m.logs.entries) != 1 {
			t.Fatal("resize replaced the log task or its retained data")
		}
		assertCanvas(t, m.screen, size[0], size[1])
		assertRegions(t, m.targets, size[0], size[1])
	}
	m.helpOpen = true
	m.syncLogs()
	if ctx.Err() == nil || m.logs.active || len(m.logs.entries) != 1 {
		t.Fatal("help did not cancel capture while retaining the observation")
	}
	m.helpOpen = false
	m.syncLogs()
	defer m.stopLogs()
	if len(m.logs.entries) != 2 || m.logs.entries[1].Level != "gap" {
		t.Fatal("return from help silently concealed the capture gap")
	}
}
