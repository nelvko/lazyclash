package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestSelectionGroupsKeepEqualWidthsAndAdjacentFocusGutters(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, plain := range []bool{false, true} {
			for _, width := range []int{20, 40, 60, 160} {
				t.Run(fmt.Sprintf("%s/plain=%t/width=%d", language, plain, width), func(t *testing.T) {
					o := dashboardOptions{Locale: i18n.New(language), Snapshot: settingsTestSnapshot(), PlainButtons: plain}
					modeIDs := []string{"mode.rule", "mode.global", "mode.direct"}
					check := func(block dashboardBlock, ids []string) {
						t.Helper()
						body := strings.Join(block.lines, "\n")
						assertRegions(t, block.targets, width, len(block.lines))
						var previous hitRegion
						for _, id := range ids {
							var current hitRegion
							for _, hit := range block.targets {
								if hit.ID == id {
									current = hit
									break
								}
							}
							if current.ID == "" {
								t.Fatalf("choice %s has no hit region", id)
							}
							painted := settingsPaintedTarget(t, body, block.targets, id)
							if strings.Contains(painted, "✓") || ansi.StringWidth(painted) != current.W {
								t.Fatalf("choice face does not match its hit region: %q / %+v", painted, current)
							}
							if previous.ID != "" {
								if current.W != previous.W {
									t.Fatalf("choice widths differ: %+v / %+v", previous, current)
								}
								if current.Y == previous.Y && current.X != previous.X+previous.W {
									t.Fatalf("extra space was added before the fixed focus gutter: %+v / %+v", previous, current)
								}
							}
							previous = current
						}
						assertCanvas(t, body, width, len(block.lines))
					}
					check(overviewModeCard(o, width), modeIDs)
					check(settingsNetwork(o, width), modeIDs)
					check(settingsInterface(o, width), []string{"language.en", "language.zh-Hans"})
					m, _ := proxiesModel(t, 160, 40)
					m.locale, m.noColor = o.Locale, plain
					check(m.proxyModes(width), modeIDs)
				})
			}
		}
	}
}

func TestProxyModeSelectionShowsSavedPreferenceAndObservedMismatch(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m, _ := proxiesModel(t, 160, 40)
		m.locale, m.noColor = i18n.New(language), true
		m.snapshot.Mode.Desired, m.snapshot.Mode.Actual = "rule", "global"
		block := m.proxyModes(80)
		body := strings.Join(block.lines, "\n")
		for _, mode := range []string{"rule", "global", "direct"} {
			painted := settingsPaintedTarget(t, body, block.targets, "mode."+mode)
			if strings.Contains(painted, "<") != (mode == "rule") {
				t.Fatalf("%s: mode selection did not reflect the saved preference: %q", language, painted)
			}
		}
		body, _ = m.proxiesBody(120)
		if !strings.Contains(ansi.Strip(body), m.tr("dashboard.settings.actualMode", m.tr("dashboard.mode.global"))) {
			t.Fatal("saved mode selection hid the different running mode")
		}
		m.snapshot.Mode.Error = "mode observation failed"
		body, _ = m.proxiesBody(120)
		if !strings.Contains(ansi.Strip(body), m.tr("dashboard.settings.actualMode", m.tr("dashboard.status.unknown"))) {
			t.Fatal("failed mode observation was presented as a known running mode")
		}
	}
}

func TestBusyModeControlsKeepSelectionWithoutHitRegions(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Snapshot: settingsTestSnapshot(), Busy: true, PlainButtons: true}
	for _, block := range []dashboardBlock{overviewModeCard(o, 80), settingsNetwork(o, 80)} {
		if len(block.targets) != 0 || strings.Count(ansi.Strip(strings.Join(block.lines, "\n")), "<") != 1 {
			t.Fatal("busy mode controls lost their saved selection or retained actionable targets")
		}
	}
	m, _ := proxiesModel(t, 160, 40)
	m.noColor, m.busy = true, true
	block := m.proxyModes(80)
	if len(block.targets) != 0 || strings.Count(ansi.Strip(strings.Join(block.lines, "\n")), "<") != 1 {
		t.Fatal("busy proxy mode controls lost their saved selection or retained actionable targets")
	}
}

func TestNarrowRulesFiltersKeepEveryLabelAndStableHitRegions(t *testing.T) {
	filters := []struct{ id, label string }{
		{"rules.case", "Aa"}, {"rules.word", "ab"}, {"rules.regex", ".*"},
	}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{34, 40} {
			for _, plain := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/width=%d/plain=%t", language, width, plain), func(t *testing.T) {
					m := longRedesignedRules(t, width, 32)
					m.locale, m.noColor = i18n.New(language), plain
					m.layout()
					baseline := make(map[string]hitRegion)
					for _, filter := range filters {
						baseline[filter.id] = findTarget(t, m, filter.id)
					}
					for _, selected := range []bool{false, true} {
						m.rules.caseSensitive, m.rules.wholeWord, m.rules.regex = selected, selected, selected
						for _, focused := range filters {
							m.focus, m.hover = focused.id, focused.id
							m.layout()
							for _, filter := range filters {
								hit := findTarget(t, m, filter.id)
								if hit != baseline[filter.id] {
									t.Fatalf("selection or focus moved %s: %+v to %+v", filter.id, baseline[filter.id], hit)
								}
								painted := settingsPaintedTarget(t, m.screen, m.targets, filter.id)
								if !strings.Contains(painted, filter.label) {
									t.Fatalf("visible filter %s lost label %q: %q", filter.id, filter.label, painted)
								}
							}
							assertCanvas(t, m.screen, width, 32)
							assertRegions(t, m.targets, width, 32)
						}
					}
				})
			}
		}
	}
}
