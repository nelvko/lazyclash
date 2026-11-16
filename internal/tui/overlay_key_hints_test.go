package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestExclusiveKeyHintsExplainHelpRequiresEscape(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, kind := range []string{"menu", "confirmation"} {
			m, _ := profileClickModel(t, true, true, false, false)
			if kind == "menu" {
				m = profileMenuRightClick(t, m, "profile.1")
			} else {
				m = connectionsModel(t, &fakeBackend{})
				m.connectionActivate("conn.close-all")
			}
			m.locale = i18n.New(language)
			m.layout()
			footer := ansi.Strip(strings.Split(m.screen, "\n")[m.height-1])
			if !strings.Contains(footer, m.tr("dashboard.footer.helpAfterClose")) || strings.Contains(footer, m.tr("dashboard.footer.quit")) {
				t.Fatalf("%s advertised unavailable global shortcuts: %s", kind, footer)
			}
			m, _ = update(m, key("?"))
			m, _ = update(m, key("q"))
			if m.helpOpen || m.quitting || kind == "menu" && !m.menu.open || kind == "confirmation" && !m.connections.confirming {
				t.Fatal("exclusive input did not retain ? and q")
			}
			m, _ = update(m, special(tea.KeyEscape))
			m, _ = update(m, key("?"))
			if !m.helpOpen {
				t.Fatal("Esc then ? did not open help")
			}
		}
	}
}

func TestMenusWithoutEnabledActionsDoNotAdvertiseEnter(t *testing.T) {
	m := ready(&fakeBackend{})
	m.menu.show([]menuItem{{ID: "disabled", DisabledReason: "unavailable"}}, 0, 0)
	if strings.Contains(m.contextHelp(), "Enter") || m.menu.key(special(tea.KeyEnter)).Action != "" {
		t.Fatal("disabled menu advertised or activated an unavailable action")
	}
}

func TestNarrowExclusiveKeyHintsDoNotAdvertiseBareHelp(t *testing.T) {
	for _, o := range []dashboardOptions{
		{Width: 12, EditingInput: true},
		{Width: 12, HelpAfterClose: true},
	} {
		if got := ansi.Strip(dashboardKeyHints(o)); got != "Esc" {
			t.Fatalf("narrow exclusive input advertised unavailable help: %q", got)
		}
	}
}

func TestBusyProfileInputDoesNotAdvertiseImport(t *testing.T) {
	m, _ := profileClickModel(t, true, true, false, false)
	m.focus, m.busy = "profiles.url", true
	if strings.Contains(m.contextHelp(), "Enter") {
		t.Fatal("busy URL field advertised an import it cannot accept")
	}
	if handled, _ := m.profileURLKey(special(tea.KeyEnter)); handled {
		t.Fatal("busy URL field unexpectedly handled import")
	}
	m.busy = false
	if m.contextHelp() != m.tr("navigation.profiles.importing") {
		t.Fatal("ready URL field did not restore its import hint")
	}
}

func TestNotificationHintsFollowTheFocusedAction(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := ready(&fakeBackend{})
		m.locale, m.busy = i18n.New(language), true
		for _, test := range []struct{ id, hint string }{
			{"notification.details.1", "details"}, {"notification.close.1", "dismiss"},
			{"notification.cancel.1", "cancel"}, {"notification.expand", "list"},
		} {
			m.focus = test.id
			if !strings.HasPrefix(m.contextHelp(), m.tr("notification.hints."+test.hint)) {
				t.Fatalf("notification action %s retained page or busy hints", test.id)
			}
		}
		var panel notificationPanel
		panel.show(0)
		panel.render([]notificationItem{{ID: 1, Kind: "working", Message: "fixture"}}, 120, 40, m.locale)
		for i, action := range panel.actions {
			panel.focus = i
			hint := "details"
			if strings.Contains(action.ID, ".dismiss.") {
				hint = "dismiss"
			} else if strings.Contains(action.ID, ".cancel.") {
				hint = "cancel"
			}
			if !strings.HasPrefix(panel.contextHelp(m.locale), m.tr("notification.hints."+hint)) || panel.key(special(tea.KeyEnter)) != action.ID {
				t.Fatalf("notification panel Enter hint disagrees with %s", action.ID)
			}
		}
		panel.render(nil, 120, 40, m.locale)
		if strings.Contains(panel.contextHelp(m.locale), "Enter") {
			t.Fatal("empty notification panel advertised details")
		}
	}
}

func TestPortsHintsDistinguishCancelReviewAndApply(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	for _, id := range []string{"ports.mixed", "ports.controller", "ports.review", "ports.cancel"} {
		m.ports.focus = id
		label := m.tr("ports.review")
		if id == "ports.cancel" {
			label = m.tr("ports.cancel")
		}
		if !strings.HasPrefix(m.contextHelp(), "Enter "+label+" · ") {
			t.Fatalf("%s advertises a different action: %s", id, m.contextHelp())
		}
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.ports.open || m.busy {
		t.Fatal("Enter on cancel did not just close the port dialog")
	}
	m = reviewPorts(t, portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}}))
	m.ports.focus = "ports.back"
	if !strings.HasPrefix(m.contextHelp(), "Enter "+m.tr("ports.back")) {
		t.Fatal("port review back button advertised applying changes")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.ports.step != "edit" || m.busy {
		t.Fatal("back button applied instead of returning to edit")
	}
}

func TestHelpCoreShortcutsMatchTheCurrentPage(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, test := range []struct {
			page, actions string
			refresh       bool
		}{
			{"overview", "help.core.overview", true},
			{"proxies", "help.core.proxies", false},
			{"profiles", "help.core.actions", true},
			{"connections", "help.hints.connectionsCore", false},
			{"rules", "help.core.actions", false},
			{"logs", "help.core.actions", false},
			{"settings", "help.core.actions", true},
		} {
			m := Model{locale: i18n.New(language), page: test.page}
			body := strings.Join(m.helpLines(1000), "\n")
			for _, actions := range []string{"help.core.actions", "help.core.overview", "help.core.proxies", "help.hints.connectionsCore"} {
				if strings.Contains(body, m.tr(actions)) != (actions == test.actions) {
					t.Fatalf("%s %s included the wrong core shortcut set: %s", language, test.page, actions)
				}
			}
			if strings.Contains(body, m.tr("help.core.refresh")) != test.refresh {
				t.Fatalf("%s %s advertised the wrong refresh behavior", language, test.page)
			}
		}
	}
}
