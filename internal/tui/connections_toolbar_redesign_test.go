package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func connectionToolbarRedesignTarget(t *testing.T, bar dashboardBlock, id string) hitRegion {
	t.Helper()
	for _, target := range bar.targets {
		if target.ID == id {
			return target
		}
	}
	t.Fatalf("toolbar target %q absent: %+v", id, bar.targets)
	return hitRegion{}
}

func TestConnectionsToolbarRedesignHeaderPauseRemainsReachableInBothViews(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, closed := range []bool{false, true} {
			for _, size := range [][2]int{{160, 45}, {60, 20}, {40, 16}} {
				t.Run(fmt.Sprintf("%s/closed=%t/%dx%d", language, closed, size[0], size[1]), func(t *testing.T) {
					calls := 0
					m, _ := archiveFixture(t, &fakeBackend{
						control:             func(context.Context, string) error { calls++; return nil },
						closeAllConnections: func(context.Context) error { calls++; return nil },
					})
					m.locale = i18n.New(language)
					if closed {
						m.connectionActivate("conn.closed")
					}
					m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					if !m.pageHeader().enabled("conn.pause") {
						t.Fatal("Pause must be an enabled page-header action in either view")
					}
					pause, closeAll := findTarget(t, m, "conn.pause"), findTarget(t, m, "conn.close-all")
					search := findTarget(t, m, "conn.search")
					if pause.Y != closeAll.Y || pause.X+pause.W > closeAll.X || pause.Y >= search.Y {
						t.Fatalf("Pause must precede Close all above the input: pause=%+v close=%+v search=%+v", pause, closeAll, search)
					}
					m, _ = click(m, pause)
					if !m.connections.paused || m.connections.closedView != closed {
						t.Fatal("header click did not freeze the current view")
					}
					resumeLabel := false
					for _, action := range m.pageHeader().actions {
						resumeLabel = resumeLabel || action.id == "conn.pause" && action.label == m.tr("connections.resume")
					}
					if !resumeLabel {
						t.Fatal("paused header did not offer Resume")
					}
					m, _ = click(m, findTarget(t, m, "conn.pause"))
					if m.connections.paused {
						t.Fatal("Resume header click did not restore live display")
					}
					m, _ = update(m, key("p"))
					m, _ = update(m, key("p"))
					if m.connections.paused || calls != 0 || m.busy {
						t.Fatal("pause/resume shortcut changed service or connection state")
					}
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
				})
			}
		}
	}
}

func TestConnectionsToolbarRedesignWideInputCountAndSortShareTextRow(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.locale = i18n.New(language)
			var baseline hitRegion
			for order := connectionSortNewest; order < connectionSortCount; order++ {
				m.connections.sort = order
				for _, query := range []struct{ value, count string }{{"", "2"}, {"one", "1 / 2"}, {"no-match", "0 / 2"}, {"  ", "2"}} {
					m.connections.search.SetValue(query.value)
					bar := m.connectionFilterBar(160)
					search := connectionToolbarRedesignTarget(t, bar, "conn.search")
					sort := connectionToolbarRedesignTarget(t, bar, "conn.sort")
					active := connectionToolbarRedesignTarget(t, bar, "conn.active")
					closed := connectionToolbarRedesignTarget(t, bar, "conn.closed")
					if active.Y != search.Y+1 || closed.Y != search.Y+1 || sort.Y != search.Y+1 || closed.X+closed.W > search.X || sort.X < search.X+search.W {
						t.Fatalf("tabs, editor text, external count and sort must share a reading row: active=%+v closed=%+v search=%+v sort=%+v", active, closed, search, sort)
					}
					if search.InputWidth < 45 {
						t.Fatalf("moving controls out of the editor did not restore useful typing space: %d cells", search.InputWidth)
					}
					if baseline.W == 0 {
						baseline = search
					} else if search.X != baseline.X || search.W != baseline.W || search.InputWidth != baseline.InputWidth {
						t.Fatalf("typing or sorting moved/resized the editor: %+v -> %+v", baseline, search)
					}
					count := strings.TrimSpace(ansi.Cut(ansi.Strip(bar.lines[search.Y+1]), search.X+search.W, sort.X))
					if count != query.count {
						t.Fatalf("query %q count outside the editor = %q, want %q", query.value, count, query.count)
					}
					for _, target := range bar.targets {
						if target.ID == "conn.pause" {
							t.Fatal("page-header Pause was duplicated inside the filter bar")
						}
					}
				}
			}
		})
	}
}

func TestConnectionsToolbarRedesignNarrowControlsPrecedeInput(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{60, 40} {
			t.Run(fmt.Sprintf("%s/%d", language, width), func(t *testing.T) {
				m := connectionsModel(t, &fakeBackend{})
				m.locale = i18n.New(language)
				m.connections.search.SetValue("one")
				bar := m.connectionFilterBar(width)
				search := connectionToolbarRedesignTarget(t, bar, "conn.search")
				for _, id := range []string{"conn.active", "conn.closed", "conn.sort"} {
					control := connectionToolbarRedesignTarget(t, bar, id)
					if control.Y+control.H > search.Y {
						t.Fatalf("%s must precede the narrow editor: control=%+v search=%+v", id, control, search)
					}
				}
				clear := connectionToolbarRedesignTarget(t, bar, "conn.clear")
				if clear.Y != search.Y+1 || clear.X < search.X || clear.X+clear.W > search.X+search.W {
					t.Fatal("Clear must remain inside the editor frame")
				}
				for _, line := range bar.lines {
					if ansi.StringWidth(line) > width {
						t.Fatalf("narrow toolbar exceeded %d cells", width)
					}
				}
			})
		}
	}
}

func TestConnectionsToolbarRedesignFallbackGeometryStaysStableWhileSorting(t *testing.T) {
	for _, fixture := range []struct {
		language string
		width    int
	}{{i18n.English, 50}, {i18n.Chinese, 44}} {
		t.Run(fixture.language, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.locale = i18n.New(fixture.language)
			baseline := connectionToolbarRedesignTarget(t, m.connectionFilterBar(fixture.width), "conn.search")
			rows := m.connectionToolbarRows(fixture.width)
			for order := connectionSortNewest; order < connectionSortCount; order++ {
				m.connections.sort = order
				for _, query := range []string{"", "one"} {
					m.connections.search.SetValue(query)
					search := connectionToolbarRedesignTarget(t, m.connectionFilterBar(fixture.width), "conn.search")
					if search.X != baseline.X || search.Y != baseline.Y || search.W != baseline.W || search.InputWidth != baseline.InputWidth || m.connectionToolbarRows(fixture.width) != rows {
						t.Fatalf("sort=%s query=%q moved/resized the fallback editor or toolbar: baseline=%+v/%d rows got=%+v/%d rows", order.key(), query, baseline, rows, search, m.connectionToolbarRows(fixture.width))
					}
				}
			}
		})
	}
}

func TestConnectionsToolbarRedesignSearchTabFollowsVisibleOrder(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {60, 20}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.notification.items = nil
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m, _ = update(m, key("/"))
			m, _ = update(m, tea.PasteMsg{Content: "one"})
			m, _ = update(m, special(tea.KeyTab))
			if m.connections.editing || m.focus != "conn.clear" {
				t.Fatalf("Tab must leave editing at its adjacent Clear control, got %q", m.focus)
			}
			search, sort := findTarget(t, m, "conn.search"), findTarget(t, m, "conn.sort")
			if sort.Y >= search.Y {
				m, _ = update(m, special(tea.KeyTab))
			} else {
				m, _ = update(m, key("/"))
				m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			}
			if m.focus != "conn.sort" || m.connections.search.Value() != "one" || len(m.filteredConnections()) != 1 {
				t.Fatalf("sort did not follow visual reading order without discarding search: focus=%q query=%q", m.focus, m.connections.search.Value())
			}
		})
	}
}

func TestConnectionsToolbarRedesignOnlyMeaningfulStatusHintsRemain(t *testing.T) {
	for _, state := range []string{"ready", "paused", "closed", "closed-paused", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			m, _ := archiveFixture(t, &fakeBackend{})
			if strings.HasPrefix(state, "closed") {
				m.connectionActivate("conn.closed")
			}
			if strings.Contains(state, "paused") {
				m.connectionActivate("conn.pause")
			}
			if state == "unavailable" {
				m.runtime.snapshot.State, m.runtime.snapshot.Connections = "unavailable", nil
			}
			bar := m.connectionsToolbar(160, m.connectionToolbarRows(160))
			plain := ansi.Strip(strings.Join(bar.lines, "\n"))
			if state == "ready" && strings.Contains(plain, m.tr("runtime.status.live")) {
				t.Fatal("ordinary ready connection list retained the redundant Live status row")
			}
			if strings.Contains(state, "paused") && !strings.Contains(plain, m.tr("connections.paused")) {
				t.Fatal("paused snapshot lost its explanatory status")
			}
			if strings.HasPrefix(state, "closed") && !strings.Contains(plain, m.tr("connections.history_scope")) {
				t.Fatal("closed view lost the local retention scope")
			}
			if state == "unavailable" && !strings.Contains(plain, m.tr("runtime.status.api_unavailable")) {
				t.Fatal("unavailable observations lost the actionable status hint")
			}
		})
	}
}
