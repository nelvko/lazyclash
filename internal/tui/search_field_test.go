package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func searchFieldModel(t *testing.T, page string, width, height int) (Model, string) {
	t.Helper()
	var m Model
	id := page + ".search"
	switch page {
	case "proxies":
		m, _ = proxiesModel(t, width, height)
	case "connections":
		m = connectionsModel(t, &fakeBackend{})
		id = "conn.search"
	case "rules":
		m = rulesModel(t, &fakeBackend{})
	case "logs":
		m = logsModel()
	default:
		t.Fatalf("unknown search page %q", page)
	}
	m.width, m.height = width, height
	m.layout()
	return m, id
}

func pageSearchInput(m *Model) *textinput.Model {
	switch m.page {
	case "proxies":
		return &m.proxies.search
	case "connections":
		return &m.connections.search
	case "rules":
		return &m.rules.search
	case "logs":
		return &m.logs.search
	}
	panic("page has no search input")
}

func TestSearchFieldBorderClicksFocusInput(t *testing.T) {
	for _, page := range []string{"proxies", "connections", "rules", "logs"} {
		for _, size := range [][2]int{{160, 45}, {80, 24}, {40, 16}} {
			t.Run(fmt.Sprint(page, "/", size), func(t *testing.T) {
				m, id := searchFieldModel(t, page, size[0], size[1])
				for _, edge := range []int{0, 2} {
					field := findTarget(t, m, id)
					if field.H != 3 {
						t.Fatalf("search frame should occupy three rows: %+v", field)
					}
					m, _ = update(m, tea.MouseClickMsg{X: field.X + field.W/2, Y: field.Y + edge, Button: tea.MouseLeft})
					if m.focus != id || !pageSearchInput(&m).Focused() {
						t.Fatalf("frame row %d did not focus its input: focus=%s", edge, m.focus)
					}
					m, _ = update(m, special(tea.KeyEscape))
				}
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			})
		}
	}
}

func TestSearchFieldLogsShortWindowRetainsVisibleTargets(t *testing.T) {
	for _, width := range []int{40, 60} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, id := searchFieldModel(t, "logs", width, 12)
			if m.dashboardOptions().BodyTopRows != 0 {
				t.Fatal("fixture did not use the short-window toolbar fallback")
			}
			field := findTarget(t, m, id)
			if field.H != 3 {
				t.Fatalf("visible search frame was clipped: %+v", field)
			}
			m, _ = click(m, field)
			if !m.logs.editing || !m.logs.search.Focused() {
				t.Fatal("visible short-window search field did not accept a click")
			}
			m, _ = update(m, special(tea.KeyEscape))
			m, _ = click(m, findTarget(t, m, "logs.level"))
			if m.logs.level != 1 {
				t.Fatal("visible short-window filter control did not accept a click")
			}
			assertCanvas(t, m.screen, width, 12)
			assertRegions(t, m.targets, width, 12)
		})
	}
}

func TestSearchFieldUnicodeQueryRetainsCursorAcrossResize(t *testing.T) {
	query := strings.Repeat("中文-query-", 20) + "VISIBLE-TAIL"
	for _, page := range []string{"proxies", "connections", "rules", "logs"} {
		for _, language := range []string{i18n.English, i18n.Chinese} {
			t.Run(page+"/"+language, func(t *testing.T) {
				m, id := searchFieldModel(t, page, 160, 45)
				m.locale = i18n.New(language)
				m, _ = click(m, findTarget(t, m, id))
				m, _ = update(m, tea.PasteMsg{Content: query})
				for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}, {40, 16}, {5, 2}, {160, 45}} {
					m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					input := pageSearchInput(&m)
					if input.Value() != query || input.Position() != len([]rune(query)) {
						t.Fatalf("resize to %v lost query or cursor: value=%q position=%d", size, input.Value(), input.Position())
					}
					if size[0] >= 40 && !strings.Contains(ansi.Strip(m.screen), "VISIBLE-TAIL") {
						t.Fatalf("resize to %v hid the current query tail:\n%s", size, ansi.Strip(m.screen))
					}
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
				}
			})
		}
	}
}

func TestSearchFieldFixedControlsAlignWithFrame(t *testing.T) {
	for _, page := range []string{"connections", "rules", "logs"} {
		for _, width := range []int{160, 60} {
			t.Run(fmt.Sprint(page, "/", width), func(t *testing.T) {
				m, id := searchFieldModel(t, page, width, 32)
				switch page {
				case "connections":
					m.runtime.snapshot.Connections.Connections = nil
					for index := range 50 {
						m.runtime.snapshot.Connections.Connections = append(m.runtime.snapshot.Connections.Connections, fixtureConnection(fmt.Sprintf("row-%02d", index), "example.invalid", index))
					}
					m.reconcileConnections()
				case "rules":
					m = longRedesignedRules(t, width, 32)
				case "logs":
					m = redesignedLogsModel(width, 32, 50)
					defer m.stopLogs()
				}
				m.layout()
				field := findTarget(t, m, id)
				controlID := map[string]string{"connections": "conn.clear", "rules": "rules.case", "logs": "logs.level"}[page]
				control := findTarget(t, m, controlID)
				if width >= 100 {
					if control.Y != field.Y+1 || control.X < field.X+field.W {
						t.Fatalf("wide controls should align with the input row: field=%+v control=%+v", field, control)
					}
				} else if control.Y < field.Y+field.H {
					t.Fatalf("narrow controls overlap the search frame: field=%+v control=%+v", field, control)
				}
				if page == "logs" {
					m.logsScroll(-20)
				} else {
					m.scroll = 20
				}
				m.layout()
				if page == "logs" && !m.logs.holding || page != "logs" && m.scroll == 0 {
					t.Fatal("fixture did not scroll its list")
				}
				if got := findTarget(t, m, id); got != field {
					t.Fatalf("body scrolling moved the fixed frame: before=%+v after=%+v", field, got)
				}
			})
		}
	}
}
