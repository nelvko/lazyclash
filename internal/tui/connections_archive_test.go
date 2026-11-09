package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func archiveFixture(t *testing.T, b *fakeBackend) (Model, mihomo.Connection) {
	t.Helper()
	m := connectionsModel(t, b)
	m.notification.items = nil
	base := liveRuntime()
	base.ObservedAt = time.Now().Add(-3 * time.Second)
	base.Service.PID = 101
	first, second := fixtureConnection("first", "one.example.invalid", 1), fixtureConnection("second", "two.example.invalid", 0)
	base.Connections.Connections = []mihomo.Connection{first, second}
	m.runtime.snapshot = base
	m.observeConnections(base.ObservedAt)
	next := base
	next.ObservedAt = base.ObservedAt.Add(time.Second)
	next.Connections = &mihomo.ConnectionSnapshot{UploadTotal: base.Connections.UploadTotal, DownloadTotal: base.Connections.DownloadTotal, Connections: []mihomo.Connection{second}}
	m.runtime.snapshot = next
	m.observeConnections(next.ObservedAt)
	m.layout()
	return m, first
}

func archiveObserve(m *Model, connections []mihomo.Connection, at time.Time) {
	snapshot := m.runtime.snapshot
	snapshot.ObservedAt = at
	snapshot.Connections = &mihomo.ConnectionSnapshot{UploadTotal: snapshot.Connections.UploadTotal, DownloadTotal: snapshot.Connections.DownloadTotal, Connections: connections}
	m.runtime.snapshot = snapshot
	m.observeConnections(at)
	m.layout()
}

func TestConnectionsArchiveTabsSearchAndKeyboardPreserveQuery(t *testing.T) {
	m, first := archiveFixture(t, &fakeBackend{})
	m.connections.search.SetValue("ONE.EXAMPLE.INVALID")
	m.reconcileConnections()
	m.layout()
	if len(m.filteredConnections()) != 0 {
		t.Fatal("active search unexpectedly matched an archived connection")
	}
	m, _ = click(m, findTarget(t, m, "conn.closed"))
	if !m.connections.closedView || len(m.filteredConnections()) != 1 || m.filteredConnections()[0].ID != first.ID || m.connections.search.Value() != "ONE.EXAMPLE.INVALID" {
		t.Fatal("closed tab did not search archived metadata while retaining the query")
	}
	if m.contextHelp() != m.tr("navigation.connections_closed") {
		t.Fatal("closed view still advertises actions for live connections")
	}
	findTarget(t, m, "connection."+m.connectionRowKey(first))
	m, _ = update(m, key("v"))
	if m.connections.closedView || m.connections.search.Value() != "ONE.EXAMPLE.INVALID" || len(m.filteredConnections()) != 0 {
		t.Fatal("view toggle did not return to the filtered active list")
	}
	m, _ = update(m, key("v"))
	if !m.connections.closedView || len(m.filteredConnections()) != 1 {
		t.Fatal("view toggle could not return to the archive")
	}
}

func TestConnectionsArchiveDetailAlwaysReadonlyWhenIDReappears(t *testing.T) {
	closes := 0
	m, first := archiveFixture(t, &fakeBackend{closeConnection: func(context.Context, string) error { closes++; return nil }})
	m.connectionActivate("conn.closed")
	m.layout()
	rowKey := m.connectionRowKey(first)
	m, _ = click(m, findTarget(t, m, "connection."+rowKey))
	if !m.connections.detailHistorical || m.detailActive() || hasAction(m.targets, "conn.close") {
		t.Fatal("historical details exposed a live close action")
	}
	closedAt := m.connections.detailClosedAt
	body, _ := m.connectionsBody(200)
	if closedAt.IsZero() || !strings.Contains(ansi.Strip(body), m.tr("connections.archived_hint")) {
		t.Fatal("historical details lost their observed close time or last-sample explanation")
	}
	replacement := first
	replacement.Start = first.Start.Add(time.Hour)
	replacement.Metadata.Host = "replacement.example.invalid"
	archiveObserve(&m, []mihomo.Connection{replacement}, time.Now().Add(-time.Second))
	if !m.connections.detailHistorical || !m.connections.detail.Start.Equal(first.Start) || m.connections.detail.Metadata.Host != first.Metadata.Host || !m.connections.detailClosedAt.Equal(closedAt) || m.detailActive() {
		t.Fatal("a reused live ID replaced the historical detail or re-enabled its close action")
	}
	m, _ = update(m, key("x"))
	m.connectionActivate("conn.close")
	if m.busy || m.connections.confirming || closes != 0 || hasAction(m.targets, "conn.close") {
		t.Fatal("historical details allowed a close operation")
	}
	m, _ = update(m, key("v"))
	if !m.connections.closedView || m.connections.detail == nil {
		t.Fatal("list-only view shortcut unexpectedly switched an open detail")
	}
}

func TestConnectionsArchiveEndedActiveDetailStaysReadonly(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	at := time.Now().Add(-3 * time.Second)
	m.runtime.snapshot.ObservedAt = at
	m.observeConnections(at)
	rows := append([]mihomo.Connection(nil), m.runtime.snapshot.Connections.Connections...)
	m.connectionActivate("connection.first")
	archiveObserve(&m, rows[1:], at.Add(time.Second))
	if !m.connections.detailHistorical || m.detailActive() {
		t.Fatal("ending a viewed active connection did not make its details read only")
	}
	archiveObserve(&m, rows, at.Add(2*time.Second))
	if m.detailActive() || hasAction(m.targets, "conn.close") {
		t.Fatal("a repeated lifetime made an already archived detail actionable")
	}
}

func TestConnectionsArchiveRowsCannotCloseAndUseDistinctKeys(t *testing.T) {
	closes := 0
	m, first := archiveFixture(t, &fakeBackend{closeConnection: func(context.Context, string) error { closes++; return nil }})
	m.connectionActivate("conn.closed")
	m.layout()
	if got := m.connectionRowKey(first); got != "closed."+first.ID+"."+first.Start.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("historical row key lost its connection lifetime: %q", got)
	}
	m, _ = update(m, key("x"))
	if m.busy || m.connections.confirming || closes != 0 {
		t.Fatal("archive list close shortcut reached the core")
	}
}

func TestConnectionsArchivePauseFreezesBothViewsWhileHistoryContinues(t *testing.T) {
	m, first := archiveFixture(t, &fakeBackend{})
	m, _ = update(m, key("p"))
	archiveObserve(&m, nil, time.Now().Add(-time.Second))
	if len(m.connections.history.entries()) != 2 || len(m.connectionClosedEntries()) != 1 {
		t.Fatal("pause stopped collecting history or exposed new archived rows")
	}
	m.connectionActivate("conn.closed")
	m.layout()
	if list := m.filteredConnections(); len(list) != 1 || list[0].ID != first.ID {
		t.Fatal("switching tabs discarded the explicitly frozen history")
	}
	m, _ = update(m, key("p"))
	if m.connections.paused || len(m.connectionClosedEntries()) != 2 || len(m.filteredConnections()) != 2 {
		t.Fatal("resume did not reveal history collected during pause")
	}
}

func TestConnectionsArchiveRemainsSearchableWhenCoreUnavailable(t *testing.T) {
	m, first := archiveFixture(t, &fakeBackend{})
	m.runtime.snapshot = app.RuntimeSnapshot{State: "unavailable", Service: m.runtime.snapshot.Service, RunningVersion: m.runtime.snapshot.RunningVersion, ObservedAt: time.Now().Add(-time.Second)}
	m.observeConnections(m.runtime.snapshot.ObservedAt)
	m.connectionActivate("conn.closed")
	m.connections.search.SetValue("one.example.invalid")
	m.reconcileConnections()
	m.layout()
	if list := m.filteredConnections(); len(list) != 1 || list[0].ID != first.ID {
		t.Fatal("temporary unavailability erased or hid known historical connections")
	}
	m, _ = click(m, findTarget(t, m, "connection."+m.connectionRowKey(first)))
	if !m.connections.detailHistorical || m.detailActive() || hasAction(m.targets, "conn.close") {
		t.Fatal("offline historical detail became actionable")
	}
}

func TestConnectionsArchiveCoreChangeInvalidatesPendingSingleClose(t *testing.T) {
	closes := 0
	m, _ := archiveFixture(t, &fakeBackend{closeConnection: func(context.Context, string) error { closes++; return nil }})
	m, _ = update(m, key("x"))
	if !m.connections.confirming {
		t.Fatal("fixture did not open the active connection close confirmation")
	}
	m.runtime.snapshot.Service.PID = 202
	archiveObserve(&m, m.runtime.snapshot.Connections.Connections, time.Now().Add(-time.Second))
	m, _ = update(m, key("y"))
	if m.busy || closes != 0 || m.detailActive() {
		t.Fatal("confirmation for a previous core closed a connection in the replacement core")
	}
}

func TestConnectionsArchiveTabsLayoutAndSelectionAcrossSizes(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m, first := archiveFixture(t, &fakeBackend{})
			m.locale = i18n.New(language)
			m.connectionActivate("conn.closed")
			for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {40, 16}, {20, 7}, {5, 2}, {160, 45}} {
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
				if m.connections.selected != m.connectionRowKey(first) {
					t.Fatalf("resize %v replaced the selected archived lifetime: %q", size, m.connections.selected)
				}
				if size[0] < 40 || size[1] < 16 {
					continue
				}
				findTarget(t, m, "connection."+m.connectionRowKey(first))
				active := findTarget(t, m, "conn.active")
				closed := findTarget(t, m, "conn.closed")
				search := findTarget(t, m, "conn.search")
				if size[0] == 160 && (active.X >= closed.X || closed.X+closed.W > search.X) {
					t.Fatalf("tabs do not precede search horizontally: %v %v %v", active, closed, search)
				}
				bar := m.connectionsToolbar(dashboardBodyWidth(m.dashboardOptions()), m.connectionToolbarRows(dashboardBodyWidth(m.dashboardOptions())))
				if size[0] >= 120 && !strings.Contains(ansi.Strip(strings.Join(bar.lines, "\n")), m.tr("connections.history_scope")) {
					t.Fatal("archive toolbar omits the session history retention explanation")
				}
			}
		})
	}
}

func TestConnectionsArchiveTabCountsIgnoreQueryAndRepresentUnavailableLiveData(t *testing.T) {
	m, _ := archiveFixture(t, &fakeBackend{})
	m.connections.search.SetValue("no-match.example.invalid")
	m.reconcileConnections()
	for _, unavailable := range []bool{false, true} {
		if unavailable {
			m.runtime.snapshot = app.RuntimeSnapshot{State: "unavailable", Service: m.runtime.snapshot.Service, RunningVersion: m.runtime.snapshot.RunningVersion, ObservedAt: time.Now().Add(-time.Second)}
			m.observeConnections(m.runtime.snapshot.ObservedAt)
		}
		active := "1"
		if unavailable {
			active = "--"
		}
		bar := m.connectionsToolbar(160, m.connectionToolbarRows(160))
		plain := ansi.Strip(strings.Join(bar.lines, "\n"))
		if !strings.Contains(plain, m.tr("connections.tab_active", active)) || !strings.Contains(plain, m.tr("connections.tab_closed", "1")) {
			t.Fatalf("tabs used filtered counts or confused unavailable data with zero: %q", plain)
		}
	}
}

func TestConnectionsArchiveViewShortcutRemainsTextInsideSearch(t *testing.T) {
	m, _ := archiveFixture(t, &fakeBackend{})
	m, _ = update(m, key("/"))
	m, _ = update(m, key("v"))
	if m.connections.closedView || !m.connections.editing || m.connections.search.Value() != "v" {
		t.Fatal("view shortcut intercepted ordinary search text")
	}
	m, _ = update(m, special(tea.KeyEscape))
	m.connections.search.Reset()
	m.reconcileConnections()
	m.layout()
	seen := map[string]bool{}
	for range len(m.focusTargets()) + 2 {
		m, _ = update(m, special(tea.KeyTab))
		seen[m.focus] = true
	}
	if !seen["conn.active"] || !seen["conn.closed"] {
		t.Fatal("view tabs are missing from keyboard focus navigation")
	}
}
