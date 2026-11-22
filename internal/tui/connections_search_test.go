package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestConnectionsSearchModesMatchCaseUnicodeWordsAndExpressions(t *testing.T) {
	var entries []mihomo.Connection
	for index, host := range []string{"Alpha.example", "alpha.example", "alphabet.example", "中文.example", "前中文.example", "中文后.example", "中文_1.example", "🚀中文.example", "a.b", "axb", "ab"} {
		entries = append(entries, fixtureConnection(fmt.Sprint("row-", index), host, index))
	}
	for _, test := range []struct {
		name, query              string
		matchCase, word, regular bool
		want                     []int
	}{
		{name: "default ignores case", query: "ALPHA", want: []int{0, 1, 2}},
		{name: "match case", query: "Alpha", matchCase: true, want: []int{0}},
		{name: "whole ASCII word", query: "alpha", word: true, want: []int{0, 1}},
		{name: "whole Unicode word", query: "中文", word: true, want: []int{3, 7}},
		{name: "literal punctuation", query: "a.b", want: []int{8}},
		{name: "regular expression", query: "a.b$", regular: true, want: []int{8, 9}},
		{name: "field anchors", query: `^alpha\.example$`, regular: true, want: []int{0, 1}},
		{name: "regular match case", query: `^Alpha\.`, matchCase: true, regular: true, want: []int{0}},
		{name: "whole word alternatives", query: "a|ab", word: true, regular: true, want: []int{8, 10}},
		{name: "regular Unicode word", query: "中[文]", word: true, regular: true, want: []int{3, 7}},
		{name: "blank query ignores matching modes", query: "  ", matchCase: true, word: true, regular: true, want: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := Model{connections: connectionsState{search: textinput.New()}}
			m.connections.search.SetValue(test.query)
			m.connections.caseSensitive, m.connections.wholeWord, m.connections.regex = test.matchCase, test.word, test.regular
			match, err := m.connectionMatcher()
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
				t.Fatalf("query %q returned %v; want %v", test.query, got, test.want)
			}
		})
	}
}

func TestConnectionsSearchMatchesEachMetadataFieldWithoutCrossingBoundaries(t *testing.T) {
	entry := mihomo.Connection{
		ID: "id-token", Rule: "rule-token", RulePayload: "payload-token", Chains: []string{"first chain", "second chain"},
		Metadata: mihomo.ConnectionMetadata{Host: "host-token", SourceIP: "192.0.2.11", SourcePort: "12345", DestinationIP: "198.51.100.22", DestinationPort: "443", Network: "network-token", Type: "type-token", Process: "process-token", ProcessPath: "/tmp/process-path-token"},
	}
	m := Model{connections: connectionsState{search: textinput.New(), regex: true}}
	for _, field := range []string{entry.ID, entry.Metadata.Host, entry.Metadata.SourceIP, entry.Metadata.SourcePort, entry.Metadata.DestinationIP, entry.Metadata.DestinationPort, entry.Metadata.Network, entry.Metadata.Type, entry.Metadata.Process, entry.Metadata.ProcessPath, entry.Rule, entry.RulePayload, entry.Chains[0], entry.Chains[1]} {
		m.connections.search.SetValue("^" + strings.ReplaceAll(field, ".", `\.`) + "$")
		match, err := m.connectionMatcher()
		if err != nil || !match(entry) {
			t.Fatalf("field %q was not independently searchable: %v", field, err)
		}
	}
	for _, query := range []string{"id-token host-token", "192.0.2.11 12345", "first chain second chain"} {
		for _, regular := range []bool{false, true} {
			m.connections.regex = regular
			m.connections.search.SetValue(query)
			match, err := m.connectionMatcher()
			if err != nil || match(entry) {
				t.Fatalf("query %q regular=%t crossed field boundaries: %v", query, regular, err)
			}
		}
	}
}

func TestConnectionsInvalidRegexRecoversAndKeepsSnapshot(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.locale = i18n.New(language)
			before := append([]mihomo.Connection(nil), m.connectionSnapshot().Connections...)
			generation := m.runtime.generation
			m, _ = update(m, key("/"))
			m, _ = update(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModAlt})
			m, _ = update(m, tea.PasteMsg{Content: "["})
			if _, err := m.connectionMatcher(); err == nil || len(m.filteredConnections()) != 0 || !strings.Contains(ansi.Strip(m.screen), m.tr("connections.invalid_regex")) || !strings.Contains(ansi.Strip(m.screen), "0 / 2") {
				t.Fatal("invalid expression did not report its error and zero filtered count")
			}
			if hasAction(m.targets, "connection.first") || hasAction(m.targets, "connection.second") {
				t.Fatal("invalid search retained stale row actions")
			}
			m, _ = update(m, special(tea.KeyBackspace))
			m, _ = update(m, tea.PasteMsg{Content: `^one\.example\.invalid$`})
			if _, err := m.connectionMatcher(); err != nil || len(m.filteredConnections()) != 1 || strings.Contains(ansi.Strip(m.screen), m.tr("connections.invalid_regex")) {
				t.Fatal("correcting the expression did not restore its connection")
			}
			if !m.connections.editing || !m.connections.search.Focused() || m.runtime.generation != generation || !reflect.DeepEqual(m.connectionSnapshot().Connections, before) {
				t.Fatal("regular expression editing changed monitoring, data or input ownership")
			}
		})
	}
}

func TestConnectionsSearchShortcutsKeepPausedActiveAndHistoricalSnapshots(t *testing.T) {
	calls := 0
	m, first := archiveFixture(t, &fakeBackend{control: func(context.Context, string) error { calls++; return nil }, closeConnection: func(context.Context, string, time.Time) error { calls++; return nil }, closeAllConnections: func(context.Context) error { calls++; return nil }})
	m.connectionActivate("conn.pause")
	frozen, frozenClosed := *m.connections.frozen, append([]closedConnection(nil), m.connections.frozenClosed...)
	generation := m.runtime.generation
	m, _ = update(m, key("/"))
	m, _ = update(m, tea.PasteMsg{Content: "example"})
	for _, code := range []rune{'c', 'w', 'r'} {
		m, _ = update(m, tea.KeyPressMsg{Code: code, Mod: tea.ModAlt})
		if !m.connections.editing || !m.connections.search.Focused() || m.connections.search.Value() != "example" {
			t.Fatalf("Alt+%c escaped the input or inserted shortcut text", code)
		}
	}
	if !m.connections.caseSensitive || !m.connections.wholeWord || !m.connections.regex {
		t.Fatal("matching shortcuts did not enable all three modes")
	}
	for _, value := range []string{"c", "w", "r"} {
		m, _ = update(m, key(value))
	}
	if m.connections.search.Value() != "examplecwr" || m.page != "connections" || m.quitting {
		t.Fatal("ordinary matching-option letters escaped the editor")
	}
	m.connections.search.SetValue(`^one\.example\.invalid$`)
	m.connections.caseSensitive, m.connections.wholeWord = false, false
	m, _ = update(m, special(tea.KeyEnter))
	m.connectionActivate("conn.closed")
	if list := m.filteredConnections(); len(list) != 1 || list[0].ID != first.ID || !m.connections.regex {
		t.Fatal("historical view lost the query or matching modes")
	}
	archiveObserve(&m, nil, time.Now().Add(-time.Second))
	if len(m.connectionClosedEntries()) != 1 || len(m.connections.history.entries()) != 2 {
		t.Fatal("search stopped live history collection or changed the frozen view")
	}
	m.connectionActivate("conn.active")
	m.connections.search.SetValue(`^two\.example\.invalid$`)
	if list := m.filteredConnections(); len(list) != 1 || list[0].ID != "second" {
		t.Fatal("active frozen snapshot did not use the same matcher")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModAlt})
	if m.connections.regex || m.connections.editing || !m.connections.paused || !reflect.DeepEqual(*m.connections.frozen, frozen) || !reflect.DeepEqual(m.connections.frozenClosed, frozenClosed) || m.runtime.generation != generation || calls != 0 {
		t.Fatal("browsing search options changed paused observations or invoked a backend action")
	}
}

func TestConnectionsMatchingOptionsUseMouseAndPreserveGeometry(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{160, 45}, {80, 24}, {60, 20}, {40, 16}, {40, 9}} {
			t.Run(fmt.Sprint(language, "/", size), func(t *testing.T) {
				m := connectionsModel(t, &fakeBackend{})
				m.locale = i18n.New(language)
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m, _ = update(m, key("/"))
				m, _ = update(m, tea.PasteMsg{Content: "one"})
				field := listSearchTarget(t, m.focusTargets(), "conn.search")
				generation := m.runtime.generation
				for _, id := range []string{"conn.case", "conn.word", "conn.regex"} {
					for remaining := len(m.focusTargets()); m.focus != id && remaining > 0; remaining-- {
						m, _ = update(m, special(tea.KeyTab))
					}
					if m.focus != id {
						t.Fatalf("Tab could not reveal %s", id)
					}
					m, _ = click(m, findTarget(t, m, id))
				}
				if !m.connections.caseSensitive || !m.connections.wholeWord || !m.connections.regex || m.connections.editing || m.connections.search.Value() != "one" || len(m.filteredConnections()) != 1 || m.runtime.generation != generation {
					t.Fatal("matching-option click changed input ownership, query, results or monitoring")
				}
				if got := listSearchTarget(t, m.focusTargets(), "conn.search"); got.X != field.X || got.Y != field.Y || got.W != field.W || got.InputWidth != field.InputWidth {
					t.Fatal("toggling matching options moved the input frame")
				}
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			})
		}
	}
}

func TestConnectionsSearchClearKeepsModesAndResetFiltersClearsThem(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m.connections.search.SetValue("one")
	m.connections.caseSensitive, m.connections.wholeWord, m.connections.regex = true, true, true
	generation := m.runtime.generation
	m.connectionActivate("conn.clear")
	if m.connections.search.Value() != "" || !m.connections.caseSensitive || !m.connections.wholeWord || !m.connections.regex || !m.connections.editing {
		t.Fatal("clear search reset matching modes or failed to return to the editor")
	}
	m.connections.search.SetValue("[")
	m.connectionActivate("conn.reset-filter")
	if m.connections.search.Value() != "" || m.connections.caseSensitive || m.connections.wholeWord || m.connections.regex || len(m.filteredConnections()) != 2 || m.runtime.generation != generation {
		t.Fatal("clear filters did not reset text and matching modes locally")
	}
}

func TestConnectionsMatchingShortcutsDoNotLeakThroughDetailsOrConfirmation(t *testing.T) {
	for _, modal := range []string{"details", "confirmation", "help", "busy"} {
		t.Run(modal, func(t *testing.T) {
			m := connectionsModel(t, &fakeBackend{})
			m.connections.search.SetValue("one")
			m.reconcileConnections()
			switch modal {
			case "details":
				m.connectionActivate("connection.first")
			case "confirmation":
				m, _ = click(m, findTarget(t, m, "conn.close-all"))
			case "help":
				m, _ = update(m, key("?"))
			case "busy":
				m.busy = true
			}
			for _, code := range []rune{'c', 'w', 'r'} {
				m, _ = update(m, tea.KeyPressMsg{Code: code, Mod: tea.ModAlt})
			}
			if m.connections.caseSensitive || m.connections.wholeWord || m.connections.regex || m.connections.search.Value() != "one" || m.connections.editing {
				t.Fatalf("matching shortcut escaped %s", modal)
			}
		})
	}
}
