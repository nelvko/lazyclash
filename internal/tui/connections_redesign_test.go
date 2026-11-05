package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func acceptConnectionsSample(m Model, snapshot app.RuntimeSnapshot) Model {
	m.runtime.active, m.runtime.pending = true, true
	m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, snapshot: snapshot})
	return m
}

func TestConnectionsPauseFreezesRowsRatesAndTotalsWhileMonitoringContinues(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	base := liveRuntime()
	base.ObservedAt = time.Unix(1_800_000_000, 0)
	base.Connections.Connections = []mihomo.Connection{fixtureConnection("first", "one.example.invalid", 1)}
	m = acceptConnectionsSample(m, base)
	second := base
	second.ObservedAt = base.ObservedAt.Add(time.Second)
	second.Connections = &mihomo.ConnectionSnapshot{UploadTotal: base.Connections.UploadTotal + 100, DownloadTotal: base.Connections.DownloadTotal + 500, Connections: append([]mihomo.Connection(nil), base.Connections.Connections...)}
	second.Connections.Connections[0].Upload += 100
	second.Connections.Connections[0].Download += 500
	m = acceptConnectionsSample(m, second)
	m, _ = update(m, key("p"))
	if !m.connections.paused || m.connections.frozen == nil || !m.runtime.active {
		t.Fatal("pause must freeze the connection view while leaving runtime monitoring active")
	}
	frozen := *m.connectionSnapshot()
	frozenRates := m.displayedConnectionRates().get("first")
	if !frozenRates.Valid {
		t.Fatal("fixture did not establish a valid rate before pausing")
	}
	fresh := liveRuntime()
	fresh.ObservedAt = second.ObservedAt.Add(time.Second)
	fresh.Connections = &mihomo.ConnectionSnapshot{UploadTotal: second.Connections.UploadTotal + 200, DownloadTotal: second.Connections.DownloadTotal + 900, Connections: []mihomo.Connection{fixtureConnection("new", "new.example.invalid", 20), second.Connections.Connections[0]}}
	fresh.Connections.Connections[1].Upload += 200
	fresh.Connections.Connections[1].Download += 900
	m = acceptConnectionsSample(m, fresh)
	if !reflect.DeepEqual(*m.connectionSnapshot(), frozen) || m.displayedConnectionRates().get("first") != frozenRates {
		t.Fatal("a live update changed paused rows, counters or rates")
	}
	if len(m.runtime.snapshot.Connections.Connections) != 2 || m.connections.rates.get("first").Download != 900 {
		t.Fatal("runtime observations stopped progressing behind the paused view")
	}
	m, _ = update(m, key("p"))
	if m.connections.paused || len(m.filteredConnections()) != 2 || m.connectionSnapshot().DownloadTotal != fresh.Connections.DownloadTotal {
		t.Fatal("resume did not immediately show the latest runtime observation")
	}
}

func TestConnectionsPausedDisappearingTargetCannotCloseAnotherConnection(t *testing.T) {
	closes := 0
	m := connectionsModel(t, &fakeBackend{closeConnection: func(context.Context, string) error { closes++; return nil }})
	m, _ = update(m, key("p"))
	m, _ = update(m, key("x"))
	if !m.connections.confirming || m.connections.detail == nil || m.connections.detail.ID != "first" {
		t.Fatal("paused active connection did not enter exact-ID confirmation")
	}
	fresh := liveRuntime()
	fresh.Connections.Connections = []mihomo.Connection{fixtureConnection("replacement", "replacement.example.invalid", 30)}
	m = acceptConnectionsSample(m, fresh)
	m, _ = update(m, key("y"))
	if m.busy || closes != 0 || m.connections.confirming || m.connections.detail.ID != "first" || m.detailActive() {
		t.Fatal("a paused disappearing connection was closed or replaced by another live row")
	}
	if !strings.Contains(m.failure, "no longer active") {
		t.Fatal("missing live target must explain why the close was not started")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if list := m.filteredConnections(); len(list) != 2 || list[0].ID != "first" {
		t.Fatal("returning from details discarded the explicitly paused observation")
	}
	m, _ = update(m, key("p"))
	if list := m.filteredConnections(); len(list) != 1 || list[0].ID != "replacement" || m.connections.selected != "replacement" {
		t.Fatal("resume did not repair the old frozen selection against live rows")
	}
}

func TestConnectionsUnavailableOrNewCoreDiscardsFrozenObservation(t *testing.T) {
	for _, transition := range []string{"unavailable", "new-core"} {
		t.Run(transition, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.runtime.snapshot.Service.PID = 10
			m, _ = update(m, key("p"))
			if transition == "unavailable" {
				m.runtime.pending = true
				m, _ = update(m, runtimeResultMsg{generation: m.runtime.generation, err: errors.New("fixture API unavailable")})
				if len(m.filteredConnections()) != 0 || m.connectionSnapshot() != nil {
					t.Fatal("unavailable runtime still advertised frozen rows as active")
				}
			}
			fresh := liveRuntime()
			fresh.Service.PID = 20
			fresh.Connections.Connections = []mihomo.Connection{fixtureConnection("new-core", "new-core.example.invalid", 2)}
			m = acceptConnectionsSample(m, fresh)
			for _, connection := range m.filteredConnections() {
				if connection.ID == "first" || connection.ID == "second" {
					t.Fatal("a new core identity resurrected an old paused observation")
				}
			}
			if m.connections.paused && m.connections.frozen != nil {
				t.Fatal("the old core's paused snapshot survived a core identity transition")
			}
		})
	}
}

func TestConnectionsPauseDetectsCounterResetAboveFrozenTotals(t *testing.T) {
	for _, counter := range []string{"upload", "download"} {
		t.Run(counter, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			base := liveRuntime()
			base.ObservedAt = time.Unix(1_800_000_000, 0)
			base.Service.PID = 10
			base.Connections = &mihomo.ConnectionSnapshot{UploadTotal: 1000, DownloadTotal: 2000, Connections: []mihomo.Connection{fixtureConnection("first", "one.example.invalid", 1)}}
			m = acceptConnectionsSample(m, base)
			m, _ = update(m, key("p"))
			advanced := base
			advanced.ObservedAt = base.ObservedAt.Add(time.Second)
			advanced.Connections = &mihomo.ConnectionSnapshot{UploadTotal: 4000, DownloadTotal: 6000, Connections: append([]mihomo.Connection(nil), base.Connections.Connections...)}
			advanced.Connections.Connections[0].Upload += 500
			advanced.Connections.Connections[0].Download += 1000
			m = acceptConnectionsSample(m, advanced)
			if !m.connections.paused || m.connectionSnapshot().UploadTotal != 1000 || m.connectionSnapshot().DownloadTotal != 2000 {
				t.Fatal("ordinary live counter growth discarded or changed the frozen observation")
			}
			reset := advanced
			reset.ObservedAt = advanced.ObservedAt.Add(time.Second)
			reset.Connections = &mihomo.ConnectionSnapshot{UploadTotal: 4500, DownloadTotal: 6500, Connections: append([]mihomo.Connection(nil), advanced.Connections.Connections...)}
			if counter == "upload" {
				reset.Connections.UploadTotal = 3000
			} else {
				reset.Connections.DownloadTotal = 5000
			}
			m = acceptConnectionsSample(m, reset)
			if m.connections.paused || m.connections.frozen != nil || m.connectionSnapshot().UploadTotal != reset.Connections.UploadTotal || m.connectionSnapshot().DownloadTotal != reset.Connections.DownloadTotal {
				t.Fatal("counter reset above the frozen baseline failed to resume the latest live observation")
			}
			if m.connections.rates.get("first").Valid {
				t.Fatal("a counter reset retained a rate derived across different counter lifetimes")
			}
		})
	}
}

func TestConnectionsSearchTabReachesToolbarWithoutLosingQuery(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m, _ = update(m, key("/"))
	m, _ = update(m, tea.PasteMsg{Content: "one"})
	m, _ = update(m, special(tea.KeyTab))
	if m.connections.editing || m.connections.search.Value() != "one" || m.focus != "conn.clear" {
		t.Fatalf("Tab from search must reach its adjacent Clear control, got %q", m.focus)
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "conn.pause" {
		t.Fatalf("toolbar pause is not next in reading order, got %q", m.focus)
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "conn.sort" {
		t.Fatalf("toolbar sort is unreachable by Tab, got %q", m.focus)
	}
	if m.connections.search.Value() != "one" || len(m.filteredConnections()) != 1 {
		t.Fatal("cycling toolbar focus discarded the applied search")
	}
}

func TestConnectionsSortKeepsSelectedIDAndNeverInvokesServiceActions(t *testing.T) {
	controls := 0
	m := connectionsModel(t, &fakeBackend{control: func(context.Context, string) error { controls++; return nil }})
	m.runtime.snapshot.Connections.Connections[1].Download = 100000
	selected := m.connections.selected
	for range int(connectionSortCount) {
		m, _ = update(m, key("s"))
		if m.connections.selected != selected || !hasAction(m.targets, "connection."+selected) || m.busy || controls != 0 {
			t.Fatal("sorting lost the selected connection or triggered a global service action")
		}
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.connections.detail == nil {
		t.Fatal("selected connection could not open after sorting")
	}
	m, _ = update(m, key("s"))
	if m.busy || controls != 0 || m.connections.detail.ID != selected {
		t.Fatal("sort key on details fell through to the global Start service action")
	}
}

func TestConnectionsResponsiveRowsFixedToolbarAndResizeSelection(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.locale = i18n.New(language)
			m.runtime.snapshot.Connections.Connections = nil
			for index := range 40 {
				connection := fixtureConnection(fmt.Sprintf("row-%02d", index), fmt.Sprintf("中文-%02d.example.invalid", index), 40-index)
				connection.Metadata.Process = "中文进程"
				connection.Chains = []string{"东京节点", "自动选择"}
				connection.Rule, connection.RulePayload = "RuleSet", "github_domain"
				m.runtime.snapshot.Connections.Connections = append(m.runtime.snapshot.Connections.Connections, connection)
			}
			m.connections.selected = "row-25"
			m.focus = "connection.row-25"
			m.reconcileConnections()
			for _, size := range [][2]int{{200, 45}, {160, 45}, {120, 32}, {80, 24}, {60, 20}, {40, 16}, {5, 2}, {160, 45}} {
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
				if m.connections.selected != "row-25" {
					// Very small terminals can temporarily park focus on the menu;
					// their selected connection must still retain its identity.
					t.Fatalf("resize to %v replaced the selected ID with %q", size, m.connections.selected)
				}
				if size[0] >= 40 && size[1] >= 16 && !hasAction(m.targets, "connection.row-25") {
					t.Fatalf("selected row left viewport after resize to %v", size)
				}
				g := dashboardGeometry(m.dashboardOptions())
				_, targets := m.connectionsBody(g.width)
				expectedHeight := 2
				if g.width >= 100 {
					expectedHeight = 1
				}
				for _, target := range targets {
					if target.ID == "connection.row-25" && target.H != expectedHeight {
						t.Fatalf("width %d row height = %d, want %d", g.width, target.H, expectedHeight)
					}
				}
				if m.dashboardOptions().BodyTopRows == 0 {
					continue
				}
				before := findTarget(t, m, "conn.search")
				m.scroll = 0
				m.layout()
				after := findTarget(t, m, "conn.search")
				if before != after {
					t.Fatalf("scroll moved the fixed search toolbar at %v: %v -> %v", size, before, after)
				}
				if !strings.Contains(ansi.Strip(m.screen), "中文") {
					t.Fatalf("responsive list lost visible CJK destination/process text at %v", size)
				}
			}
		})
	}
}

func TestConnectionsHomeEndAndPagingSelectVisibleIDsAcrossLayouts(t *testing.T) {
	for _, size := range [][2]int{{200, 45}, {160, 45}, {120, 32}, {80, 24}, {60, 20}, {40, 16}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.runtime.snapshot.Connections.Connections = nil
			for index := range 96 {
				m.runtime.snapshot.Connections.Connections = append(m.runtime.snapshot.Connections.Connections, fixtureConnection(fmt.Sprintf("nav-%02d", index), fmt.Sprintf("nav-%02d.example.invalid", index), 96-index))
			}
			m.connections.selected, m.focus = "nav-00", "connection.nav-00"
			m.reconcileConnections()
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			g := dashboardGeometry(m.dashboardOptions())
			rowHeight := 1
			if g.width < 100 {
				rowHeight = 2
			}
			page := max(1, g.height/rowHeight)
			for _, step := range []struct {
				key   rune
				index int
			}{{tea.KeyPgDown, page}, {tea.KeyEnd, 95}, {tea.KeyPgUp, 95 - page}, {tea.KeyHome, 0}, {tea.KeyPgUp, 0}, {tea.KeyEnd, 95}, {tea.KeyPgDown, 95}, {tea.KeyHome, 0}} {
				m, _ = update(m, special(step.key))
				id := fmt.Sprintf("nav-%02d", step.index)
				if m.connections.selected != id || m.connections.selectedIndex != step.index || m.focus != "connection."+id {
					t.Fatalf("%v at %v selected %q/index %d/focus %q, want %q", special(step.key).String(), size, m.connections.selected, m.connections.selectedIndex, m.focus, id)
				}
				hit := findTarget(t, m, "connection."+id)
				if hit.H != rowHeight || hit.Y < g.y || hit.Y+hit.H > g.y+g.height {
					t.Fatalf("%v left selected row partly outside its viewport at %v: %+v", special(step.key).String(), size, hit)
				}
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			}
		})
	}
}

func TestConnectionsReturnAndSearchRevealSelectionAfterLiveInsertions(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m, _ = update(m, special(tea.KeyEnter))
	fresh := liveRuntime()
	for index := range 40 {
		fresh.Connections.Connections = append(fresh.Connections.Connections, fixtureConnection(fmt.Sprintf("new-%02d", index), "new.example.invalid", 100+index))
	}
	fresh.Connections.Connections = append(fresh.Connections.Connections, fixtureConnection("first", "one.example.invalid", 1))
	m = acceptConnectionsSample(m, fresh)
	m, _ = update(m, special(tea.KeyEscape))
	if m.connections.selected != "first" || !hasAction(m.targets, "connection.first") {
		t.Fatal("returning from details hid the selected ID after live insertions")
	}
	m, _ = update(m, key("/"))
	m, _ = update(m, special(tea.KeyEnter))
	if m.focus != "connection.first" || !hasAction(m.targets, m.focus) {
		t.Fatal("finishing search failed to reveal its retained selection")
	}
}
