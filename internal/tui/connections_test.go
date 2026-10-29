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

func TestConnectionsSelectionAndConfirmationStayBoundToID(t *testing.T) {
	var closed []string
	b := &fakeBackend{closeConnection: func(_ context.Context, id string) error { closed = append(closed, id); return nil }}
	m := connectionsModel(t, b)
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, key("x"))
	if !m.connections.confirming || m.focus != "conn.cancel" || len(closed) != 0 {
		t.Fatal("close must require explicit confirmation, defaulting to Cancel")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.connections.confirming || len(closed) != 0 {
		t.Fatal("default Enter closed a connection")
	}
	m, _ = update(m, key("x"))
	// A newly arriving connection sorts ahead of the selected item. Closing must
	// still target the ID in the details, never its old row index.
	m.runtime.pending = true
	fresh := liveRuntime()
	fresh.Connections.Connections = []mihomo.Connection{fixtureConnection("newest", "new.example.invalid", 10), fixtureConnection("first", "one.example.invalid", 1), fixtureConnection("second", "two.example.invalid", 0)}
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: fresh})
	if m.connections.detail.ID != "first" || m.connections.selected != "first" || !m.connections.confirming {
		t.Fatal("refresh retargeted selection or confirmation")
	}
	m, cmd := update(m, key("y"))
	if !m.busy || m.connections.confirming {
		t.Fatal("explicit confirmation did not start one asynchronous close")
	}
	work := actionCmd(t, cmd)
	m, _ = update(m, key("y"))
	result := work()
	if len(closed) != 1 || closed[0] != "first" {
		t.Fatalf("wrong or duplicate connection closed: %v", closed)
	}
	m, _ = update(m, result)
	if m.busy || m.failure != "" || m.message != "Connection close requested" || !m.runtime.pending {
		t.Fatal("close did not refresh data and report its result")
	}
}

func TestConnectionsGoneDuringConfirmationNeverCloseAnotherRow(t *testing.T) {
	closed := 0
	m := connectionsModel(t, &fakeBackend{closeConnection: func(context.Context, string) error { closed++; return nil }})
	m, _ = update(m, key("x"))
	fresh := liveRuntime()
	fresh.Connections.Connections = []mihomo.Connection{fixtureConnection("second", "two.example.invalid", 0)}
	m.runtime.pending = true
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: fresh})
	m, _ = update(m, key("y"))
	if m.busy || closed != 0 || m.connections.confirming || m.connections.detail.ID != "first" || !strings.Contains(m.failure, "no longer active") {
		t.Fatal("disappearing target changed confirmation to another connection")
	}
}

func TestConnectionCloseFailurePreservesDetailsAndCanRetry(t *testing.T) {
	attempts := 0
	b := &fakeBackend{closeConnection: func(context.Context, string) error { attempts++; return errors.New("controller unavailable") }}
	m := connectionsModel(t, b)
	m, _ = update(m, key("x"))
	m, cmd := update(m, key("y"))
	m, _ = update(m, actionCmd(t, cmd)())
	if attempts != 1 || m.busy || m.connections.detail == nil || m.connections.detail.ID != "first" || !strings.Contains(m.failure, "controller unavailable") {
		t.Fatal("close error was hidden or lost its target")
	}
	fresh := liveRuntime()
	fresh.Connections.Connections = []mihomo.Connection{fixtureConnection("first", "one.example.invalid", 1)}
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: fresh})
	m, _ = update(m, key("x"))
	m, cmd = update(m, key("y"))
	_ = actionCmd(t, cmd)()
	if attempts != 2 {
		t.Fatal("close could not be retried after fresh runtime data")
	}
}

func TestConnectionsMouseConfirmationBlocksBackgroundNavigation(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m, _ = click(m, findTarget(t, m, "connection.first"))
	if m.connections.detail == nil || m.connections.detail.ID != "first" {
		t.Fatal("mouse did not open the clicked connection")
	}
	m, _ = click(m, findTarget(t, m, "conn.close"))
	m, _ = click(m, findTarget(t, m, "nav.settings"))
	if m.page != "connections" || !m.connections.confirming {
		t.Fatal("confirmation allowed background mouse navigation")
	}
	m, _ = click(m, findTarget(t, m, "conn.cancel"))
	if m.connections.confirming || m.busy {
		t.Fatal("mouse Cancel did not dismiss the confirmation")
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
			if m.connections.confirming {
				t.Fatal("tiny confirmation could not be canceled with Esc")
			}
		})
	}
}

func TestConnectionsNavigationPreservesExistingPageShortcuts(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	for _, item := range []struct{ key, page string }{{"2", "nodes"}, {"3", "profiles"}, {"4", "settings"}, {"5", "connections"}, {"1", "overview"}, {"c", "connections"}} {
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
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: fresh})
	if m.focus != "connection.first" || !hasAction(m.targets, m.focus) {
		t.Fatal("live insertion moved the focused connection outside the viewport")
	}
	fresh = liveRuntime()
	fresh.Connections.Connections = []mihomo.Connection{fixtureConnection("second", "two.example.invalid", 0)}
	m.runtime.pending = true
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: fresh})
	if m.connections.selected != "second" || m.focus != "connection.second" || !hasAction(m.targets, m.focus) {
		t.Fatal("removed connection left focus pointing to a dead row")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.connections.detail == nil || m.connections.detail.ID != "second" {
		t.Fatal("repaired focus could not open the remaining connection")
	}
}
