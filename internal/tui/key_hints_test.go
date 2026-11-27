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

func finishKeyHintsSave(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a preference save")
	}
	msg := cmd()
	for {
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			break
		}
		msg = batch[0]()
	}
	saved, ok := msg.(keyHintsSavedMsg)
	if !ok {
		t.Fatalf("expected the preference result, got %T", msg)
	}
	m, _ = update(m, saved)
	return m
}

func TestKeyHintsSwitchWaitsForSaveAndRejectsRepeatedInputs(t *testing.T) {
	b := &keyHintsBackend{fakeBackend: &fakeBackend{}}
	m := ready(b.fakeBackend)
	m.backend = b
	m.page, m.focus, m.width, m.height = "settings", "key-hints.toggle", 160, 45
	m.layout()
	hit := findTarget(t, m, m.focus)
	m, first := click(m, hit)
	if m.keyHints.hidden || !m.keyHints.pending || m.busy || hasAction(m.targets, hit.ID) {
		t.Fatal("the pending switch must retain the visible footer and prevent another click")
	}
	m, _ = update(m, key("?"))
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = click(m, hit)
	if m.keyHints.generation != 1 || !m.keyHints.pending || m.keyHints.hidden {
		t.Fatal("pending save accepted another input")
	}
	m, _ = update(m, keyHintsSavedMsg{generation: 0, hidden: true})
	if m.keyHints.hidden || !m.keyHints.pending {
		t.Fatal("stale completion changed a pending switch")
	}
	m = finishKeyHintsSave(t, m, first)
	if m.keyHints.pending || !m.keyHints.hidden || !b.prefs.HideKeyHints {
		t.Fatal("the switch did not adopt the persisted preference")
	}
}

func TestKeyHintsSaveFailureReadsBackPreferenceAndPreservesOtherOperation(t *testing.T) {
	for _, published := range []bool{false, true} {
		b := &keyHintsBackend{fakeBackend: &fakeBackend{}}
		b.prefs.HideKeyHints = true
		m := ready(b.fakeBackend)
		m.backend = b
		b.saveHints = func(_ context.Context, hidden bool) error {
			if published {
				b.prefs.HideKeyHints = hidden
			}
			return errors.New("disk full")
		}
		m.busy, m.job, m.message = true, "action", "Updating configuration"
		operation := m.notification.operation
		cmd := m.saveKeyHints(false)
		if !m.keyHints.hidden || !m.keyHints.pending {
			t.Fatal("unconfirmed preference changed the footer")
		}
		m.acceptKeyHintsSaved(cmd().(keyHintsSavedMsg))
		if m.keyHints.hidden != b.prefs.HideKeyHints || m.keyHints.pending || !m.busy || m.job != "action" || m.message != "Updating configuration" || m.failure != "" || m.notification.operation != operation {
			t.Fatal("failed preference did not reflect storage or disturbed the foreground operation")
		}
		found := false
		for _, item := range m.notification.items {
			found = found || (item.Key == "key-hints.save" && strings.Contains(item.Message, "disk full"))
		}
		if !found {
			t.Fatal("preference persistence failure was silent")
		}
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
		var save tea.Cmd
		m, save = update(m, key(button))
		m = finishKeyHintsSave(t, m, save)
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
				if m.page != page || m.focus != focus || m.keyHints.hidden == (button == "?") || !m.keyHints.pending || save == nil {
					t.Fatalf("%s: page=%s focus=%s hidden=%t", button, m.page, m.focus, m.keyHints.hidden)
				}
				m, _ = update(m, save())
				if m.keyHints.hidden != (button == "?") || b.prefs.HideKeyHints != m.keyHints.hidden || m.keyHints.pending {
					t.Fatalf("%s did not finish saving footer visibility", button)
				}
				assertCanvas(t, m.screen, m.width, m.height)
				assertRegions(t, m.targets, m.width, m.height)
			}
		})
	}
}

func TestKeyHintsWarningKeepsOriginAcrossPageChanges(t *testing.T) {
	m := ready(&fakeBackend{})
	m.backend = &keyHintsBackend{fakeBackend: &fakeBackend{}, saveHints: func(context.Context, bool) error { return errors.New("disk full") }}
	first := m.saveKeyHints(true)
	m.navigate("profiles")
	m.saveKeyHints(true)
	m.acceptKeyHintsSaved(first().(keyHintsSavedMsg))
	if len(m.notification.items) != 1 || m.notification.items[0].Page != "overview" || m.notification.items[0].Kind != "warning" {
		t.Fatalf("warning lost the page originating the save: %+v", m.notification.items)
	}
}

func TestRepeatedKeyHintsSaveFailureResolvesOnSuccess(t *testing.T) {
	m := noticeModel()
	saveErr := errors.New("disk full")
	m.backend = &keyHintsBackend{fakeBackend: &fakeBackend{}, saveHints: func(context.Context, bool) error { return saveErr }}
	m.begin("Other task", "Finished", func(context.Context) error { return nil })
	op := m.notification.operation
	toggle := func() { m.acceptKeyHintsSaved(m.saveKeyHints(!m.keyHints.hidden)().(keyHintsSavedMsg)) }
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
