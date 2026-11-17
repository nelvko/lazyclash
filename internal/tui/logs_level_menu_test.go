package tui

import (
	"fmt"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestLogsLevelMenuSelectsCurrentValueAndCommitsLocally(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{160, 45}, {60, 20}, {40, 16}, {40, 9}} {
			for _, entry := range []string{"keyboard", "mouse"} {
				t.Run(fmt.Sprint(language, "/", size, "/", entry), func(t *testing.T) {
					m := redesignedLogsModel(size[0], size[1], 12)
					defer m.stopRuntime()
					m.locale = i18n.New(language)
					m.logs.level = 2
					m.logs.search.SetValue("event")
					m.pauseLogs()
					m.logs.unread = 5
					frozen := append([]mihomo.LogEntry(nil), m.logs.frozen...)
					ctx, generation := m.runtime.ctx, m.runtime.generation
					m.layout()
					if entry == "mouse" {
						m, _ = click(m, findTarget(t, m, "logs.level"))
					} else {
						m, _ = update(m, key("l"))
					}
					if !m.menu.open || !m.logs.levelMenu || len(m.menu.items) != 4 || m.menu.selected != 2 || m.logs.level != 2 {
						t.Fatal("opening the level selector changed its value or failed to focus the current option")
					}
					m, _ = update(m, special(tea.KeyDown))
					if m.menu.selected != 3 || m.logs.level != 2 {
						t.Fatal("browsing the level menu applied an unconfirmed value")
					}
					if entry == "mouse" {
						m, _ = update(m, menuMouse(m.menu, m.menu.selected-m.menu.offset, tea.MouseLeft))
					} else {
						m, _ = update(m, special(tea.KeyEnter))
					}
					if m.menu.open || m.logs.levelMenu || m.logs.level != 3 || m.focus != "logs.level" {
						t.Fatal("choosing a level failed to close the menu or restore the trigger focus")
					}
					if !m.logs.paused || m.logs.holding || m.logs.unread != 0 || !reflect.DeepEqual(m.logs.frozen, frozen) || m.logs.search.Value() != "event" || m.runtime.generation != generation || ctx.Err() != nil || len(m.logs.entries) != 12 {
						t.Fatal("choosing a display level changed capture, retained events, search or the paused snapshot")
					}
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
				})
			}
		}
	}
}

func TestLogsLevelMenuOwnsInputAndCancelsWithoutClickThrough(t *testing.T) {
	for _, cancel := range []string{"escape", "tab", "outside click"} {
		t.Run(cancel, func(t *testing.T) {
			m := redesignedLogsModel(120, 32, 12)
			defer m.stopRuntime()
			m.logs.level = 2
			m.logs.search.SetValue("event")
			m.pauseLogs()
			m.logs.unread = 5
			m.layout()
			clear := findTarget(t, m, "logs.clear")
			ctx, generation := m.runtime.ctx, m.runtime.generation
			frozen := append([]mihomo.LogEntry(nil), m.logs.frozen...)
			m, _ = update(m, key("l"))
			m, _ = update(m, special(tea.KeyDown))
			for _, msg := range []tea.Msg{key("q"), key("2"), tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}, tea.KeyPressMsg{Code: 'r', Mod: tea.ModAlt}, tea.PasteMsg{Content: "q123"}} {
				m, _ = update(m, msg)
				if !m.menu.open || !m.logs.levelMenu || m.page != "logs" || m.quitting || m.logs.search.Value() != "event" || m.logs.regex || m.logs.level != 2 || m.runtime.generation != generation || ctx.Err() != nil {
					t.Fatalf("menu input %T leaked into search, navigation or reconnect", msg)
				}
			}
			switch cancel {
			case "escape":
				m, _ = update(m, special(tea.KeyEscape))
			case "tab":
				m, _ = update(m, special(tea.KeyTab))
			case "outside click":
				m, _ = click(m, clear)
			}
			if m.menu.open || m.logs.levelMenu || m.logs.level != 2 || m.focus != "logs.level" || !m.logs.paused || !m.logs.holding || m.logs.unread != 5 || len(m.logs.entries) != 12 || !reflect.DeepEqual(m.logs.frozen, frozen) {
				t.Fatal("cancel applied the pending value, cleared logs through the backdrop or lost the original focus")
			}
		})
	}
}

func TestLogsLevelMenuSurvivesCaptureAndTracksResizedTrigger(t *testing.T) {
	m := redesignedLogsModel(160, 45, 12)
	defer m.stopRuntime()
	m.logs.level = 2
	m.layout()
	ctx, generation := m.runtime.ctx, m.runtime.generation
	m, _ = update(m, key("l"))
	m, _ = update(m, special(tea.KeyUp))
	for _, size := range [][2]int{{120, 32}, {60, 20}, {40, 16}, {40, 9}, {160, 45}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		event := logSessionEvent(fmt.Sprint("while selecting ", size))
		event.Instance = m.runtime.instance
		before := len(m.logs.entries)
		m, _ = update(m, runtimeResultMsg{generation: generation, events: []app.RuntimeEvent{event}})
		if !m.menu.open || !m.logs.levelMenu || m.menu.selected != 1 || m.logs.level != 2 || len(m.logs.entries) != before+1 || m.runtime.generation != generation || ctx.Err() != nil {
			t.Fatalf("resize or background capture at %v closed the menu, changed its pending choice or replaced capture", size)
		}
		trigger := findTarget(t, m, "logs.level")
		bounds := m.menu.bounds
		bottom := dashboardContentBottom(m.dashboardOptions())
		if bounds.W < 1 || bounds.H < 1 || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > m.width || bounds.Y+bounds.H > bottom {
			t.Fatalf("level menu escaped the content canvas at %v: %+v", size, bounds)
		}
		if bottom-(trigger.Y+trigger.H) >= bounds.H && bounds.Y != trigger.Y+trigger.H {
			t.Fatalf("level menu did not follow its trigger below at %v: trigger=%+v menu=%+v", size, trigger, bounds)
		}
		if bottom-(trigger.Y+trigger.H) < bounds.H && trigger.Y >= bounds.H && bounds.Y+bounds.H > trigger.Y {
			t.Fatalf("level menu did not move above its trigger at %v: trigger=%+v menu=%+v", size, trigger, bounds)
		}
		assertCanvas(t, m.screen, size[0], size[1])
		assertRegions(t, m.targets, size[0], size[1])
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.menu.open || m.logs.levelMenu || m.logs.level != 2 || m.focus != "logs.level" {
		t.Fatal("closing after resize committed the pending cursor value or lost trigger focus")
	}
}

func TestLogsLevelMenuClosesWithoutCommitWhenContextChanges(t *testing.T) {
	for _, change := range []string{"busy", "page", "quit"} {
		t.Run(change, func(t *testing.T) {
			m := redesignedLogsModel(120, 32, 12)
			defer m.stopRuntime()
			m.logs.level = 2
			m.layout()
			m, _ = update(m, key("l"))
			m, _ = update(m, special(tea.KeyDown))
			switch change {
			case "busy":
				m.busy = true
				m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
			case "page":
				m.navigate("rules")
				m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
			case "quit":
				m, _ = update(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			}
			if m.menu.open || m.logs.levelMenu || m.logs.level != 2 {
				t.Fatal("a context change left a stale selection menu open or committed its cursor value")
			}
		})
	}
}
