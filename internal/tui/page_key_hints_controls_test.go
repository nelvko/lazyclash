package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestProfilesHintsFollowInputAndDetailActions(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Profiles: []string{"work"}, ProfileDetails: profileFixtures("default", "work")}})
		m.locale, m.page, m.width, m.height = i18n.New(language), "profiles", 160, 45
		m.focus = "profiles.url"
		if m.contextHelp() != m.tr("navigation.profiles.inputEmpty") {
			t.Fatal("profile input retained card shortcuts")
		}
		m, _ = update(m, key("m"))
		if m.profilesUI.url.Value() != "m" || m.menu.open {
			t.Fatal("menu hint does not match text input ownership")
		}
		m, _ = update(m, special(tea.KeyEscape))
		if m.focus != "profiles.add" || !strings.Contains(m.contextHelp(), "Enter "+m.tr("profiles.add")) || strings.Contains(m.contextHelp(), "↑↓") {
			t.Fatal("leaving input did not show the add action")
		}
		m.focus = "profile.0"
		if m.contextHelp() != m.tr("navigation.profiles") {
			t.Fatal("profile card did not advertise settings and menu")
		}
		m = openSettingsForTest(t, m, "work")
		if m.contextHelp() != m.tr("profile_settings.footer") {
			t.Fatal("settings advertised card shortcuts")
		}
		m, _ = update(m, special(tea.KeyEscape))
		if m.form != "" || m.focus != "profile.0" || m.contextHelp() != m.tr("navigation.profiles") {
			t.Fatal("closing settings did not restore the card hint")
		}
	}
}

func TestOverviewHintsHideActionsOnReadOnlyCardsAndUnavailableToggle(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{80, 160} {
			m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
			m.locale, m.page, m.width, m.height = i18n.New(language), "overview", width, 45
			for _, card := range []string{"dashboard.traffic.title", "dashboard.system.title"} {
				m.focus = "overview.card." + card
				m.layout()
				if strings.Contains(m.contextHelp(), "Enter") {
					t.Fatal("read-only Overview card advertised an activation")
				}
				focus := m.focus
				m, _ = update(m, special(tea.KeyEnter))
				if m.focus != focus || m.busy {
					t.Fatal("read-only card unexpectedly activated a control")
				}
			}
			m.focus = "overview.card.dashboard.profile.title"
			if m.contextHelp() != m.tr("overview.cardHint") {
				t.Fatal("interactive card lost its enter-controls hint")
			}
			m, _ = update(m, special(tea.KeyEnter))
			if m.focus != "card.profiles" || !strings.Contains(m.contextHelp(), m.tr("navigation.controls.openProfiles")) {
				t.Fatal("entering profile card did not expose its page action")
			}
			m.focus = "core.toggle"
			for _, state := range []string{"activating", "deactivating", "unknown"} {
				m.snapshot.Service = service.Status{State: state}
				if strings.Contains(m.contextHelp(), "Enter") {
					t.Fatalf("%s toggle advertised activation", state)
				}
				m, _ = update(m, special(tea.KeyEnter))
				if m.busy {
					t.Fatal("unavailable toggle started an operation")
				}
			}
			for _, active := range []bool{false, true} {
				state, label := "inactive", "dashboard.settings.start"
				if active {
					state, label = "active", "dashboard.settings.stop"
				}
				m.snapshot.Service = service.Status{State: state, Active: active}
				if !strings.HasPrefix(m.contextHelp(), "Enter "+m.tr(label)+" · ") {
					t.Fatal("toggle hint does not follow its observed state")
				}
			}
		}
	}
}

func TestSettingsHintsNameTheFocusedControl(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
		m.locale, m.page, m.width, m.height = i18n.New(language), "settings", 160, 45
		for _, test := range []struct{ focus, label string }{
			{"settings.language", "settings.chooseLanguage"}, {"mode.direct", "navigation.controls.applyMode"},
			{"runtime-settings", "navigation.controls.editPorts"}, {"key-hints.toggle", "navigation.controls.hideHints"},
			{"install", "navigation.controls.manageCore"}, {autostartToggleTarget, "dashboard.settings.enableAutostart"},
		} {
			m.focus = test.focus
			if !strings.HasPrefix(m.contextHelp(), "Enter "+m.tr(test.label)+" · ") {
				t.Fatalf("%s hint does not name its action: %s", test.focus, m.contextHelp())
			}
		}
		m.focus = "key-hints.toggle"
		m, _ = update(m, special(tea.KeyEnter))
		if !m.keyHints.hidden || !strings.HasPrefix(m.contextHelp(), "Enter "+m.tr("navigation.controls.showHints")+" · ") {
			t.Fatal("key-hint switch did not describe the reverse action")
		}
		m.focus = "settings.language"
		m, _ = update(m, special(tea.KeyLeft))
		if m.focus != "nav.settings" {
			t.Fatal("settings Back hint disagrees with horizontal-key navigation")
		}
	}
}
