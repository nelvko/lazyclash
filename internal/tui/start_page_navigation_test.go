package tui

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

func TestStartPageEntersSelectedPage(t *testing.T) {
	for i, page := range app.StartPages() {
		t.Run(page, func(t *testing.T) {
			b := &fakeBackend{snapshot: settingsTestSnapshot(), prefs: app.Preferences{StartPage: page}}
			m := New(t.Context(), b, "en")
			directionCleanup(t, &m)
			if m.page != page {
				t.Fatalf("first frame opened %s, want %s", m.page, page)
			}
			m, _ = update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
			m, _ = update(m, m.snapshotCmd(m.id, m.opCtx, true)())
			if m.page != page || m.startPage != page || !m.runtime.active || (page == "rules") != m.rules.pending {
				t.Fatalf("startup page=%s saved=%s runtime=%t rules pending=%t", m.page, m.startPage, m.runtime.active, m.rules.pending)
			}
			// The configured destination should have the same focus as a normal visit.
			focus := m.focus
			m, _ = update(m, key(strconv.Itoa(i+1)))
			if m.focus != focus {
				t.Fatalf("startup focus=%s, navigation focus=%s", focus, m.focus)
			}
		})
	}
}

func TestStartPageRespectsNavigationDuringInitialSnapshot(t *testing.T) {
	for _, target := range []struct{ key, page string }{{"1", "overview"}, {"7", "settings"}} {
		t.Run(target.page, func(t *testing.T) {
			b := &fakeBackend{snapshot: settingsTestSnapshot(), prefs: app.Preferences{StartPage: "rules"}}
			m := New(t.Context(), b, "en")
			directionCleanup(t, &m)
			initial := m.snapshotCmd(m.id, m.opCtx, true)()
			m, _ = update(m, key(target.key))
			m, _ = update(m, initial)
			if m.page != target.page || m.startPage != "rules" || m.rules.active {
				t.Fatalf("late startup changed page: page=%s preference=%s", m.page, m.startPage)
			}
		})
	}
}

func TestStartPageBeforeInstallKeepsGuideOnOverview(t *testing.T) {
	b := &fakeBackend{prefs: app.Preferences{StartPage: "profiles"}}
	m := New(t.Context(), b, "en")
	directionCleanup(t, &m)
	m, _ = update(m, m.snapshotCmd(m.id, m.opCtx, true)())
	if m.page != "profiles" || m.focus != "profiles.create" || m.wizard.inline || m.busy {
		t.Fatalf("start before install: page=%s focus=%s job=%s", m.page, m.focus, m.job)
	}
	m, _ = update(m, key("1"))
	if m.page != "overview" || !m.wizard.inline {
		t.Fatal("returning to Overview did not show the installation guide")
	}
}

func TestStartPageUnreadablePreferenceFallsBackToOverview(t *testing.T) {
	b := &fakeBackend{
		snapshot: settingsTestSnapshot(),
		prefs:    app.Preferences{StartPage: "logs"},
		prefsErr: errors.New("unreadable preferences"),
	}
	m := New(t.Context(), b, "en")
	directionCleanup(t, &m)
	m, _ = update(m, m.snapshotCmd(m.id, m.opCtx, true)())
	if m.page != app.StartPageOverview || !strings.Contains(m.failure, b.prefsErr.Error()) {
		t.Fatalf("page=%s, failure=%q", m.page, m.failure)
	}
}
