package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func TestSevenPageKeysDoNotEscapeInputsOrHelp(t *testing.T) {
	m := ready(&fakeBackend{})
	for i, page := range navPages() {
		m, _ = update(m, key(string(rune('1'+i))))
		if m.page != page {
			t.Fatalf("shortcut %d opened %s, expected %s", i+1, m.page, page)
		}
	}
	for _, state := range []string{"form", "wizard", "connection-search", "rule-search", "log-search", "help"} {
		t.Run(state, func(t *testing.T) {
			m := ready(&fakeBackend{})
			switch state {
			case "form":
				m.openForm()
			case "wizard":
				m.openWizard(true)
			case "connection-search":
				m.page = "connections"
				m.focusConnectionSearch()
			case "rule-search":
				m.page = "rules"
				m.focusRulesSearch()
			case "log-search":
				m.page = "logs"
				m.logsActivate("logs.search")
			case "help":
				m.helpOpen = true
			}
			page := m.page
			for _, digit := range "1234567" {
				m, _ = update(m, key(string(digit)))
				if m.page != page {
					t.Fatal("printable digit escaped the active input/modal")
				}
			}
			var value string
			switch state {
			case "form":
				value = m.input.Value()
			case "wizard":
				value = m.wizard.local.Value()
			case "connection-search":
				value = m.connections.search.Value()
			case "rule-search":
				value = m.rules.search.Value()
			case "log-search":
				value = m.logs.search.Value()
			case "help":
				return
			}
			if value != "1234567" {
				t.Fatalf("digits did not edit the input: %q", value)
			}
		})
	}
}

func TestNavigationArrowsAndTabHaveIndependentRegions(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height = 120, 32
	m.browsePage("rules")
	m.layout()
	m, _ = update(m, special(tea.KeyDown))
	if m.page != "logs" || m.focus != "nav.logs" || m.scroll != 0 {
		t.Fatal("sidebar arrow did not browse the page while retaining navigation focus")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.page != "logs" {
		t.Fatal("Enter did not activate the chosen navigation item")
	}
	m.browsePage("overview")
	m.layout()
	m, _ = update(m, special(tea.KeyTab))
	if strings.HasPrefix(m.focus, "nav.") {
		t.Fatal("Tab walked through every sidebar entry instead of entering the content")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focus != "nav.overview" {
		t.Fatal("reverse Tab did not restore the navigation group")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.sidebarOpen || m.focus != "menu" || !hasAction(m.targets, m.focus) || hasAction(m.targets, "nav.overview") {
		t.Fatal("folding the sidebar did not collapse navigation into its menu entry")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	m, _ = update(m, special(tea.KeyTab))
	if m.sidebarOpen || strings.HasPrefix(m.focus, "nav.") {
		t.Fatal("restoring the sidebar trapped focus in the compact navigation popup")
	}
}

func TestSidebarFoldsWithoutOpeningDrawerAcrossResize(t *testing.T) {
	for _, size := range [][2]int{{99, 32}, {80, 24}, {120, 19}, {20, 7}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := pageNavigationModel()
			directionCleanup(t, &m)
			m.browsePage("rules")
			m.layout()
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			// Repeated size and ordinary redraws must not reopen navigation.
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.layout()
			if m.page != "rules" || m.sidebarOpen || m.focus != "menu" || !hasAction(m.targets, "menu") {
				t.Fatalf("folding left a drawer or inaccessible focus: page=%s open=%t focus=%s", m.page, m.sidebarOpen, m.focus)
			}
			for _, target := range m.targets {
				if strings.HasPrefix(target.ID, "nav.") {
					t.Fatal("folded sidebar retained a navigation overlay")
				}
			}
			m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
			if m.sidebarOpen || m.focus != "nav.rules" || !hasAction(m.targets, m.focus) {
				t.Fatal("expanding did not restore focus to the persistent sidebar")
			}
		})
	}
}

func TestSidebarResizePreservesContentAndExplicitDrawer(t *testing.T) {
	m := pageNavigationModel()
	directionCleanup(t, &m)
	m.navigate("settings")
	m.focus = "key-hints.toggle"
	m.layout()
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.focus != "key-hints.toggle" || m.sidebarOpen || !hasAction(m.targets, m.focus) {
		t.Fatal("folding navigation disturbed content focus")
	}
	m.activate("menu")
	m.layout()
	m, _ = update(m, tea.WindowSizeMsg{Width: 70, Height: 24})
	if !m.sidebarOpen || m.focus != "nav.settings" || !hasAction(m.targets, m.focus) {
		t.Fatal("resizing a manually opened compact menu dismissed it")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	if m.sidebarOpen || m.focus != "nav.settings" || !hasAction(m.targets, m.focus) {
		t.Fatal("expanding left the compact drawer over the persistent sidebar")
	}
}

func TestHelpIsScrollableCapturesInputAndRestoresFocus(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height = 80, 24
	m.focus = "setup.install"
	m, _ = update(m, key("?"))
	if !m.helpOpen || !strings.Contains(ansi.Strip(m.screen), "Help / Overview") {
		t.Fatal("help did not open")
	}
	for _, hit := range m.targets {
		if hit.ID != "help.close" && hit.ID != "key-hints.toggle" {
			t.Fatal("background retained mouse targets under Help")
		}
	}
	m, _ = update(m, special(tea.KeyEnd))
	if m.helpScroll == 0 || !strings.Contains(ansi.Strip(m.screen), "Messages") {
		t.Fatal("help content cannot be scrolled to the end")
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 20, Height: 5})
	assertCanvas(t, m.View().Content, 20, 5)
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	m, _ = update(m, special(tea.KeyEscape))
	if m.helpOpen || m.focus != "setup.install" || m.page != "overview" {
		t.Fatal("closing help did not restore the previous page and focus")
	}
}

func TestCompactMenuCapturesPageSearchAndDismissesBeforeBodyClick(t *testing.T) {
	for _, page := range []string{"connections", "rules", "logs"} {
		m := ready(&fakeBackend{})
		m.width, m.height = 80, 24
		m.navigate(page)
		m.activate("menu")
		m.layout()
		m, _ = update(m, key("/"))
		if !m.sidebarOpen || m.connections.editing || m.rules.editing || m.logs.editing || !strings.HasPrefix(m.focus, "nav.") {
			t.Fatalf("%s search activated behind the compact menu", page)
		}
		m, _ = update(m, tea.MouseClickMsg{X: 60, Y: 5, Button: tea.MouseLeft})
		if m.sidebarOpen || m.focus != "menu" {
			t.Fatal("outside click did not dismiss navigation without activating content")
		}
		m.activate("menu")
		m.activate("menu")
		m.layout()
		if m.sidebarOpen || m.focus != "menu" {
			t.Fatal("menu trigger could not close its popup")
		}
	}
}

func TestDismissedGuideKeepsCardOperationsUsable(t *testing.T) {
	added := false
	m := ready(&fakeBackend{snapshot: app.Snapshot{GuideDismissed: true}, add: func(context.Context, string, string) error { added = true; return nil }})
	m.width, m.height = 160, 45
	m.layout()
	if strings.Contains(ansi.Strip(m.screen), "Get started") {
		t.Fatal("layout reset a dismissed guide")
	}
	findTarget(t, m, "card.profiles")
	findTarget(t, m, "core.install")
	if hasAction(m.contentTargets(), "card.install") {
		t.Fatal("uninstalled overview retained a duplicate kernel installation action")
	}
	m, _ = update(m, m.activate("card.profiles"))
	if m.page != "profiles" || m.form != "" || added {
		t.Fatal("Manage profiles did not open the configuration page without importing")
	}
}
