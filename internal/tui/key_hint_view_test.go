package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestKeyHintsKeepHelpVisibleOnEveryPage(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{20, 40, 60, 80, 120} {
			for _, page := range navPages() {
				t.Run(fmt.Sprintf("%s/%s/%d", language, page, width), func(t *testing.T) {
					m := ready(&fakeBackend{})
					m.locale, m.page, m.focus = i18n.New(language), page, "content"
					if page == "overview" {
						m.focus = "overview.card.core"
					}
					o := dashboardOptions{Locale: m.locale, Width: width, Height: 24, Page: page, Help: m.contextHelp()}
					screen, _, _ := renderDashboard(o)
					footer := strings.TrimSpace(ansi.Strip(strings.Split(screen, "\n")[23]))
					help := m.tr("dashboard.footer.help")
					if strings.Count(footer, help) != 1 || strings.Contains(footer, "…") {
						t.Fatalf("help missing, duplicated, or hints cut mid-item: %q", footer)
					}
					tail := help
					if width >= 40 {
						tail += " · " + m.tr("dashboard.footer.quit")
					}
					if !strings.HasSuffix(footer, tail) {
						t.Fatalf("help is not pinned at the right: %q", footer)
					}
					assertCanvas(t, screen, width, 24)
				})
			}
		}
	}
}

func TestKeyHintsPreferActivationAndDropWholeSecondaryHints(t *testing.T) {
	o := dashboardOptions{Width: 40, Help: "s Sort · t Test · ↑↓ Move · Enter Select · / Search · Esc Back · ? Help"}
	footer := ansi.Strip(dashboardKeyHints(o))
	if !strings.HasPrefix(footer, "Enter Select") || strings.Contains(footer, "Sort") || strings.Contains(footer, "Test") {
		t.Fatalf("secondary keys displaced the current action: %q", footer)
	}
	o.Width = 80
	footer = ansi.Strip(dashboardKeyHints(o))
	if !strings.HasPrefix(footer, "Enter Select · / Search · ↑↓ Move · Esc Back") {
		t.Fatalf("wider footer did not restore primary hints in order: %q", footer)
	}
	for width := 20; width <= 180; width++ {
		o.Width = width
		footer = ansi.Strip(dashboardKeyHints(o))
		if ansi.StringWidth(footer) != width-4 || strings.Contains(footer, "…") {
			t.Fatalf("width %d has clipped or overflowing hints: %q", width, footer)
		}
		left, _, _ := strings.Cut(footer, "  ")
		for _, item := range strings.Split(strings.TrimSpace(left), " · ") {
			if item != "" && !strings.Contains(o.Help, item+" · ") {
				t.Fatalf("width %d retained a partial hint %q in %q", width, item, footer)
			}
		}
	}
}

func TestEditingHintExplainsHowToReachHelp(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, page := range []string{"proxies", "profiles", "connections", "rules", "logs"} {
			m, _ := proxiesModel(t, 80, 24)
			m.locale, m.page, m.width, m.height = i18n.New(language), page, 80, 24
			switch page {
			case "proxies":
				m.focus, m.proxies.editing = "proxies.search", true
			case "profiles":
				m.focus, m.profilesUI.editing = "profiles.url", true
			case "connections":
				m.focus, m.connections.editing = "conn.search", true
			case "rules":
				m.focus, m.rules.editing = "rules.search", true
			case "logs":
				m.focus, m.logs.editing = "logs.search", true
			}
			m.layout()
			footer := strings.TrimSpace(ansi.Strip(strings.Split(m.screen, "\n")[23]))
			if !strings.HasSuffix(footer, "Esc → "+m.tr("dashboard.footer.help")) || strings.Contains(footer, m.tr("dashboard.footer.quit")) {
				t.Fatalf("%s/%s: editing advertises a shortcut owned by input: %q", language, page, footer)
			}
		}
	}
}

func TestProxiesHintsDescribeFocusedAction(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m, _ := proxiesModel(t, 160, 40)
		m.locale = i18n.New(language)
		for _, tc := range []struct {
			focus, group, action string
			width                int
		}{
			{"group.0", "", "expand", 160},
			{"group.0", "PROXY", "collapse", 160},
			{"group.0", "PROXY", "directory", 80},
			{"node.0.1", "PROXY", "select", 160},
			{"node.1.0", "AUTO", "inspect", 160},
			{"proxies.reference.0.0", "PROXY", "open", 160},
			{"proxies.delay.0.1", "PROXY", "testNode", 160},
			{"proxies.sort.0", "PROXY", "sort", 160},
			{"proxies.test.0", "PROXY", "testGroup", 160},
			{"proxies.back", "PROXY", "back", 160},
			{"proxies.directory", "PROXY", "directory", 160},
			{"proxies.search", "PROXY", "search", 160},
			{"proxies.search-open", "PROXY", "search", 160},
			{"proxies.clear", "PROXY", "clear", 160},
		} {
			setProxyTestGroup(&m, tc.group)
			m.focus, m.width = tc.focus, tc.width
			got := m.contextHelp()
			if !strings.HasPrefix(got, m.tr("navigation.proxies.action."+tc.action)+" · ") {
				t.Fatalf("%s %s width %d advertises the wrong action: %q", language, tc.focus, tc.width, got)
			}
		}
		m.focus, m.proxies.testing = "proxies.test.0", true
		m.proxies.testGroup = "PROXY"
		if !strings.HasPrefix(m.contextHelp(), m.tr("navigation.proxies.action.cancelTest")) {
			t.Fatal("running group test did not advertise cancellation")
		}
		m.proxies.editing = true
		if m.contextHelp() != m.tr("navigation.proxies.searching") {
			t.Fatal("search editing retained node selection hints")
		}
	}
}

func TestHelpShowsCurrentPageBeforeGeneralNavigation(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, page := range navPages() {
			m := Model{locale: i18n.New(language), page: page, width: 60, height: 18, helpOpen: true}
			lines := m.helpLines(54)
			want := m.tr("help.currentPage", pageTitle(page, m.locale))
			if len(lines) < 2 || lines[0] != want || lines[1] == m.tr("help.navigation") {
				t.Fatalf("%s %s puts general help before page keys: %v", language, page, lines)
			}
			view, hits := m.renderHelp("")
			if !strings.Contains(ansi.Strip(view), lines[1]) {
				t.Fatalf("%s %s hides its first page shortcut at 60x18:\n%s", language, page, ansi.Strip(view))
			}
			assertCanvas(t, view, 60, 18)
			assertRegions(t, hits, 60, 18)
		}
	}
}

func TestHelpIncludesOnlyCurrentPageAndCommonInstructions(t *testing.T) {
	pageInstructions := map[string]string{
		"overview": "help.overview.keys", "proxies": "help.proxies.expand",
		"profiles": "help.profiles.add", "connections": "help.connections.keys",
		"rules": "help.rules.keys", "logs": "help.logs.keys", "settings": "help.settings.keys",
	}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, page := range navPages() {
			m := Model{locale: i18n.New(language), page: page}
			body := strings.Join(m.helpLines(1000), "\n")
			for other, instruction := range pageInstructions {
				if strings.Contains(body, m.tr(instruction)) != (page == other) {
					t.Fatalf("%s %s included the wrong page instructions: %s", language, page, other)
				}
			}
			for _, common := range []string{"help.navigation", "help.controls", "help.forms.input", "help.messages.controls"} {
				if !strings.Contains(body, m.tr(common)) {
					t.Fatalf("%s %s omitted common instructions: %s", language, page, common)
				}
			}
		}
	}
}
