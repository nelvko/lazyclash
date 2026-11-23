package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestWideningWindowDoesNotDropPairedCardsAtSidebarBreakpoint(t *testing.T) {
	for _, height := range []int{9, 18, 24, 40} {
		for _, page := range []string{"overview", "settings"} {
			pairedBefore := false
			for width := 80; width <= 160; width++ {
				o := dashboardOptions{Width: width, Height: height, Page: page, Snapshot: settingsTestSnapshot()}
				g := dashboardGeometry(o)
				paired := false
				if page == "overview" {
					cards := overviewLayout(o, g.width).cards
					paired = cards[0].Y == cards[1].Y
				} else {
					body, _ := settingsContent(o, g.width)
					paired = strings.Count(strings.Split(ansi.Strip(body), "\n")[0], "┌") == 2
				}
				if pairedBefore && !paired {
					t.Fatalf("%s reverted to one column while widening to %dx%d (body %d)", page, width, height, g.width)
				}
				pairedBefore = paired
			}
			if !pairedBefore {
				t.Fatalf("%s never paired cards in a wide %d-row terminal", page, height)
			}
		}
	}
}

func TestWideShortWindowKeepsEveryNavigationDestinationReachable(t *testing.T) {
	for height := sidebarMinHeight; height <= 30; height++ {
		o := dashboardOptions{Width: sidebarMinWidth, Height: height, Page: "overview", Snapshot: settingsTestSnapshot()}
		screen, targets, _ := renderDashboard(o)
		if !dashboardGeometry(o).sidebar {
			t.Fatalf("%dx%d folded the sidebar", o.Width, height)
		}
		if height > sidebarMinHeight && len(visibleSidebarTargets(targets)) != len(navPages()) {
			t.Fatalf("%dx%d scrolled navigation before using compact items", o.Width, height)
		}
		o.Sidebar = &sidebarState{}
		for _, page := range navPages() {
			o.Focus = "nav." + page
			screen, targets, _ = renderDashboard(o)
			if !hasAction(targets, o.Focus) {
				t.Fatalf("%dx%d cannot reveal %s", o.Width, height, o.Focus)
			}
			assertCanvas(t, screen, o.Width, height)
			assertRegions(t, targets, o.Width, height)
		}
	}
}

func TestSettingsValuesAndActionsStayBesideAlignedLabels(t *testing.T) {
	for _, width := range []int{60, 100, 220} {
		o := dashboardOptions{}
		block := settingsSection(o, width, "Preferences", []settingsRow{
			{label: "Language", value: "English"},
			{label: "Shortcut hints", value: "Visible"},
			{label: "Theme", actions: []overviewControlButton{{id: "theme", label: "Choose", enabled: true}}},
		})
		var values []int
		for _, line := range block.lines {
			plain := ansi.Strip(line)
			for _, value := range []string{"English", "Visible"} {
				if index := strings.Index(plain, value); index >= 0 {
					values = append(values, ansi.StringWidth(plain[:index]))
				}
			}
		}
		if len(values) != 2 || values[0] != values[1] || values[0] != 18 {
			t.Fatalf("width %d moved values away from their label column: %v", width, values)
		}
		if len(block.targets) != 1 || block.targets[0].X != values[0] {
			t.Fatalf("width %d did not align the action with values: %+v", width, block.targets)
		}
		assertCanvas(t, strings.Join(block.lines, "\n"), width, len(block.lines))
	}
}

func TestCoreStateRemainsVisibleWhenHintsAreHidden(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, plain := range []bool{false, true} {
			for _, width := range []int{30, 40, 80, 120, 160} {
				for _, page := range navPages() {
					m := headerModel(page, width, 24)
					m.locale, m.noColor = i18n.New(language), plain
					m.keyHints.hidden = true
					for _, state := range []struct {
						service string
						active  bool
						compact string
					}{
						{"active", true, "ON"}, {"inactive", false, "OFF"}, {"activating", false, "WAIT"}, {"failed", false, "ERR"}, {"unknown", false, "?"},
					} {
						m.snapshot.Service = service.Status{State: state.service, Active: state.active}
						m.layout()
						o := m.navigationOptions()
						full, compact, _ := headerCoreState(o)
						g := dashboardGeometry(o)
						head := ansi.Strip(strings.Split(m.screen, "\n")[g.headerY])
						if g.sidebar {
							if !strings.Contains(sidebarText(m.screen), full) {
								t.Fatalf("%s %s %d plain=%v hides core %s in the sidebar", language, page, width, plain, state.service)
							}
							if strings.Contains(head, full) {
								t.Fatalf("wide header repeats the sidebar core state: %q", head)
							}
							assertCanvas(t, m.screen, width, 24)
							assertRegions(t, m.targets, width, 24)
							continue
						}
						if plain && width < 40 && compact != state.compact {
							t.Fatalf("plain state %s is ambiguous: %q", state.service, compact)
						}
						if !strings.Contains(head, compact) {
							if !strings.Contains(head, full) {
								t.Fatalf("%s %s %d plain=%v hides core %s in the title: %q", language, page, width, plain, state.service, head)
							}
						}
						assertCanvas(t, m.screen, width, 24)
						assertRegions(t, m.targets, width, 24)
					}
				}
			}
		}
	}
}

func TestCoreHeaderPendingActionOverridesPreviousObservedState(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), CoreAction: "stop", Snapshot: app.Snapshot{Installed: true, Service: service.Status{State: "active", Active: true}}}
	full, _, _ := headerCoreState(o)
	if !strings.Contains(full, "Stopping") || strings.Contains(full, "Running") {
		t.Fatalf("pending stop retained the old running badge: %q", full)
	}
}
