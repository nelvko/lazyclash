package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestLogsSearchModesCombineWithLevelAndUnicodeWords(t *testing.T) {
	entries := []mihomo.LogEntry{
		{Level: "info", Message: "Alpha.example"},
		{Level: "info", Message: "alpha.example"},
		{Level: "info", Message: "alphabet.example"},
		{Level: "warning", Message: "中文.example"},
		{Level: "warning", Message: "前中文.example"},
		{Level: "warning", Message: "中文后.example"},
		{Level: "warning", Message: "中文_1.example"},
		{Level: "warning", Message: "🚀中文.example"},
		{Level: "error", Message: "a.b"},
		{Level: "error", Message: "axb"},
		{Level: "fatal", Message: "ab"},
	}
	for _, test := range []struct {
		name, query              string
		matchCase, word, regular bool
		level                    int
		want                     []int
	}{
		{name: "default ignores case", query: "ALPHA", want: []int{0, 1, 2}},
		{name: "match case", query: "Alpha", matchCase: true, want: []int{0}},
		{name: "whole ASCII word", query: "alpha", word: true, want: []int{0, 1}},
		{name: "whole Unicode word", query: "中文", word: true, want: []int{3, 7}},
		{name: "literal punctuation", query: "a.b", want: []int{8}},
		{name: "regular expression", query: "a.b$", regular: true, want: []int{8, 9}},
		{name: "regular match case", query: "Alpha\\.", matchCase: true, regular: true, want: []int{0}},
		{name: "regular whole word alternatives", query: "a|ab", word: true, regular: true, want: []int{8, 10}},
		{name: "regular Unicode whole word", query: "中[文]", word: true, regular: true, want: []int{3, 7}},
		{name: "raw level remains searchable", query: "^warning ", regular: true, want: []int{3, 4, 5, 6, 7}},
		{name: "level and expression intersect", query: "^warning ", level: 1, regular: true},
		{name: "warning alias level", query: "中文", level: 2, word: true, want: []int{3, 7}},
		{name: "fatal belongs to error level", query: "ab", level: 3, word: true, want: []int{10}},
		{name: "blank query keeps level filter", query: " ", level: 3, regular: true, want: []int{8, 9, 10}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := logsModel()
			m.logs.entries = entries
			m.logs.search.SetValue(test.query)
			m.logs.caseSensitive, m.logs.wholeWord, m.logs.regex, m.logs.level = test.matchCase, test.word, test.regular, test.level
			match, err := m.logMatcher()
			if err != nil {
				t.Fatalf("valid search failed: %v", err)
			}
			var got []int
			for index, entry := range entries {
				if match(entry) {
					got = append(got, index)
				}
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("query %q level=%d returned %v; want %v", test.query, test.level, got, test.want)
			}
			if len(m.logs.entries) != len(entries) {
				t.Fatal("local search changed retained events")
			}
		})
	}
}

func TestLogsInvalidRegexRecoversWithoutReplacingCapture(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := redesignedLogsModel(120, 32, 0)
			defer m.stopRuntime()
			m.locale = i18n.New(language)
			m.appendLogs([]mihomo.LogEntry{{Level: "info", Message: "Needle.example"}, {Level: "error", Message: "other"}})
			ctx, generation := m.runtime.ctx, m.runtime.generation
			m, _ = update(m, key("/"))
			m, _ = update(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModAlt})
			m, _ = update(m, tea.PasteMsg{Content: "["})
			if _, err := m.logMatcher(); err == nil || !m.logs.regex || len(m.logDocument(80).lines) != 0 || !strings.Contains(ansi.Strip(m.screen), m.tr("logs.invalid_regex")) {
				t.Fatal("invalid expression did not hide matches and report its error")
			}
			m, _ = update(m, special(tea.KeyBackspace))
			m, _ = update(m, tea.PasteMsg{Content: "^info needle\\."})
			if _, err := m.logMatcher(); err != nil || m.logDocument(80).matched != 1 || strings.Contains(ansi.Strip(m.screen), m.tr("logs.invalid_regex")) {
				t.Fatal("correcting the expression did not restore matching events")
			}
			if !m.logs.editing || !m.logs.search.Focused() || len(m.logs.entries) != 2 || m.runtime.generation != generation || ctx.Err() != nil {
				t.Fatal("editing a regular expression changed capture, entries or input ownership")
			}
		})
	}
}

func TestLogsSearchShortcutsKeepPausedSnapshotAndInputOwnership(t *testing.T) {
	m := redesignedLogsModel(120, 32, 12)
	defer m.stopRuntime()
	m.pauseLogs()
	frozen := append([]mihomo.LogEntry(nil), m.logs.frozen...)
	ctx, generation := m.runtime.ctx, m.runtime.generation
	m, _ = update(m, key("/"))
	m, _ = update(m, tea.PasteMsg{Content: "event"})
	for _, code := range []rune{'c', 'w', 'r'} {
		m, _ = update(m, tea.KeyPressMsg{Code: code, Mod: tea.ModAlt})
		if !m.logs.editing || !m.logs.search.Focused() || m.focus != "logs.search" || m.logs.search.Value() != "event" {
			t.Fatalf("Alt+%c escaped the editor or entered shortcut text", code)
		}
	}
	if !m.logs.caseSensitive || !m.logs.wholeWord || !m.logs.regex {
		t.Fatal("search shortcuts did not enable all three matching modes")
	}
	for _, value := range []string{"c", "w", "r"} {
		m, _ = update(m, key(value))
	}
	if m.logs.search.Value() != "eventcwr" || m.page != "logs" || m.quitting {
		t.Fatal("ordinary matching-option letters were stolen from the input")
	}
	m, _ = update(m, special(tea.KeyEnter))
	for _, code := range []rune{'c', 'w', 'r'} {
		m, _ = update(m, tea.KeyPressMsg{Code: code, Mod: tea.ModAlt})
	}
	if m.logs.caseSensitive || m.logs.wholeWord || m.logs.regex || m.logs.editing {
		t.Fatal("matching shortcuts did not toggle off from the log list")
	}
	if !m.logs.paused || !reflect.DeepEqual(m.logs.frozen, frozen) || m.runtime.generation != generation || ctx.Err() != nil || len(m.logs.entries) != 12 {
		t.Fatal("matching shortcuts replaced capture or the paused display snapshot")
	}
}

func TestLogsMatchingOptionsSupportMouseAndTabAtWideAndNarrowSizes(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {60, 20}, {40, 16}, {40, 9}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := redesignedLogsModel(size[0], size[1], 12)
			defer m.stopRuntime()
			ctx, generation := m.runtime.ctx, m.runtime.generation
			m, _ = update(m, key("/"))
			m, _ = update(m, tea.PasteMsg{Content: "event"})
			for _, id := range []string{"logs.reset-search", "logs.case", "logs.word", "logs.regex", "logs.level", "logs.view", "logs.list"} {
				m, _ = update(m, special(tea.KeyTab))
				if m.focus != id || m.logs.editing || m.logs.search.Focused() {
					t.Fatalf("Tab should visit %s; focus=%s editing=%t", id, m.focus, m.logs.editing)
				}
			}
			field := findTarget(t, m, "logs.search")
			for _, id := range []string{"logs.case", "logs.word", "logs.regex"} {
				m, _ = click(m, findTarget(t, m, id))
			}
			if !m.logs.caseSensitive || !m.logs.wholeWord || !m.logs.regex || m.logs.search.Value() != "event" || m.logs.editing {
				t.Fatal("matching-option click edited or cleared search instead of toggling its mode")
			}
			if findTarget(t, m, "logs.search") != field || m.runtime.generation != generation || ctx.Err() != nil {
				t.Fatal("toggling matching options moved the search field or reconnected capture")
			}
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
		})
	}
}

func TestLogsClearSearchKeepsMatchingModesAndResetFiltersClearsThem(t *testing.T) {
	m := redesignedLogsModel(120, 32, 12)
	defer m.stopRuntime()
	m.logs.search.SetValue("event")
	m.logs.caseSensitive, m.logs.wholeWord, m.logs.regex, m.logs.level = true, true, true, 3
	ctx, generation := m.runtime.ctx, m.runtime.generation
	m.logsActivate("logs.reset-search")
	if m.logs.search.Value() != "" || !m.logs.caseSensitive || !m.logs.wholeWord || !m.logs.regex || m.logs.level != 3 || !m.logs.editing {
		t.Fatal("clearing search reset matching modes or failed to return to the editor")
	}
	m.logs.search.SetValue("[")
	m.logsActivate("logs.reset-filters")
	if m.logs.search.Value() != "" || m.logs.caseSensitive || m.logs.wholeWord || m.logs.regex || m.logs.level != 0 || m.logs.editing || m.focus != "logs.list" {
		t.Fatal("reset filters did not clear text, level and matching modes")
	}
	if m.runtime.generation != generation || ctx.Err() != nil || len(m.logs.entries) != 12 || m.logDocument(80).matched != 12 {
		t.Fatal("clearing filters changed capture or lost retained entries")
	}
}
