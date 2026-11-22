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

func fixtureConnection(id, host string, offset int) mihomo.Connection {
	return mihomo.Connection{ID: id, Metadata: mihomo.ConnectionMetadata{Network: "tcp", Type: "HTTP", Host: host, DestinationIP: "192.0.2.10", DestinationPort: "443", SourceIP: "127.0.0.1", SourcePort: "12345", Process: "fixture", ProcessPath: "/tmp/fixture"}, Start: time.Unix(1000+int64(offset), 0), Upload: 123, Download: 456, Chains: []string{"DIRECT", "PROXY"}, Rule: "MATCH"}
}

func connectionsModel(t *testing.T, b *fakeBackend) Model {
	t.Helper()
	m := runtimeModel(t, b)
	m.page, m.focus = "connections", "connection.first"
	m.runtime.active, m.runtime.received = true, true
	m.runtime.snapshot = liveRuntime()
	m.runtime.snapshot.Connections.Connections = []mihomo.Connection{fixtureConnection("first", "one.example.invalid", 1), fixtureConnection("second", "two.example.invalid", 0)}
	m.reconcileConnections()
	m.layout()
	return m
}

func TestConnectionsSearchAcceptsNormalInputAndOpensExactMatch(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m, _ = update(m, key("/"))
	for _, value := range []string{"q", "1", "2", "3", "4", "5"} {
		m, _ = update(m, key(value))
	}
	if m.page != "connections" || m.quitting || m.connections.search.Value() != "q12345" {
		t.Fatal("search input triggered global navigation or quit")
	}
	m.connections.search.Reset()
	m, _ = update(m, tea.PasteMsg{Content: "SECOND"})
	if list := m.filteredConnections(); len(list) != 1 || list[0].ID != "second" {
		t.Fatal("case-insensitive ID search did not select exact connection")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.connections.editing || m.focus != "connection.second" || m.connections.detail != nil {
		t.Fatal("search completion did not focus its matched connection")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.connections.detail == nil || m.connections.detail.ID != "second" || !strings.Contains(ansi.Strip(m.screen), "Connection details") {
		t.Fatal("Enter did not open the selected connection details")
	}
}

func TestConnectionsCloseShortcutOnlyOpensInspector(t *testing.T) {
	var requests int
	m := connectionsModel(t, &fakeBackend{
		closeConnection:     func(context.Context, string, time.Time) error { requests++; return nil },
		closeAllConnections: func(context.Context) error { requests++; return nil },
	})
	for _, dismiss := range []tea.KeyPressMsg{special(tea.KeyEnter), special(tea.KeyEscape)} {
		for _, input := range []string{"x", "x", "x", "y"} {
			var cmd tea.Cmd
			m, cmd = update(m, key(input))
			if cmd != nil || m.busy || requests != 0 || m.connections.confirming || m.focus != "conn.back" || m.connections.detail == nil || m.connections.detail.ID != "first" {
				t.Fatalf("%q should only leave the inspector open with dismiss focused; requests=%d focus=%s", input, requests, m.focus)
			}
		}
		var cmd tea.Cmd
		m, cmd = update(m, dismiss)
		if cmd != nil || m.busy || requests != 0 || m.connections.detail != nil || m.focus != "connection.first" {
			t.Fatal("dismissing the inspector should return to the selected row without disconnecting")
		}
	}
}

func TestConnectionInspectorExplicitActivationClosesExactTargetOnce(t *testing.T) {
	for _, method := range []string{"click", "enter", "space"} {
		t.Run(method, func(t *testing.T) {
			var closed []string
			var closedStart time.Time
			m := connectionsModel(t, &fakeBackend{
				closeConnection: func(_ context.Context, id string, start time.Time) error {
					closed = append(closed, id)
					closedStart = start
					return nil
				},
				closeAllConnections: func(context.Context) error { t.Fatal("action closed all connections"); return nil },
			})
			m, _ = update(m, key("x"))
			fresh := liveRuntime()
			fresh.Connections.Connections = append([]mihomo.Connection{fixtureConnection("newest", "new.example.invalid", 10)}, m.runtime.snapshot.Connections.Connections...)
			m = acceptConnectionsSample(m, fresh)
			if m.connections.detail.ID != "first" || m.connections.selected != "first" || m.focus != "conn.back" {
				t.Fatal("refresh moved the inspector or its default focus to the newly inserted row")
			}
			var cmd tea.Cmd
			if method == "click" {
				m, cmd = click(m, findTarget(t, m, "conn.close"))
			} else {
				m, _ = update(m, special(tea.KeyTab))
				if m.focus != "conn.close" {
					t.Fatal("Tab did not focus the disconnect button")
				}
				for _, input := range []string{"x", "y"} {
					m, cmd = update(m, key(input))
					if cmd != nil || m.busy || m.focus != "conn.close" || len(closed) != 0 {
						t.Fatalf("%q activated the focused disconnect button", input)
					}
				}
				input := special(tea.KeyEnter)
				if method == "space" {
					input = special(tea.KeySpace)
				}
				m, cmd = update(m, input)
			}
			if !m.busy || m.connections.confirming || hasAction(m.targets, "conn.close") {
				t.Fatal("activating the explicit button must start work directly and disable further clicks")
			}
			if duplicate := m.connectionActivate("conn.close"); duplicate != nil {
				t.Fatal("busy inspector scheduled a duplicate disconnect")
			}
			result := actionCmd(t, cmd)()
			if len(closed) != 1 || closed[0] != "first" || !closedStart.Equal(fixtureConnection("first", "", 1).Start) {
				t.Fatalf("expected a single close for the inspected row; got %v", closed)
			}
			m, _ = update(m, result)
			if m.busy || m.failure != "" || m.message != m.tr("connections.close_requested") || !m.runtime.active {
				t.Fatal("successful close did not report its result or preserve monitoring")
			}
		})
	}
}

func TestConnectionCloseRetryRequiresFreshLiveObservationAfterSuccess(t *testing.T) {
	var closedStarts []time.Time
	m := connectionsModel(t, &fakeBackend{closeConnection: func(_ context.Context, _ string, start time.Time) error {
		closedStarts = append(closedStarts, start)
		return nil
	}})
	snapshot := m.runtime.snapshot
	snapshot.ObservedAt = time.Now().Add(-5 * time.Second)
	m = acceptRuntimeFixture(m, snapshot)
	m, _ = update(m, key("x"))
	m, cmd := click(m, findTarget(t, m, "conn.close"))
	queued := time.Now()
	m, _ = update(m, actionCmd(t, cmd)())
	m, _ = update(m, special(tea.KeyEscape))
	m, _ = update(m, special(tea.KeyEnter))
	if m.connections.detail == nil || m.connectionActivate("conn.close") != nil || m.busy || len(closedStarts) != 1 {
		t.Fatal("reopening the same cached row enabled a duplicate close")
	}
	for _, event := range []app.RuntimeEvent{
		{Source: app.RuntimeConnections, ObservedAt: queued, Connections: snapshot.Connections},
		{Source: app.RuntimeConnections, ObservedAt: snapshot.ObservedAt, Connections: snapshot.Connections},
		{Source: app.RuntimeConnections, ObservedAt: time.Now(), Connected: true},
		{Source: app.RuntimeConnections, ObservedAt: time.Now(), Err: errors.New("stream reset")},
	} {
		event.Instance = m.runtime.instance
		m.acceptRuntimeEvent(event)
		m.layout()
		if hasAction(m.targets, "conn.close") || m.connectionActivate("conn.close") != nil {
			t.Fatalf("non-confirming observation re-enabled disconnect: %+v", event)
		}
	}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeConnections, Instance: m.runtime.instance, ObservedAt: time.Now(), Connections: snapshot.Connections})
	m.layout()
	m, cmd = click(m, findTarget(t, m, "conn.close"))
	m, _ = update(m, actionCmd(t, cmd)())
	if len(closedStarts) != 2 || !closedStarts[0].Equal(closedStarts[1]) {
		t.Fatal("a deliberate retry failed after a fresh observation confirmed the same connection remained active")
	}
	replacement := snapshot.Connections.Connections[0]
	replacement.Start = replacement.Start.Add(time.Minute)
	fresh := *snapshot.Connections
	fresh.Connections = []mihomo.Connection{replacement}
	m.acceptRuntimeEvent(app.RuntimeEvent{Source: app.RuntimeConnections, Instance: m.runtime.instance, ObservedAt: time.Now(), Connections: &fresh})
	m.layout()
	if hasAction(m.targets, "conn.close") {
		t.Fatal("an old inspector exposed the replacement lifetime’s close button")
	}
	m, _ = update(m, special(tea.KeyEscape))
	m, _ = update(m, special(tea.KeyEnter))
	m, cmd = click(m, findTarget(t, m, "conn.close"))
	m, _ = update(m, actionCmd(t, cmd)())
	if len(closedStarts) != 3 || !closedStarts[2].Equal(replacement.Start) {
		t.Fatal("a fresh inspector could not explicitly close the replacement lifetime")
	}
}

func TestConnectionsGoneOrReusedDuringInspectionCannotCloseAnotherLifetime(t *testing.T) {
	for _, id := range []string{"other", "first"} {
		t.Run(id, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{closeConnection: func(context.Context, string, time.Time) error {
				t.Fatal("closed an uninspected connection lifetime")
				return nil
			}})
			m, _ = update(m, key("x"))
			m, _ = update(m, special(tea.KeyTab))
			fresh := liveRuntime()
			fresh.Connections.Connections = []mihomo.Connection{fixtureConnection(id, "replacement.example.invalid", 2)}
			m = acceptConnectionsSample(m, fresh)
			if m.connections.detail == nil || m.connections.detail.Metadata.Host != "one.example.invalid" || m.detailActive() || m.focus != "conn.back" || hasAction(m.targets, "conn.close") {
				t.Fatal("ended or replaced target was not made read-only")
			}
			if cmd := m.connectionActivate("conn.close"); cmd != nil || m.busy {
				t.Fatal("stale button activation scheduled a close for another connection")
			}
		})
	}
}

func TestConnectionCloseFailurePreservesDetailsAndCanRetry(t *testing.T) {
	attempts := 0
	m := connectionsModel(t, &fakeBackend{closeConnection: func(_ context.Context, id string, _ time.Time) error {
		if id != "first" {
			t.Fatalf("attempt used wrong ID %q", id)
		}
		attempts++
		if attempts == 1 {
			return errors.New("controller unavailable")
		}
		return nil
	}})
	m, _ = update(m, key("x"))
	for attempt := 1; attempt <= 2; attempt++ {
		var cmd tea.Cmd
		m, cmd = click(m, findTarget(t, m, "conn.close"))
		m, _ = update(m, actionCmd(t, cmd)())
		if m.busy || attempts != attempt {
			t.Fatalf("explicit activation %d did not finish exactly once", attempt)
		}
		if attempt == 1 {
			if m.connections.detail == nil || m.connections.detail.ID != "first" || !strings.Contains(m.failure, "controller unavailable") {
				t.Fatal("close error was hidden or lost its target")
			}
			fresh := liveRuntime()
			fresh.Connections.Connections = []mihomo.Connection{fixtureConnection("first", "one.example.invalid", 1)}
			m = acceptConnectionsSample(m, fresh)
		} else if m.failure != "" {
			t.Fatal("successful retry retained an error")
		}
	}
}

func TestConnectionsLayoutAndFocusedRowSurviveResize(t *testing.T) {
	long := strings.Repeat("中文长路径/", 25)
	for _, size := range [][2]int{{160, 45}, {120, 32}, {100, 28}, {80, 24}, {60, 20}, {120, 18}, {20, 7}, {5, 2}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.runtime.snapshot.Connections.Connections = nil
			for i := 0; i < 40; i++ {
				c := fixtureConnection(fmt.Sprintf("id-%02d", i), long, 40-i)
				c.Metadata.ProcessPath = long
				m.runtime.snapshot.Connections.Connections = append(m.runtime.snapshot.Connections.Connections, c)
			}
			m.reconcileConnections()
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for i := 0; i < 25; i++ {
				m, _ = update(m, special(tea.KeyDown))
			}
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
			if size[0] >= 20 && size[1] >= 8 && !hasAction(m.targets, "connection."+m.connections.selected) {
				t.Fatal("keyboard-selected row is outside the visible viewport")
			}
			m, _ = update(m, special(tea.KeyEnter))
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
			m, _ = update(m, key("x"))
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
			m, _ = update(m, special(tea.KeyEscape))
			if m.connections.detail != nil || m.connections.confirming || m.busy {
				t.Fatal("Esc should dismiss details safely after resize")
			}
		})
	}
}

func TestConnectionsNavigationMatchesMenuPageShortcuts(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	for _, item := range []struct{ key, page string }{{"2", "proxies"}, {"3", "profiles"}, {"4", "connections"}, {"5", "rules"}, {"1", "overview"}, {"c", "connections"}} {
		m, _ = update(m, key(item.key))
		if m.page != item.page {
			t.Fatalf("%s opened %s instead of %s", item.key, m.page, item.page)
		}
	}
}

func TestLiveConnectionsRefreshKeepsFocusVisibleAndRepairsRemovedSelection(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m.width, m.height = 80, 24
	fresh := liveRuntime()
	for i := 0; i < 35; i++ {
		fresh.Connections.Connections = append(fresh.Connections.Connections, fixtureConnection(fmt.Sprintf("new-%02d", i), "new.example.invalid", 100+i))
	}
	fresh.Connections.Connections = append(fresh.Connections.Connections, fixtureConnection("first", "one.example.invalid", 1))
	m.runtime.pending = true
	m = acceptRuntimeFixture(m, fresh)
	m.layout()
	if m.focus != "connection.first" || !hasAction(m.targets, m.focus) {
		t.Fatal("live insertion moved the focused connection outside the viewport")
	}
	fresh = liveRuntime()
	fresh.Connections.Connections = []mihomo.Connection{fixtureConnection("second", "two.example.invalid", 0)}
	m.runtime.pending = true
	m = acceptRuntimeFixture(m, fresh)
	m.layout()
	if m.connections.selected != "second" || m.focus != "connection.second" || !hasAction(m.targets, m.focus) {
		t.Fatal("removed connection left focus pointing to a dead row")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.connections.detail == nil || m.connections.detail.ID != "second" {
		t.Fatal("repaired focus could not open the remaining connection")
	}
}
