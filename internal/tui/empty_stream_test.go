package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func emptyStreamModel(t *testing.T, language, page string) Model {
	t.Helper()
	m := runtimeModel(t, &fakeBackend{})
	m.locale, m.page = i18n.New(language), page
	m.runtime.received, m.runtime.snapshot = true, liveRuntime()
	m.logs = newLogsState(m.locale)
	m.logs.active = true
	return m
}

func TestConnectionsEmptyStatesDistinguishSourceFailuresFromFilters(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, test := range []struct {
			name, title, action string
			prepare             func(*Model)
		}{
			{"empty", "connections.empty", "", func(*Model) {}},
			{"filter", "connections.no_matches", "conn.reset-filter", func(m *Model) { m.connections.search.SetValue("missing") }},
			{"stopped", "runtime.status.stopped", "core.start", func(m *Model) {
				m.snapshot.Service.State, m.snapshot.Service.Active = "inactive", false
				m.runtime.snapshot.State, m.runtime.snapshot.Connections = "stopped", nil
				m.connections.search.SetValue("missing")
			}},
			{"read-failure", "empty.read_failed", "conn.retry", func(m *Model) {
				m.runtime.snapshot.Connections, m.runtime.snapshot.ConnectionsError = nil, "connection source unavailable"
				m.connections.search.SetValue("missing")
			}},
			{"loading", "empty.loading", "", func(m *Model) {
				m.runtime.received = false
				m.connections.search.SetValue("missing")
			}},
			{"closed-history", "connections.closed_empty", "", func(m *Model) {
				m.connections.closedView = true
				m.snapshot.Service.State, m.snapshot.Service.Active = "inactive", false
			}},
		} {
			t.Run(language+"/"+test.name, func(t *testing.T) {
				m := emptyStreamModel(t, language, "connections")
				test.prepare(&m)
				state := m.connectionsEmptyState()
				if state.Title != m.tr(test.title) || state.ActionID != test.action {
					t.Fatalf("wrong state: %+v", state)
				}
				for _, size := range [][2]int{{80, 24}, {60, 18}} {
					m.width, m.height = size[0], size[1]
					body, targets := m.connectionsBody(dashboardBodyWidth(m.dashboardOptions()))
					assertStreamEmptyCentered(t, body, dashboardBodyWidth(m.dashboardOptions()), 0)
					if test.action != "" && !hasAction(targets, test.action) {
						t.Fatalf("recovery action %s disappeared at %v", test.action, size)
					}
				}
			})
		}
	}
}

func TestLogsEmptyStatesKeepFailureAndPauseMeaning(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, test := range []struct {
			name, title, action string
			prepare             func(*Model)
		}{
			{"empty", "empty.logs.none", "", func(*Model) {}},
			{"filter", "logs.no_matches", "logs.reset-filters", func(m *Model) { m.logs.level = 2 }},
			{"stopped", "runtime.status.stopped", "", func(m *Model) {
				m.snapshot.Service.State, m.snapshot.Service.Active = "inactive", false
				m.logs.err, m.logs.reconnecting = errors.New("old stream failure"), true
				m.logs.search.SetValue("missing")
			}},
			{"read-failure", "empty.read_failed", "", func(m *Model) {
				m.logs.err, m.logs.reconnecting = errors.New("log source unavailable"), true
				m.logs.search.SetValue("missing")
			}},
			{"opening", "logs.opening", "", func(m *Model) { m.logs.active = false }},
			{"paused-empty", "logs.display_paused", "", func(m *Model) {
				m.pauseLogs()
				m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "arrived while paused"}})
			}},
		} {
			t.Run(language+"/"+test.name, func(t *testing.T) {
				m := emptyStreamModel(t, language, "logs")
				test.prepare(&m)
				state := m.logsEmptyState()
				if state.Title != m.tr(test.title) || state.ActionID != test.action {
					t.Fatalf("wrong state: %+v", state)
				}
				for _, size := range [][2]int{{80, 24}, {60, 18}} {
					m.width, m.height = size[0], size[1]
					body, targets := m.logsBody(dashboardBodyWidth(m.dashboardOptions()))
					footerRows := 0
					if status, _ := m.logsStatus(); status != "" {
						footerRows = 1
					}
					assertStreamEmptyCentered(t, body, dashboardBodyWidth(m.dashboardOptions()), footerRows)
					if test.action != "" && !hasAction(targets, test.action) {
						t.Fatalf("recovery action %s disappeared at %v", test.action, size)
					}
				}
			})
		}
	}
}

func assertStreamEmptyCentered(t *testing.T, body string, width, suffix int) {
	t.Helper()
	lines := strings.Split(ansi.Strip(body), "\n")
	lines = lines[:len(lines)-suffix]
	first, last := -1, -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
		left := len(line) - len(strings.TrimLeft(line, " "))
		right := width - left - ansi.StringWidth(strings.TrimSpace(line))
		if difference := left - right; i == first && (difference < -1 || difference > 1) {
			t.Fatalf("empty row is not centered: %q", line)
		}
	}
	difference := first - (len(lines) - last - 1)
	if first < 0 || difference < -1 || difference > 1 {
		t.Fatalf("empty block is not vertically centered:\n%s", body)
	}
}

func TestEmptyStreamRecoveryControlsPerformTheirAdvertisedActions(t *testing.T) {
	m := emptyStreamModel(t, i18n.English, "logs")
	m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "retained"}})
	m.logs.search.SetValue("missing")
	m.logs.level = 3
	m.logsActivate("logs.reset-filters")
	if m.logs.search.Value() != "" || m.logs.level != 0 || m.focus != "logs.list" || len(m.logDocument(80).lines) == 0 {
		t.Fatal("clear filters did not restore retained log entries")
	}
	m.focus = "logs.reset-filters"
	if !strings.HasPrefix(m.logsContextHelp(), m.tr("empty.logs.reset_hint")) {
		t.Fatal("clear filters advertised a different action")
	}
	m = emptyStreamModel(t, i18n.English, "connections")
	m.runtime.active, m.runtime.pending = false, false
	m.runtime.snapshot.Connections, m.runtime.snapshot.ConnectionsError = nil, "connection source unavailable"
	m.focus = "conn.retry"
	if m.activate("conn.retry") == nil || !m.runtime.active || !strings.HasPrefix(m.connectionsContextHelp(), m.tr("empty.connections.retry_hint")) {
		t.Fatal("connection retry did not restart observation with a matching hint")
	}
	m.stopRuntime()
	m.busy = true
	if m.connectionActivate("conn.retry") != nil || m.runtime.active {
		t.Fatal("busy connection retry started another observation")
	}
	m.busy = false
	m.connections.search.SetValue("missing")
	m.connectionActivate("conn.reset-filter")
	if m.connections.search.Value() != "" || !m.connections.editing {
		t.Fatal("clear connection search did not restore the list input")
	}
}

func TestEmptyStreamControlsRemainUniqueAndResetScrollWhenDataReturns(t *testing.T) {
	for _, page := range []string{"connections", "logs"} {
		for _, size := range [][2]int{{80, 24}, {60, 18}} {
			for _, condition := range []string{"empty", "filter", "failure", "service-unavailable", "initial-failure", "stopped", "paused"} {
				t.Run(page+"/"+condition, func(t *testing.T) {
					m := emptyStreamModel(t, i18n.English, page)
					m.width, m.height = size[0], size[1]
					switch condition {
					case "filter":
						m.logs.search.SetValue("missing")
						m.connections.search.SetValue("missing")
					case "failure":
						m.logs.err = errors.New("log source unavailable")
						m.runtime.snapshot.Connections, m.runtime.snapshot.ConnectionsError = nil, "connection source unavailable"
					case "service-unavailable":
						m.snapshot.ServiceError = "service state unreadable"
					case "initial-failure":
						m.snapshotReady, m.snapshot.Installed = false, false
						m.failure = "initial state unreadable"
					case "stopped":
						m.snapshot.Service.State, m.snapshot.Service.Active = "inactive", false
					case "paused":
						m.pauseLogs()
					}
					m.layout()
					seen := map[string]bool{}
					for _, target := range m.focusTargets() {
						if seen[target.ID] {
							t.Fatalf("duplicate keyboard stop %s at %v", target.ID, size)
						}
						seen[target.ID] = true
					}
				})
			}
		}
		m := emptyStreamModel(t, i18n.English, page)
		m.width, m.height, m.scroll = 80, 24, 200
		m.layout()
		if m.scroll != 0 {
			t.Fatal("empty viewport retained an old list scroll offset")
		}
		m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "new log record"}})
		m.runtime.snapshot.Connections.Connections = []mihomo.Connection{fixtureConnection("new", "new.example.invalid", 0)}
		m.reconcileConnections()
		m.layout()
		want := "new log record"
		if page == "connections" {
			want = "new.example.invalid"
		}
		if !strings.Contains(ansi.Strip(m.screen), want) {
			t.Fatalf("new data remained outside the empty viewport:\n%s", ansi.Strip(m.screen))
		}
	}
}

func TestTinyConnectionsCanPageFromCompleteToolbarToEmptyReasonAndAction(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{60, 9}, {40, 9}} {
			for _, condition := range []string{"filter", "failure", "initial-failure", "stopped"} {
				t.Run(language+"/"+condition, func(t *testing.T) {
					m := emptyStreamModel(t, language, "connections")
					m.width, m.height, m.focus = size[0], size[1], "content"
					switch condition {
					case "filter":
						m.connections.search.SetValue("missing")
					case "failure":
						m.runtime.snapshot.Connections, m.runtime.snapshot.ConnectionsError = nil, "source unreadable"
					case "initial-failure":
						m.snapshotReady, m.snapshot.Installed = false, false
						m.failure = "initial state unreadable"
					case "stopped":
						m.snapshot.Service.Active, m.snapshot.Service.State = false, "inactive"
					}
					if m.snapshotReady {
						// A source failure occurs inside the existing monitor, not
						// during the first pending subscription.
						m.runtime.active, m.runtime.pending = true, false
					} else if status, _ := m.connectionStatus(); status != m.tr("empty.read_failed") {
						t.Fatal("initial failure was described as an uninstalled core")
					}
					m.layout()
					o := m.dashboardOptions()
					if o.BodyTopRows != 0 {
						t.Fatal("test did not exercise the inline toolbar")
					}
					g := dashboardGeometry(o)
					bar := m.connectionsToolbar(g.width, m.connectionToolbarRows(g.width))
					body, _ := m.connectionsBody(g.width)
					if !strings.HasPrefix(body, strings.Join(bar.lines, "\n")) {
						t.Fatal("making room for an empty state truncated the toolbar")
					}
					state := m.connectionsEmptyState()
					if state.ActionID == "" {
						t.Fatal("test did not exercise an empty state with a recovery action")
					}
					m, _ = update(m, special(tea.KeyPgDown))
					if m.scroll == 0 {
						t.Fatal("empty list swallowed PgDn instead of scrolling its document")
					}
					m, _ = update(m, special(tea.KeyEnd))
					if plain := ansi.Strip(m.screen); !strings.Contains(plain, state.Title) || !hasAction(m.targets, state.ActionID) {
						t.Fatalf("empty reason or action was unreachable at %v:\n%s", size, plain)
					}
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
					m, _ = update(m, special(tea.KeyHome))
					if m.scroll != 0 {
						t.Fatal("Home did not return to the complete toolbar")
					}
				})
			}
		}
	}
}
