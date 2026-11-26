package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type keyHintsBackend struct {
	*fakeBackend
	saveHints func(context.Context, bool) error
}

func (b *keyHintsBackend) SaveKeyHintsPreference(ctx context.Context, hidden bool) error {
	if b.saveHints != nil {
		return b.saveHints(ctx, hidden)
	}
	b.prefs.HideKeyHints = hidden
	return ctx.Err()
}

func TestKeyHintsSaveCoalescesRapidChoicesWithoutBlockingNavigation(t *testing.T) {
	m := ready(&fakeBackend{})
	b := &keyHintsBackend{fakeBackend: &fakeBackend{}}
	var saved []bool
	b.saveHints = func(_ context.Context, hidden bool) error {
		saved = append(saved, hidden)
		return nil
	}
	m.backend = b
	first := m.toggleKeyHints()
	if !m.keyHints.hidden || m.busy || first == nil {
		t.Fatal("toggle did not immediately apply independently of the busy task")
	}
	if m.toggleKeyHints() != nil || m.toggleKeyHints() != nil || m.toggleKeyHints() != nil {
		t.Fatal("parallel saves were started while one was pending")
	}
	if m.keyHints.hidden {
		t.Fatal("last requested choice did not win immediately")
	}
	last := m.acceptKeyHintsSaved(first().(keyHintsSavedMsg))
	if last == nil || len(saved) != 1 || !saved[0] {
		t.Fatal("first completion did not enqueue the final choice")
	}
	if m.acceptKeyHintsSaved(last().(keyHintsSavedMsg)) != nil || len(saved) != 2 || saved[1] || m.keyHints.pending || m.busy {
		t.Fatal("coalesced persistence did not finish with the latest choice")
	}
}

func TestKeyHintsStartupAndFailureKeepSessionChoiceAndExistingOperation(t *testing.T) {
	m := ready(&fakeBackend{})
	m.loadKeyHintsPreference(true)
	if !m.keyHints.hidden {
		t.Fatal("saved choice did not load")
	}
	m.backend = &keyHintsBackend{fakeBackend: &fakeBackend{}, saveHints: func(context.Context, bool) error { return errors.New("disk full") }}
	m.busy, m.job, m.message, m.failure = true, "action", "Updating configuration", ""
	operation := m.notification.operation
	cmd := m.toggleKeyHints()
	m.loadKeyHintsPreference(true)
	if m.keyHints.hidden {
		t.Fatal("late startup preference replaced the user's newer choice")
	}
	m.acceptKeyHintsSaved(cmd().(keyHintsSavedMsg))
	if m.keyHints.hidden || !m.busy || m.job != "action" || m.message != "Updating configuration" || m.failure != "" || m.notification.operation != operation {
		t.Fatal("preference failure disturbed the current operation or display choice")
	}
	found := false
	for _, item := range m.notification.items {
		found = found || (item.Key == "key-hints.save" && strings.Contains(item.Message, "disk full"))
	}
	if !found {
		t.Fatal("preference persistence failure was silent")
	}
}

func TestKeyHintsQuitWaitsForFinalPreferenceSave(t *testing.T) {
	m := ready(&fakeBackend{})
	b := &keyHintsBackend{fakeBackend: &fakeBackend{}}
	m.backend = b
	m, save := update(m, key("?"))
	m, quit := update(m, key("q"))
	if quit != nil || !m.quitting || !m.keyHints.pending {
		t.Fatal("quit discarded an outstanding display preference save")
	}
	m, quit = update(m, save())
	if quit == nil || m.keyHints.pending || !b.prefs.HideKeyHints {
		t.Fatal("final preference save did not persist the choice and release quit")
	}
}

func TestKeyHintsShortcutInDrawerKeepsNavigationFocus(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height, m.sidebarOpen, m.focus = 60, 20, true, "nav.overview"
	m.layout()
	for _, button := range []string{"?", "？"} {
		m, _ = update(m, key(button))
		if m.keyHints.hidden != (button == "?") || !m.sidebarOpen || m.focus != "nav.overview" {
			t.Fatalf("%s changed drawer focus or failed to toggle hints", button)
		}
	}
}

func TestHiddenKeyHintsReleaseLastRowAndProtectPopupTargets(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 20}, {80, 24}, {60, 10}, {20, 7}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			o := dashboardOptions{Width: size[0], Height: size[1], Page: "proxies", Help: "Shortcut bar"}
			shown := dashboardGeometry(o)
			o.HideKeyHints = true
			hidden := dashboardGeometry(o)
			if hidden.height != shown.height+1 || hidden.x != shown.x || hidden.width != shown.width || hidden.sidebar != shown.sidebar {
				t.Fatal("hiding hints did not release exactly one row with stable geometry")
			}
			o.Body = strings.Repeat("content\n", hidden.height-1) + "Last row"
			o.BodyTargets = []hitRegion{{ID: "last-row", Y: hidden.height - 1, W: 8, H: 1}}
			screen, targets, _ := renderDashboard(o)
			if !strings.Contains(ansi.Strip(strings.Split(screen, "\n")[size[1]-1]), "Last row") || strings.Contains(ansi.Strip(screen), "Shortcut bar") || !hasAction(targets, "last-row") {
				t.Fatal("released row was not usable")
			}
			assertCanvas(t, screen, o.Width, o.Height)
			assertRegions(t, targets, o.Width, o.Height)
			if !hidden.sidebar {
				o.SidebarOpen = true
				screen, targets, _ = renderDashboard(o)
				if hasAction(targets, "last-row") {
					t.Fatal("hidden footer let a mouse target pass through the popup's last row")
				}
				assertCanvas(t, screen, o.Width, o.Height)
				assertRegions(t, targets, o.Width, o.Height)
			}
		})
	}
}

func TestQuestionMarkTogglesAndPersistsKeyHintsInPlace(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {60, 20}, {20, 7}, {10, 4}, {1, 1}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			b := &fakeBackend{}
			m := ready(b)
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			page, focus := m.page, m.focus
			for _, button := range []string{"?", "？"} {
				var save tea.Cmd
				m, save = update(m, key(button))
				if m.page != page || m.focus != focus || m.keyHints.hidden != (button == "?") || save == nil {
					t.Fatalf("%s: page=%s focus=%s hidden=%t", button, m.page, m.focus, m.keyHints.hidden)
				}
				m, _ = update(m, save())
				if b.prefs.HideKeyHints != m.keyHints.hidden || m.keyHints.pending {
					t.Fatalf("%s did not finish saving footer visibility", button)
				}
				assertCanvas(t, m.screen, m.width, m.height)
				assertRegions(t, m.targets, m.width, m.height)
			}
		})
	}
}

func TestKeyHintsWarningFollowsLastToggleAcrossPageChanges(t *testing.T) {
	m := ready(&fakeBackend{})
	m.backend = &keyHintsBackend{fakeBackend: &fakeBackend{}, saveHints: func(context.Context, bool) error { return errors.New("disk full") }}
	first := m.toggleKeyHints()
	m.navigate("profiles")
	m.toggleKeyHints()
	m.navigate("logs")
	last := m.acceptKeyHintsSaved(first().(keyHintsSavedMsg))
	if last == nil {
		t.Fatal("second toggle did not queue a save")
	}
	m.acceptKeyHintsSaved(last().(keyHintsSavedMsg))
	if len(m.notification.items) != 1 || m.notification.items[0].Page != "profiles" || m.notification.items[0].Kind != "warning" {
		t.Fatalf("warning lost the originating page of the saved choice: %+v", m.notification.items)
	}
}

func TestRepeatedKeyHintsSaveFailureResolvesOnSuccess(t *testing.T) {
	m := noticeModel()
	saveErr := errors.New("disk full")
	m.backend = &keyHintsBackend{fakeBackend: &fakeBackend{}, saveHints: func(context.Context, bool) error { return saveErr }}
	m.begin("Other task", "Finished", func(context.Context) error { return nil })
	op := m.notification.operation
	toggle := func() { m.acceptKeyHintsSaved(m.toggleKeyHints()().(keyHintsSavedMsg)) }
	toggle()
	first := m.notification.items[0]
	m.navigate("logs")
	toggle()
	if len(m.notification.items) != 1 || m.notification.items[0].ID != first.ID || m.notification.items[0].Page != "logs" || m.notification.items[0].generation == first.generation {
		t.Fatalf("duplicate or stale warning: %+v", m.notification.items)
	}
	saveErr = nil
	toggle()
	if len(m.notification.items) != 0 || !m.busy || m.notification.operation != op {
		t.Fatal("preference recovery changed the foreground task or left a warning")
	}
}
