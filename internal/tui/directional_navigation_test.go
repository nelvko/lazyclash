package tui

import (
	"fmt"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func directionKey(value string) tea.KeyPressMsg {
	switch value {
	case "left":
		return special(tea.KeyLeft)
	case "right":
		return special(tea.KeyRight)
	default:
		return key(value)
	}
}

func directionCleanup(t *testing.T, m *Model) {
	t.Helper()
	t.Cleanup(func() {
		m.stopRuntime()
		m.stopRules()
		m.stopLogs()
		m.stopCoreStatus()
		if m.cancel != nil {
			m.cancel()
		}
	})
}

func TestDirectionalNavigationSidebarRestoresWithoutActivating(t *testing.T) {
	for _, busy := range []bool{false, true} {
		for _, enter := range []string{"right", "l"} {
			t.Run(fmt.Sprintf("%s/busy=%t", enter, busy), func(t *testing.T) {
				m := pageNavigationModel()
				directionCleanup(t, &m)
				m.snapshot = app.Snapshot{Installed: true, ConfigReady: true, HasRun: true, GuideDismissed: true, Service: service.Status{Active: true, State: "active"}}
				m.focus = "core.stop"
				if busy {
					// Actions are disabled during a write; logical card focus remains
					// available and must restore without entering its controls.
					m.focus = "overview.card.dashboard.control.core"
					m.newJob("action")
				}
				want := m.focus
				m.returnToNavigation()
				m.layout()
				id := m.id
				for _, back := range []string{"left", "h"} {
					m, _ = update(m, directionKey(back))
					if m.focus != "nav.overview" || m.page != "overview" || m.sidebarOpen {
						t.Fatalf("permanent sidebar %s moved or opened navigation", back)
					}
				}
				m, _ = update(m, directionKey(enter))
				if m.focus != want || m.page != "overview" || m.id != id || m.busy != busy || m.failure != "" {
					t.Fatalf("%s activated the restored control or lost focus: focus=%s busy=%t", enter, m.focus, m.busy)
				}
				if busy && m.opCtx.Err() != nil {
					t.Fatal("entering content canceled the background operation")
				}
			})
		}
	}
}

func TestDirectionalNavigationDrawerEnterAndBackKeepBrowsedPage(t *testing.T) {
	for _, button := range []string{"right", "l", "left", "h"} {
		t.Run(button, func(t *testing.T) {
			m := pageNavigationModel()
			directionCleanup(t, &m)
			m.width, m.height = 80, 24
			m.pageViews["settings"] = pageViewState{focus: "key-hints.toggle"}
			m.browsePage("settings")
			m.sidebarOpen = true
			m.layout()
			m, _ = update(m, directionKey(button))
			want := "key-hints.toggle"
			if button == "left" || button == "h" {
				want = "menu"
			}
			if m.sidebarOpen || m.page != "settings" || m.focus != want || m.keyHints.changed {
				t.Fatalf("drawer %s did not close at the current page without activation: page=%s focus=%s", button, m.page, m.focus)
			}
		})
	}
}

func TestDirectionalNavigationContentBackPreservesPageState(t *testing.T) {
	fixtures := []struct {
		name string
		make func(*testing.T) Model
	}{
		{"overview", func(*testing.T) Model { m := pageNavigationModel(); m.enterContent(); return m }},
		{"settings", func(*testing.T) Model { m := pageNavigationModel(); m.navigate("settings"); return m }},
		{"proxies", func(*testing.T) Model {
			m := pageNavigationModel()
			m.snapshot.Installed, m.snapshot.Service.Active = true, true
			m.snapshot.Groups = []mihomo.Group{{Name: "PROXY", Type: "Selector", Now: "DIRECT", All: []string{"DIRECT", "node"}}}
			setProxyTestGroup(&m, "PROXY")
			m.navigate("proxies")
			m.focus = "node.0.0"
			return m
		}},
		{"connections", func(t *testing.T) Model {
			m := connectionsModel(t, &fakeBackend{})
			m.connections.search.SetValue("example")
			return m
		}},
		{"rules", func(t *testing.T) Model {
			m := rulesModel(t, &fakeBackend{})
			m.rules.search.SetValue("example")
			return m
		}},
		{"logs", func(*testing.T) Model {
			m := logsModel()
			m.width, m.height = 120, 32
			m.logs.paused, m.logs.anchor.row = true, 4
			m.logs.search.SetValue("retained")
			return m
		}},
	}
	for _, fixture := range fixtures {
		for _, back := range []string{"left", "h"} {
			t.Run(fixture.name+"/"+back, func(t *testing.T) {
				m := fixture.make(t)
				directionCleanup(t, &m)
				m.layout()
				focus, selectedConnection, selectedRule := m.focus, m.connections.selected, m.rules.selected
				id, applied := m.id, m.snapshot.Applied
				m, _ = update(m, directionKey(back))
				if m.page != fixture.name || m.focus != "nav."+fixture.name || m.id != id || m.snapshot.Applied != applied {
					t.Fatalf("%s did not return to current-page navigation: page=%s focus=%s", back, m.page, m.focus)
				}
				m, _ = update(m, directionKey("right"))
				if m.focus != focus || m.connections.selected != selectedConnection || m.rules.selected != selectedRule {
					t.Fatal("returning to content lost its remembered focus or list selection")
				}
				if fixture.name == "proxies" && m.proxies.activeGroup != "PROXY" ||
					fixture.name == "connections" && m.connections.search.Value() != "example" ||
					fixture.name == "rules" && m.rules.search.Value() != "example" ||
					fixture.name == "logs" && (m.logs.search.Value() != "retained" || !m.logs.paused || m.logs.anchor.row != 4) {
					t.Fatal("directional navigation reset page-specific state")
				}
			})
		}
	}
}

func TestDirectionalNavigationDetailsBackOneLayerDuringBackgroundWork(t *testing.T) {
	for _, page := range []string{"connections", "rules"} {
		for _, back := range []string{"left", "h"} {
			for _, busy := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/busy=%t", page, back, busy), func(t *testing.T) {
					m := rulesModel(t, &fakeBackend{})
					if page == "connections" {
						m = connectionsModel(t, &fakeBackend{})
					}
					directionCleanup(t, &m)
					selected := m.focus
					m, _ = update(m, special(tea.KeyEnter))
					if busy {
						m.newJob("action")
					}
					id := m.id
					m, _ = update(m, directionKey(back))
					if m.page != page || m.focus != selected || m.connections.detail != nil || m.rules.detail != nil {
						t.Fatalf("first %s bypassed the list layer: focus=%s", back, m.focus)
					}
					m, _ = update(m, directionKey(back))
					if m.focus != "nav."+page || m.page != page || m.busy != busy || m.id != id {
						t.Fatal("second back did not return to navigation independently of the operation")
					}
					if busy && m.opCtx.Err() != nil {
						t.Fatal("directional back canceled the background operation")
					}
				})
			}
		}
	}
}

func TestDirectionalNavigationProfileGridKeepsHorizontalKeys(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(fmt.Sprintf("busy=%t", busy), func(t *testing.T) {
			m := profileNavigationModel(160, 32)
			directionCleanup(t, &m)
			if busy {
				m.newJob("action")
			}
			id, applied := m.id, m.snapshot.Applied
			for _, step := range []struct{ button, focus string }{{"right", "profile.1"}, {"left", "profile.0"}, {"l", "profile.1"}, {"h", "profile.0"}, {"left", "profile.0"}} {
				m, _ = update(m, directionKey(step.button))
				if m.focus != step.focus || m.page != "profiles" || m.snapshot.Applied != applied || m.id != id || m.busy != busy {
					t.Fatalf("grid %s was intercepted or applied a profile: focus=%s", step.button, m.focus)
				}
			}
		})
	}
}

func TestDirectionalNavigationInputsOwnCursorAndPrintableKeys(t *testing.T) {
	fixtures := []struct {
		name  string
		setup func(*testing.T) Model
		input func(*Model) *textinput.Model
	}{
		{"profile URL", func(*testing.T) Model { m := profileNavigationModel(120, 32); m.activate("profiles.url"); return m }, func(m *Model) *textinput.Model { return &m.profilesUI.url }},
		{"connection search", func(t *testing.T) Model {
			m := connectionsModel(t, &fakeBackend{})
			m.focusConnectionSearch()
			return m
		}, func(m *Model) *textinput.Model { return &m.connections.search }},
		{"rule search", func(t *testing.T) Model { m := rulesModel(t, &fakeBackend{}); m.focusRulesSearch(); return m }, func(m *Model) *textinput.Model { return &m.rules.search }},
		{"log search", func(*testing.T) Model {
			m := logsModel()
			m.width, m.height = 120, 32
			m.logsActivate("logs.search")
			return m
		}, func(m *Model) *textinput.Model { return &m.logs.search }},
		{"generic form", func(*testing.T) Model { m := pageNavigationModel(); m.openForm(); return m }, func(m *Model) *textinput.Model { return &m.input }},
		{"profile create", func(*testing.T) Model { m := profileNavigationModel(120, 32); m.openProfileCreate(); return m }, func(m *Model) *textinput.Model { return m.profileCreateActive() }},
		{"wizard local", func(*testing.T) Model { m := pageNavigationModel(); m.openWizard(true); return m }, func(m *Model) *textinput.Model { return &m.wizard.local }},
		{"ports", func(t *testing.T) Model { return portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}}) }, func(m *Model) *textinput.Model { return &m.ports.mixed }},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			m := fixture.setup(t)
			directionCleanup(t, &m)
			fixture.input(&m).SetValue("ab")
			fixture.input(&m).CursorEnd()
			m.layout()
			page, focus, form := m.page, m.focus, m.form
			m, _ = update(m, directionKey("left"))
			if fixture.input(&m).Position() != 1 {
				t.Fatal("Left did not move the input cursor")
			}
			m, _ = update(m, directionKey("right"))
			if fixture.input(&m).Position() != 2 {
				t.Fatal("Right did not move the input cursor")
			}
			m, _ = update(m, directionKey("h"))
			m, _ = update(m, directionKey("l"))
			if fixture.input(&m).Value() != "abhl" || m.page != page || m.focus != focus || m.form != form {
				t.Fatalf("input keys escaped to navigation: value=%q focus=%s", fixture.input(&m).Value(), m.focus)
			}
		})
	}
}

func TestDirectionalNavigationOverlaysOwnTheirKeys(t *testing.T) {
	for _, owner := range []string{"help", "notifications", "profile menu", "connection confirmation", "wizard choice"} {
		t.Run(owner, func(t *testing.T) {
			m := profileNavigationModel(120, 32)
			switch owner {
			case "help":
				m.keyHintsAction("help.open")
			case "notifications":
				m.notificationAction("notification.expand")
			case "profile menu":
				m.openProfileMenuForCard(1)
			case "connection confirmation":
				m = connectionsModel(t, &fakeBackend{})
				m, _ = update(m, key("x"))
			case "wizard choice":
				m.openNetwork(false)
				m.wizard.focus = "download-method"
			}
			directionCleanup(t, &m)
			m.layout()
			page, focus, selection := m.page, m.focus, m.menu.selected
			for _, button := range []string{"left", "right", "h", "l"} {
				m, _ = update(m, directionKey(button))
				if m.page != page || m.focus != focus || m.sidebarOpen || m.busy {
					t.Fatalf("%s escaped %s to page navigation", button, owner)
				}
				switch owner {
				case "help":
					if !m.helpOpen {
						t.Fatal("direction key closed help")
					}
				case "notifications":
					if !m.notification.panel.open {
						t.Fatal("direction key closed notifications")
					}
				case "profile menu":
					if !m.menu.open || m.menu.selected != selection {
						t.Fatal("direction key escaped or selected the profile menu")
					}
				case "connection confirmation":
					if !m.connections.confirming || m.connections.detail == nil {
						t.Fatal("direction key escaped connection confirmation")
					}
				case "wizard choice":
					if !m.wizard.networkOpen || m.wizard.focus != "download-method" {
						t.Fatal("direction key escaped the wizard choice")
					}
				}
			}
		})
	}
}

func TestDirectionalNavigationShiftTabStillMovesToPreviousControl(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(fmt.Sprintf("busy=%t", busy), func(t *testing.T) {
			m := profileNavigationModel(120, 32)
			directionCleanup(t, &m)
			m.focus = "profiles.add"
			if busy {
				m.newJob("action")
			}
			m.layout()
			m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			if m.focus != "profiles.url" || m.page != "profiles" || m.busy != busy {
				t.Fatalf("Shift+Tab did not move to the preceding input control: %s", m.focus)
			}
		})
	}
}
