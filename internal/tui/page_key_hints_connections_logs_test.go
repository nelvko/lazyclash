package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func hintHasKey(hint, key string) bool {
	for _, part := range strings.Split(hint, " · ") {
		label, _, _ := strings.Cut(part, " ")
		if label == key {
			return true
		}
	}
	return false
}

func TestConnectionsHintsFollowInputDetailsAndConfirmation(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := connectionsModel(t, &fakeBackend{})
		m.locale = i18n.New(language)
		if !strings.Contains(m.contextHelp(), m.tr("navigation.connections.action.details")) || !strings.Contains(m.contextHelp(), m.tr("navigation.connections.review")) {
			t.Fatal("focused connection did not advertise details and target review")
		}
		m, _ = update(m, key("/"))
		if m.contextHelp() != m.tr("navigation.connections.searching") {
			t.Fatal("search input advertises list actions")
		}
		m, _ = update(m, special(tea.KeyEnter))
		if m.connections.detail != nil || m.connections.editing {
			t.Fatal("search Enter did not just return to the results")
		}
		m, _ = update(m, special(tea.KeyEnter))
		if m.connections.detail == nil || !strings.Contains(m.contextHelp(), m.tr("navigation.connections.action.back")) {
			t.Fatal("details did not advertise the focused Back action")
		}
		for _, ignored := range []string{"p", "s", "v", "x", "y"} {
			if hintHasKey(m.contextHelp(), ignored) {
				t.Fatalf("detail advertises ignored %s key", ignored)
			}
			paused, sort, closed := m.connections.paused, m.connections.sort, m.connections.closedView
			m, _ = update(m, key(ignored))
			if m.connections.paused != paused || m.connections.sort != sort || m.connections.closedView != closed {
				t.Fatalf("detail unexpectedly acted on %s", ignored)
			}
		}
		m, _ = update(m, special(tea.KeyTab))
		if m.focus != "conn.close" || !strings.HasPrefix(m.contextHelp(), m.tr("navigation.connections.action.close")) || hintHasKey(m.contextHelp(), "x") || hintHasKey(m.contextHelp(), "y") {
			t.Fatal("focused disconnect button advertised another shortcut or another action")
		}
		m, _ = update(m, special(tea.KeyEscape))
		if m.connections.detail != nil {
			t.Fatal("Esc did not return from details")
		}
		m, _ = click(m, findTarget(t, m, "conn.close-all"))
		if !m.connections.confirming || !strings.HasPrefix(m.contextHelp(), m.tr("navigation.connections.action.cancel")) {
			t.Fatal("confirmation did not advertise default Enter Cancel")
		}
		for _, ignored := range []string{"/", "p", "s", "v", "x"} {
			if hintHasKey(m.contextHelp(), ignored) {
				t.Fatalf("confirmation advertises ignored %s key", ignored)
			}
		}
		m, _ = update(m, special(tea.KeyTab))
		if m.focus != "conn.confirm" || !strings.HasPrefix(m.contextHelp(), m.tr("navigation.connections.action.confirm")) {
			t.Fatal("confirmation focus did not update Enter action")
		}
		m, _ = update(m, special(tea.KeyTab))
		m, _ = update(m, special(tea.KeyEnter))
		if m.connections.confirming || m.busy {
			t.Fatal("default Enter did not cancel without closing")
		}
	}
}

func TestConnectionsHintsOnlyOfferAvailableRowActions(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m, _ = update(m, key("p"))
	if !m.connections.paused || !strings.Contains(m.contextHelp(), m.tr("navigation.connections.resume")) || strings.Contains(m.contextHelp(), m.tr("navigation.connections.pause")) {
		t.Fatal("paused list still advertises Pause")
	}
	m.runtime.snapshot.Connections.Connections = nil
	if hintHasKey(m.contextHelp(), "x") {
		t.Fatal("paused ended connection still advertises close")
	}
	m, _ = update(m, key("p"))
	m.runtime.snapshot.Connections = nil
	m.reconcileConnections()
	for _, unavailable := range []string{"Enter", "x", "p", "↑↓"} {
		if hintHasKey(m.contextHelp(), unavailable) {
			t.Fatalf("empty unavailable list still advertises %s", unavailable)
		}
	}
	m, first := archiveFixture(t, &fakeBackend{})
	m.connectionActivate("conn.closed")
	m.connections.selected = m.connectionRowKey(first)
	m.focus = "connection." + m.connections.selected
	if !hintHasKey(m.contextHelp(), "Enter") || hintHasKey(m.contextHelp(), "x") {
		t.Fatal("closed row did not offer read-only details")
	}
	m.openSelectedConnection()
	if hintHasKey(m.contextHelp(), "x") {
		t.Fatal("historical detail advertises close")
	}
}

func TestLogsHintsFollowEditingPauseAndFocusedControls(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := logsModel()
		m.locale = i18n.New(language)
		m, _ = update(m, key("/"))
		if m.contextHelp() != m.tr("navigation.logs.searching") {
			t.Fatal("logs search advertises list shortcuts")
		}
		m, _ = update(m, key("v"))
		if m.logs.search.Value() != "v" || m.logs.singleLine {
			t.Fatal("typing v in search toggled wrapping or lost the character")
		}
		m, _ = update(m, special(tea.KeyEnter))
		if m.logs.editing || m.focus != "logs.list" || hintHasKey(m.contextHelp(), "Enter") {
			t.Fatal("log list advertises a nonexistent Enter action")
		}
		if hintHasKey(m.contextHelp(), "↑↓") || hintHasKey(m.contextHelp(), "End") {
			t.Fatal("empty logs advertise browsing nonexistent entries")
		}
		m, _ = update(m, key("f"))
		if !m.logs.paused || !strings.Contains(m.contextHelp(), m.tr("navigation.logs.resume")) || strings.Contains(m.contextHelp(), m.tr("navigation.logs.pause")) {
			t.Fatal("paused logs still advertise Pause")
		}
		m.focus = "logs.follow"
		if !strings.HasPrefix(m.contextHelp(), m.tr("navigation.logs.action.resume")) {
			t.Fatal("follow button did not advertise Resume")
		}
		m, _ = update(m, special(tea.KeyEnter))
		if m.logs.paused {
			t.Fatal("Enter Resume did not resume the logs")
		}
		m.focus, m.logs.level = "logs.level", 0
		if !strings.HasPrefix(m.contextHelp(), m.tr("navigation.logs.action.level")) {
			t.Fatal("level control did not advertise its selector")
		}
		m, _ = update(m, special(tea.KeyEnter))
		if !m.menu.open || m.logs.level != 0 {
			t.Fatal("Enter did not open the advertised level selector")
		}
		m, _ = update(m, special(tea.KeyDown))
		m, _ = update(m, special(tea.KeyEnter))
		if m.menu.open || m.logs.level != 1 {
			t.Fatal("Enter did not commit the chosen filter")
		}
	}
}
