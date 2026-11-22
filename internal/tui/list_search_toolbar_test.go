package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func listSearchTarget(t *testing.T, targets []hitRegion, id string) hitRegion {
	t.Helper()
	for _, target := range targets {
		if target.ID == id {
			return target
		}
	}
	t.Fatalf("target %q absent: %+v", id, targets)
	return hitRegion{}
}

func TestListSearchToolbarsKeepScopeSearchCountAndFunctionOrder(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{160, 60, 40, 32} {
			for _, page := range []string{"connections", "logs"} {
				t.Run(fmt.Sprint(language, "/", page, "/", width), func(t *testing.T) {
					var bar dashboardBlock
					var searchID, scopeID, count string
					if page == "connections" {
						m := connectionsModel(t, &fakeBackend{})
						m.locale = i18n.New(language)
						m.connections.search.SetValue("one")
						bar = m.connectionFilterBar(width)
						searchID, scopeID, count = "conn.search", "conn.closed", m.connectionCount()
					} else {
						m := redesignedLogsModel(width, 24, 12)
						defer m.stopRuntime()
						m.locale = i18n.New(language)
						m.logs.search.SetValue("event-001")
						bar = m.logFilterBar(width)
						searchID, scopeID = "logs.search", "logs.level"
						count, _ = m.logCount()
					}
					search := listSearchTarget(t, bar.targets, searchID)
					scope := listSearchTarget(t, bar.targets, scopeID)
					countX, countY := -1, -1
					for y, line := range bar.lines {
						plain := ansi.Strip(line)
						if index := strings.Index(plain, count); index >= 0 {
							countX, countY = ansi.StringWidth(plain[:index]), y
						}
						if ansi.StringWidth(line) > width {
							t.Fatalf("toolbar exceeded %d cells: %q", width, plain)
						}
					}
					if countX < 0 {
						t.Fatalf("filtered count %q missing from toolbar: %q", count, bar.lines)
					}
					inline := scope.Y == search.Y+1
					if width == 160 && !inline || width <= 40 && inline {
						t.Fatalf("unexpected scope placement at width %d: scope=%+v search=%+v", width, scope, search)
					}
					if inline {
						if scope.X+scope.W > search.X || countY != search.Y+1 || countX < search.X+search.W+2 {
							t.Fatalf("expected scope, input and count in reading order: scope=%+v search=%+v count=%d,%d", scope, search, countX, countY)
						}
					} else if scope.Y+scope.H > search.Y || countY != search.Y+1 && countY < search.Y+search.H {
						t.Fatalf("stacked scope or count overlaps search frame: scope=%+v search=%+v count=%d,%d", scope, search, countX, countY)
					}
					if page == "connections" {
						sort := listSearchTarget(t, bar.targets, "conn.sort")
						if width == 160 {
							if sort.Y != search.Y+1 || sort.X != search.X+search.W+2 || countX < sort.X+sort.W+1 {
								t.Fatalf("sort must follow search and precede count: sort=%+v search=%+v count=%d", sort, search, countX)
							}
						} else if sort.Y+sort.H > search.Y || sort.Y == scope.Y && sort.X != scope.X+scope.W+2 {
							t.Fatalf("stacked sort must sit with scope above search: sort=%+v scope=%+v search=%+v", sort, scope, search)
						}
					} else if hasAction(bar.targets, "logs.view") {
						t.Fatal("log toolbar still exposes a view toggle")
					}
					assertRegions(t, bar.targets, width, len(bar.lines))
				})
			}
		}
	}
}

func TestListSearchLogsKeepGeometryAcrossLabelsFiltersAndCacheGrowth(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{160, 80, 60, 40, 32} {
			t.Run(fmt.Sprint(language, "/", width), func(t *testing.T) {
				m := redesignedLogsModel(width, 24, 12)
				defer m.stopRuntime()
				m.locale = i18n.New(language)
				baseline := m.logFilterBar(width)
				ctx, generation := m.runtime.ctx, m.runtime.generation
				for _, total := range []int{12, 99, 100, retainedLogLimit} {
					for len(m.logs.entries) < total {
						m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "event incoming"}})
					}
					for level := range logLevelKeys {
						for _, query := range []string{"", "event", "  "} {
							m.logs.level = level
							m.logs.search.SetValue(query)
							bar := m.logFilterBar(width)
							for _, id := range []string{"logs.level", "logs.search"} {
								got, want := listSearchTarget(t, bar.targets, id), listSearchTarget(t, baseline.targets, id)
								if got != want {
									t.Fatalf("total=%d level=%d query=%q moved %s: got %+v; want %+v", total, level, query, id, got, want)
								}
							}
							levelTarget := listSearchTarget(t, bar.targets, "logs.level")
							if !strings.Contains(ansi.Strip(bar.lines[levelTarget.Y]), m.tr(logLevelKeys[level])+" ▾") {
								t.Fatal("reserved level width clipped the selected label")
							}
							if level == 0 && strings.TrimSpace(query) == "" && len(bar.lines) != len(baseline.lines) {
								t.Fatal("hidden count retained an extra toolbar row")
							}
							assertRegions(t, bar.targets, width, len(bar.lines))
						}
					}
				}
				if ctx.Err() != nil || m.runtime.generation != generation || !m.logs.active {
					t.Fatal("local toolbar changes interrupted capture")
				}
			})
		}
	}
}

func TestListSearchConnectionsTabAndShiftTabFollowTheInputTextRow(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{160, 45}, {60, 20}, {40, 16}, {40, 9}} {
			t.Run(fmt.Sprint(language, "/", size), func(t *testing.T) {
				m := connectionsModel(t, &fakeBackend{})
				m.locale = i18n.New(language)
				m.connections.search.SetValue("one")
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				want := []string{"conn.active", "conn.closed", "conn.search", "conn.clear", "conn.case", "conn.word", "conn.regex", "conn.sort", "connection.first"}
				if size[0] != 160 {
					want = []string{"conn.active", "conn.closed", "conn.sort", "conn.search", "conn.clear", "conn.case", "conn.word", "conn.regex", "connection.first"}
				}
				var got []string
				for _, target := range m.focusTargets() {
					switch target.ID {
					case "conn.active", "conn.closed", "conn.search", "conn.clear", "conn.case", "conn.word", "conn.regex", "conn.sort", "connection.first":
						got = append(got, target.ID)
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("Tab targets must follow the input text row: got %v; want %v", got, want)
				}
				m.focus = want[0]
				m.layout()
				for _, id := range want[1:] {
					m, _ = update(m, special(tea.KeyTab))
					if m.focus != id {
						t.Fatalf("Tab should visit %s; got %s", id, m.focus)
					}
				}
				for i := len(want) - 2; i >= 0; i-- {
					m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
					if m.focus != want[i] {
						t.Fatalf("Shift+Tab should visit %s; got %s", want[i], m.focus)
					}
				}
				if m.connections.search.Value() != "one" || len(m.filteredConnections()) != 1 {
					t.Fatal("keyboard traversal changed the search or its results")
				}
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			})
		}
	}
}

func logToolbarTabIDs(m Model) []string {
	var ids []string
	for _, target := range m.focusTargets() {
		switch target.ID {
		case "logs.level", "logs.search", "logs.reset-search", "logs.case", "logs.word", "logs.regex", "logs.list":
			ids = append(ids, target.ID)
		}
	}
	return ids
}

func TestListSearchLogsTabAndShiftTabFollowTheInputTextRow(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{160, 45}, {60, 20}, {40, 16}, {40, 9}} {
			t.Run(fmt.Sprint(language, "/", size), func(t *testing.T) {
				m := redesignedLogsModel(size[0], size[1], 12)
				defer m.stopRuntime()
				m.locale = i18n.New(language)
				m.logs.search.SetValue("event")
				m.layout()
				want := []string{"logs.level", "logs.search", "logs.reset-search", "logs.case", "logs.word", "logs.regex", "logs.list"}
				if got := logToolbarTabIDs(m); !reflect.DeepEqual(got, want) {
					t.Fatalf("Tab targets must follow the text row, including offscreen controls: got %v; want %v", got, want)
				}
				m.focus = want[0]
				m.layout()
				for _, id := range want[1:] {
					m, _ = update(m, special(tea.KeyTab))
					if m.focus != id {
						t.Fatalf("Tab should visit %s; got %s", id, m.focus)
					}
				}
				for i := len(want) - 2; i >= 0; i-- {
					m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
					if m.focus != want[i] {
						t.Fatalf("Shift+Tab should visit %s; got %s", want[i], m.focus)
					}
				}
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			})
		}
	}
}
