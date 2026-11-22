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
					bulkID := "conn.close-all"
					if closed {
						bulkID = "conn.clear-history"
					}
					pause, bulk := findTarget(t, m, "conn.pause"), findTarget(t, m, bulkID)
					search := findTarget(t, m, "conn.search")
					if pause.Y != bulk.Y || pause.X+pause.W > bulk.X || pause.Y >= search.Y {
						t.Fatalf("Pause must precede %s above the input: pause=%+v action=%+v search=%+v", bulkID, pause, bulk, search)
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

func TestConnectionsToolbarWideQueryCountFollowsStableSearch(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.locale = i18n.New(language)
			baseline := connectionToolbarRedesignTarget(t, m.connectionFilterBar(160), "conn.search")
			for _, query := range []struct{ value, count string }{{"", ""}, {"one", "1 / 2"}, {"example", "2 / 2"}, {"no-match", "0 / 2"}, {"  ", ""}} {
				m.connections.search.SetValue(query.value)
				bar := m.connectionFilterBar(160)
				search := connectionToolbarRedesignTarget(t, bar, "conn.search")
				closed := connectionToolbarRedesignTarget(t, bar, "conn.closed")
				if search != baseline || search.InputWidth != 42 || closed.Y != search.Y+1 || closed.X+closed.W > search.X {
					t.Fatalf("query %q displaced tabs or search: closed=%+v search=%+v baseline=%+v", query.value, closed, search, baseline)
				}
				plain := ansi.Strip(bar.lines[search.Y+1])
				count := strings.TrimSpace(ansi.Cut(plain, search.X+search.W, ansi.StringWidth(plain)))
				if count != query.count {
					t.Fatalf("query %q count beside search = %q, want %q", query.value, count, query.count)
				}
				if hasAction(bar.targets, "conn.pause") || hasAction(bar.targets, "conn.sort") {
					t.Fatal("wide filter bar duplicated a page or table header action")
				}
			}
		})
	}
}

func TestConnectionsToolbarRedesignCompactTabFacesHaveOneCellGap(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.locale, m.noColor = i18n.New(language), true
			bar := m.connectionFilterBar(160)
			active := connectionToolbarRedesignTarget(t, bar, "conn.active")
			closed := connectionToolbarRedesignTarget(t, bar, "conn.closed")
			if active.Y != closed.Y || active.W != closed.W {
				t.Fatalf("compact tab peers must retain equal hit widths: active=%+v closed=%+v", active, closed)
			}
			plain := ansi.Strip(bar.lines[active.Y])
			activeFace := strings.TrimSpace(ansi.Cut(plain, active.X, active.X+active.W))
			closedFace := strings.TrimSpace(ansi.Cut(plain, closed.X, closed.X+closed.W))
			activeStart, closedStart := strings.Index(plain, activeFace), strings.Index(plain, closedFace)
			if activeFace == "" || closedFace == "" || activeStart < 0 || closedStart < 0 {
				t.Fatalf("compact tab faces are missing: %q", plain)
			}
			gap := ansi.StringWidth(plain[:closedStart]) - ansi.StringWidth(plain[:activeStart]) - ansi.StringWidth(activeFace)
			if gap != 1 {
				t.Fatalf("compact tab faces must be separated by one cell, got %d: %q", gap, plain)
			}
		})
	}
}

func TestConnectionsToolbarRedesignClearKeepsInputAndControlsFixed(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{160, 45}, {60, 20}, {40, 16}} {
			t.Run(fmt.Sprint(language, "/", size), func(t *testing.T) {
				m := connectionsModel(t, &fakeBackend{})
				m.locale = i18n.New(language)
				m.notification.items = nil
				m.connections.search.SetValue("one")
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				var before []hitRegion
				ids := []string{"conn.search", "conn.case", "conn.word", "conn.regex"}
				if size[0] == 160 {
					ids = append(ids, "conn.sort.destination")
				} else {
					ids = append(ids, "conn.sort")
				}
				for _, id := range ids {
					before = append(before, findTarget(t, m, id))
				}
				m, _ = click(m, findTarget(t, m, "conn.clear"))
				if !m.connections.editing || m.connections.search.Value() != "" || m.connectionCount() != "" {
					t.Fatal("Clear must return to an empty editor with no query count")
				}
				for i, id := range ids {
					if after := findTarget(t, m, id); after != before[i] {
						t.Fatalf("clearing search moved/resized %s: %+v -> %+v", id, before[i], after)
					}
				}
			})
		}
	}
}

func TestConnectionsToolbarRedesignNarrowControlsPrecedeInput(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{connectionTableMinWidth - 1, 60, 40} {
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
			sortID := "conn.sort"
			if size[0] == 160 {
				sortID = "conn.sort.destination"
				for _, id := range []string{"conn.case", "conn.word", "conn.regex", sortID} {
					m, _ = update(m, special(tea.KeyTab))
					if m.focus != id {
						t.Fatalf("Tab should reach %s in visual order, got %q", id, m.focus)
					}
				}
			} else {
				m, _ = update(m, key("/"))
				m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			}
			if m.focus != sortID || m.connections.search.Value() != "one" || len(m.filteredConnections()) != 1 {
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
			if state == "closed" && len(bar.lines) != len(m.connectionFilterBar(160).lines)+1 {
				t.Fatal("ordinary history view added a status line between search and column headers")
			}
			if state == "unavailable" && !strings.Contains(plain, m.tr("runtime.status.api_unavailable")) {
				t.Fatal("unavailable observations lost the actionable status hint")
			}
		})
	}
}
