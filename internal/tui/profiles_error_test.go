package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func TestInitialProfileReadFailureDoesNotPresentAnEmptyLibrary(t *testing.T) {
	b := &fakeBackend{snapshotErr: errors.New("invalid configuration library; preserving it for inspection")}
	m := ready(b)
	m.page = "profiles"
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	view := ansi.Strip(m.View().Content)
	for _, absent := range []string{"Configurations · 0 saved", "Built-in · No subscription", "Mihomo not installed", "Paste subscription URL or local path"} {
		if strings.Contains(view, absent) {
			t.Fatalf("unreadable snapshot was presented as known state: %q\n%s", absent, view)
		}
	}
	for _, present := range []string{"Profiles unavailable", "invalid configuration library", "[ Retry ]"} {
		if !strings.Contains(view, present) {
			t.Fatalf("missing read failure feedback %q\n%s", present, view)
		}
	}
	findTarget(t, m, "page.refresh")

	cmd := m.activate("page.refresh")
	m.layout()
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Loading configurations") || strings.Contains(view, "0 saved") {
		t.Fatal("retry presented an empty library before a successful read")
	}
	b.snapshotErr = nil
	b.snapshot = app.Snapshot{Profiles: []string{"default"}, ProfileDetails: profileFixtures("default")}
	m, _ = update(m, actionCmd(t, cmd)())
	view = ansi.Strip(m.View().Content)
	if !m.snapshotReady || strings.Contains(view, "Profiles unavailable") || !strings.Contains(view, "Configurations · 0 saved") || !strings.Contains(view, "default") {
		t.Fatal("successful retry did not show the loaded library", view)
	}
}

func TestProfileRefreshFailureRetainsLoadedCardsAndState(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{
		Installed: true, Version: "known", Applied: "work", AppliedName: "My Profile",
		Profiles: []string{"default", "work"},
		ProfileDetails: []app.ProfileInfo{
			{ID: "default", Name: "default", Validated: true},
			{ID: "work", Name: "My Profile", Validated: true},
		},
	}}
	m := ready(b)
	m.page = "profiles"
	m.width, m.height = 120, 32
	b.snapshotErr = errors.New("read temporarily failed")
	cmd := m.refresh()
	m, _ = update(m, actionCmd(t, cmd)())
	view := ansi.Strip(m.View().Content)
	if !m.snapshotReady || !strings.Contains(view, "My Profile") || m.snapshot.Version != "known" || !strings.Contains(view, "read temporarily failed") || strings.Contains(view, "Profiles unavailable") || strings.Contains(view, "State unavailable") {
		t.Fatal("refresh failure discarded a successful snapshot", view)
	}
}
