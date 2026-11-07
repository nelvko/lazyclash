package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestSearchControlsKeepCompactSelectionAndFocusReadableWithoutColor(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m.noColor = false
	baseline := m.ruleFilterButtons()
	if len(baseline.targets) != 3 || ansi.Strip(baseline.lines[0]) != " Aa ab .*" {
		t.Fatalf("matching options are not a compact three-option group: %+v", baseline)
	}
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ASCII} {
		m.colorProfile = profile
		for state := range 8 {
			m.rules.caseSensitive = state&1 != 0
			m.rules.wholeWord = state&2 != 0
			m.rules.regex = state&4 != 0
			m.focus, m.hover = "rules.case", "rules.word"
			block := m.ruleFilterButtons()
			if !reflect.DeepEqual(block.targets, baseline.targets) || ansi.StringWidth(block.lines[0]) != ansi.StringWidth(baseline.lines[0]) {
				t.Fatalf("selection or color capability moved matching options: profile=%s state=%d", profile, state)
			}
			plain := ansi.Strip(terminalTheme(block.lines[0], profile))
			focusMarker := "›"
			if profile == colorprofile.ASCII && m.rules.caseSensitive {
				focusMarker = "+"
			}
			if !strings.HasPrefix(plain, focusMarker) {
				t.Fatalf("keyboard focus lost its independent marker: %q", plain)
			}
			if profile == colorprofile.ASCII {
				for index, label := range []string{"Aa", "ab", ".*"} {
					marker := " "
					if state&(1<<index) != 0 {
						marker = "*"
					}
					if index == 0 {
						marker = focusMarker
					}
					want := marker + label
					if !strings.Contains(plain, want) {
						t.Fatalf("monochrome option lost its selected state: %q lacks %s", plain, want)
					}
				}
			}
		}
	}
	m.busy = true
	if len(m.ruleFilterButtons().targets) != 0 {
		t.Fatal("busy matching options remained clickable")
	}
}

func TestSearchControlsKeepRuleCountsAndInvalidExpressionVisible(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{60, 120} {
			t.Run(fmt.Sprint(language, "/", width), func(t *testing.T) {
				m := rulesModel(t, &fakeBackend{})
				m.locale = i18n.New(language)
				for _, query := range []string{"", "needle"} {
					m.rules.search.SetValue(query)
					m.rules.editing = true
					for _, id := range []string{"rules.search", "rules.case", "rules.word", "rules.regex", "rules.clear"} {
						m.focus, m.hover = id, id
						bar := m.rulesToolbar(width, m.filterToolbarRows(width))
						plain := ansi.Strip(strings.Join(bar.lines, "\n"))
						if want := m.tr("rules.filtered_count", len(m.filteredRules()), len(sampleRules())); !strings.Contains(plain, want) {
							t.Fatalf("editing or hint %s hid rule counts at width %d:\n%s", id, width, plain)
						}
					}
				}
				m.rules.regex = true
				m.rules.search.SetValue("[")
				m.focus, m.hover = "rules.case", "rules.word"
				bar := m.rulesToolbar(width, m.filterToolbarRows(width))
				plain := ansi.Strip(strings.Join(bar.lines, "\n"))
				if !strings.Contains(plain, m.tr("rules.filtered_count", 0, len(sampleRules()))) || !strings.Contains(plain, m.tr("rules.invalid_regex")) {
					t.Fatalf("matching-option hint hid invalid expression or counts:\n%s", plain)
				}
			})
		}
	}
}

func TestSearchControlsKeepLogCountsWhileEditingOrInspectingOptions(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{60, 120} {
			t.Run(fmt.Sprint(language, "/", width), func(t *testing.T) {
				m := redesignedLogsModel(160, 45, 12)
				defer m.stopLogs()
				m.locale = i18n.New(language)
				m.logs.search.SetValue("event-001")
				for _, editing := range []bool{false, true} {
					m.logs.editing = editing
					for _, id := range []string{"logs.search", "logs.level", "logs.view", "logs.reset-search"} {
						m.focus, m.hover = id, id
						bar := m.logsToolbar(width, m.filterToolbarRows(width))
						plain := ansi.Strip(strings.Join(bar.lines, "\n"))
						if !strings.Contains(plain, m.tr("logs.summary", 1, 12)) {
							t.Fatalf("editing=%t or hint %s hid retained-log counts:\n%s", editing, id, plain)
						}
					}
				}
			})
		}
	}
}

func TestSearchControlsLogClearReturnsToInputWithoutChangingCollection(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(fmt.Sprint("paused=", paused), func(t *testing.T) {
			m := redesignedLogsModel(120, 32, 12)
			defer m.stopLogs()
			if paused {
				m.pauseLogs()
			}
			m.logs.search.SetValue("no matching event")
			m.logs.level, m.logs.compact = 2, true
			m.logs.search.Blur()
			m.logs.editing, m.focus = false, "logs.reset-search"
			ctx, generation := m.logs.ctx, m.logs.generation
			retained, frozen := len(m.logs.entries), len(m.logs.frozen)
			m.logsActivate("logs.reset-search")
			if m.logs.search.Value() != "" || !m.logs.editing || !m.logs.search.Focused() || m.focus != "logs.search" {
				t.Fatal("clearing search did not return to an editable empty input")
			}
			if !m.logs.active || ctx.Err() != nil || m.logs.generation != generation || m.logs.paused != paused || len(m.logs.entries) != retained || len(m.logs.frozen) != frozen || m.logs.level != 2 || !m.logs.compact {
				t.Fatal("clearing search changed collection, pause, entries or other display settings")
			}
		})
	}
}

func TestSearchMatchingOptionsUseTextEmphasisAndWholeWordIcon(t *testing.T) {
	for _, selected := range []bool{false, true} {
		for _, hovered := range []bool{false, true} {
			for _, label := range []string{"Aa", "ab", ".*"} {
				o := buttonOptions{Selected: selected, Hovered: hovered}
				value := renderRuleSearchToggle(label, o)
				color := muted
				if hovered {
					color = text
				}
				if selected {
					color = accent
				}
				face := lipgloss.NewStyle().Bold(selected).Underline(label == "ab").Render(label)
				want := themed(" ", focus, bg, false) + themed(face, color, bg, false)
				if value != want {
					t.Fatalf("selected=%t label=%s changed the page surface or icon emphasis: got %q want %q", selected, label, value, want)
				}
			}
		}
	}
}
