package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestSearchCursorColumnUsesBubblesVisibleCells(t *testing.T) {
	for _, test := range []struct {
		name, value string
		width, left int
		want        int
	}{
		{"placeholder", "", 10, 0, 0},
		{"ASCII", "github", 10, 2, 4},
		{"Chinese", "中文", 10, 0, 4},
		{"mixed", "a中b", 10, 1, 3},
		{"scrolled ASCII", "abcdefghijklmno", 6, 3, 3},
		{"scrolled repeated", strings.Repeat("ab", 12), 6, 3, 3},
		{"scrolled Chinese", "前缀中文abcdefghijk", 8, 3, 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := textinput.New()
			input.Prompt = ""
			input.Placeholder = "筛选条件"
			input.SetWidth(test.width)
			input.SetValue(test.value)
			input.Focus()
			input.CursorEnd()
			input.SetCursor(input.Position() - test.left)
			input.SetVirtualCursor(false)
			if got, ok := searchCursorColumn(input); !ok || got != test.want {
				t.Fatalf("visible cursor column = %d, found=%t; want %d", got, ok, test.want)
			}
			if input.VirtualCursor() || strings.Contains(input.View(), "38;2;1;2;3") {
				t.Fatal("cursor measurement modified the displayed input")
			}
		})
	}
}

func cursorSearchModel(t *testing.T, page string) (Model, string) {
	t.Helper()
	if page == "providers" {
		m, _ := ruleProvidersModel(t, 3)
		m.layout()
		return m, "providers.search"
	}
	return searchFieldModel(t, page, 120, 32)
}

func assertSearchCursorCell(t *testing.T, m Model, want string) {
	t.Helper()
	view := m.View()
	cursor := view.Cursor
	if cursor == nil || cursor.Shape != tea.CursorBar || !cursor.Blink {
		t.Fatalf("search should expose a blinking real bar: %+v", cursor)
	}
	rows := strings.Split(view.Content, "\n")
	if cursor.Y < 0 || cursor.Y >= len(rows) {
		t.Fatalf("cursor is outside the rendered rows: %+v", cursor)
	}
	if got := ansi.Strip(ansi.Cut(rows[cursor.Y], cursor.X, cursor.X+ansi.StringWidth(want))); got != want {
		t.Fatalf("cursor %+v points to %q, want %q\n%s", cursor, got, want, ansi.Strip(rows[cursor.Y]))
	}
	if strings.Contains(view.Content, "38;2;1;2;3") || strings.Contains(view.Content, "\x1b[7m") || strings.Contains(view.Content, "\x1b[7;") {
		t.Fatal("real cursor leaked a virtual block into the displayed content")
	}
}

func TestSearchCursorTracksChineseAndScrolledMiddleEditing(t *testing.T) {
	for _, page := range []string{"connections", "rules", "logs", "providers"} {
		t.Run(page, func(t *testing.T) {
			m, id := cursorSearchModel(t, page)
			m, _ = click(m, findTarget(t, m, id))
			m, _ = update(m, tea.PasteMsg{Content: "中文"})
			field := findTarget(t, m, id)
			cursor := m.View().Cursor
			if cursor == nil || cursor.X != field.X+2+4 || cursor.Y != field.Y+1 {
				t.Fatalf("Chinese text has the wrong cursor offset: field=%+v cursor=%+v", field, cursor)
			}
			m, _ = update(m, special(tea.KeyLeft))
			assertSearchCursorCell(t, m, "文")
			m, _ = update(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
			m, _ = update(m, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
			query := strings.Repeat("中文abcdefgh", 20) + "VISIBLE-TAIL"
			m, _ = update(m, tea.PasteMsg{Content: query})
			for range 3 {
				m, _ = update(m, special(tea.KeyLeft))
			}
			assertSearchCursorCell(t, m, "A")
			for _, size := range [][2]int{{80, 24}, {60, 20}, {120, 32}} {
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				assertSearchCursorCell(t, m, "A")
			}
			for range 5 {
				m, _ = update(m, special(tea.KeyLeft))
			}
			assertSearchCursorCell(t, m, "B")
			m, _ = update(m, special(tea.KeyHome))
			assertSearchCursorCell(t, m, "中")
			m, _ = update(m, special(tea.KeyRight))
			assertSearchCursorCell(t, m, "文")
		})
	}
}

func TestSearchCursorHidesBehindOverlaysAndClippedFields(t *testing.T) {
	m, id := cursorSearchModel(t, "rules")
	m, _ = click(m, findTarget(t, m, id))
	if m.View().Cursor == nil {
		t.Fatal("fixture has no focused cursor")
	}
	for _, test := range []struct {
		name string
		set  func(*Model)
	}{
		{"busy", func(m *Model) { m.busy = true }},
		{"help", func(m *Model) { m.helpOpen = true }},
		{"menu", func(m *Model) { m.menu.open = true }},
		{"sidebar", func(m *Model) { m.sidebarOpen = true }},
		{"ports", func(m *Model) { m.ports.open = true }},
		{"wizard", func(m *Model) { m.wizard.open = true }},
		{"network wizard", func(m *Model) { m.wizard.networkOpen = true }},
		{"form", func(m *Model) { m.form = "source" }},
		{"notifications", func(m *Model) { m.notification.panel.open = true }},
		{"unfocused", func(m *Model) { m.focus = "content" }},
		{"covered field", func(m *Model) { m.targets = nil }},
		{"partial field", func(m *Model) {
			m.targets = []hitRegion{findTarget(t, *m, id)}
			m.targets[0].H = 2
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := m
			test.set(&changed)
			if cursor := changed.View().Cursor; cursor != nil {
				t.Fatalf("hidden input retained a real cursor: %+v", cursor)
			}
		})
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 5, Height: 2})
	if m.View().Cursor != nil {
		t.Fatal("tiny layout retained the offscreen cursor")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	if m.View().Cursor == nil {
		t.Fatal("restoring the visible input did not restore its cursor")
	}
}

func TestSearchCursorRespectsColorCapability(t *testing.T) {
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
		for _, noColor := range []bool{false, true} {
			t.Run(fmt.Sprint(profile, "/", noColor), func(t *testing.T) {
				m, id := cursorSearchModel(t, "logs")
				m.colorProfile, m.noColor = profile, noColor
				m, _ = click(m, findTarget(t, m, id))
				view := m.View()
				if view.Cursor == nil || view.Cursor.Shape != tea.CursorBar {
					t.Fatal("color capability removed the real search cursor")
				}
				if (noColor || profile == colorprofile.ASCII) && (view.Cursor.Color != nil || strings.Contains(view.Content, "\x1b")) {
					t.Fatal("uncolored output retained a cursor color or canvas style")
				}
				if view.ForegroundColor != nil || view.BackgroundColor != nil {
					t.Fatal("search cursor changed the terminal's default palette")
				}
			})
		}
	}
}
