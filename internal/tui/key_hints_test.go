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
		t.Fatal("missing hint preference save")
	}
	msg := cmd()
	for {
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			break
		}
		msg = batch[0]()
	}
	if _, ok := msg.(keyHintsSavedMsg); !ok {
		t.Fatalf("expected hint preference completion, got %T", msg)
	}
	m, _ = update(m, msg)
	return m
}

func TestKeyHintsWaitForSaveAndIgnoreDuplicateActivation(t *testing.T) {
	m := ready(&fakeBackend{})
	first := m.toggleKeyHints()
	if m.keyHints.hidden || !m.keyHints.pending || m.busy || first == nil {
		t.Fatal("preference change must wait for confirmation independently of the busy task")
	}
	if m.toggleKeyHints() != nil || m.keyHints.hidden {
		t.Fatal("a pending switch accepted a duplicate activation")
	}
	m.acceptKeyHintsSaved(keyHintsSavedMsg{generation: m.keyHints.generation - 1, hidden: true})
	if m.keyHints.hidden || !m.keyHints.pending {
		t.Fatal("stale completion changed the current operation")
	}
	m = finishKeyHintsSave(t, m, first)
	if !m.keyHints.hidden || m.keyHints.pending || m.busy {
		t.Fatal("confirmed preference did not apply")
	}
	m.loadKeyHintsPreference(false)
	if !m.keyHints.hidden {
		t.Fatal("older startup read replaced the confirmed choice")
	}
}

func TestKeyHintsFailedSaveKeepsActualPreferenceAndExistingOperation(t *testing.T) {
	m := ready(&fakeBackend{})
	m.loadKeyHintsPreference(true)
	m.backend = &keyHintsBackend{fakeBackend: &fakeBackend{}, saveHints: func(context.Context, bool) error { return errors.New("disk full") }}
	m.busy, m.job, m.message, m.failure = true, "action", "Updating configuration", ""
	operation := m.notification.operation
	cmd := m.toggleKeyHints()
	m.acceptKeyHintsSaved(cmd().(keyHintsSavedMsg))
	if !m.keyHints.hidden || m.keyHints.pending || !m.busy || m.job != "action" || m.message != "Updating configuration" || m.failure != "" || m.notification.operation != operation {
		t.Fatal("failed preference save changed the previous value or disturbed the current task")
	}
	for _, item := range m.notification.items {
		if item.Key == "key-hints.save" && strings.Contains(item.Message, "disk full") {
			return
		}
	}
	t.Fatal("preference persistence failure was silent")
}

func TestKeyHintsQuitWaitsForFinalPreferenceSave(t *testing.T) {
	m := ready(&fakeBackend{})
	b := &keyHintsBackend{fakeBackend: &fakeBackend{}}
	m.backend = b
	m, save := update(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	m, quit := update(m, key("q"))
	if quit != nil || !m.quitting || !m.keyHints.pending {
		t.Fatal("quit discarded an outstanding display preference save")
	}
	m, quit = update(m, save())
	if quit == nil || m.keyHints.pending || !b.prefs.HideKeyHints {
		t.Fatal("final preference save did not persist the choice and release quit")
	}
}

func TestKeyHintsShortcutBelongsToPagesDrawerAndHelpButNotInputs(t *testing.T) {
	ctrlT := tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}
	for _, state := range []string{"page", "drawer", "help", "profile-input", "search", "form", "wizard", "menu"} {
		t.Run(state, func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.backend = &keyHintsBackend{fakeBackend: &fakeBackend{}}
			want := false
			switch state {
			case "page":
				want = true
			case "drawer":
				m.sidebarOpen, want = true, true
			case "help":
				m.helpOpen, want = true, true
			case "profile-input":
				m.page, m.focus = "profiles", "profiles.url"
			case "search":
				m.page, m.connections.editing = "connections", true
			case "form":
				m.form = "source"
			case "wizard":
				m.wizard.open = true
			case "menu":
				m.menu.open = true
			}
			handled, _ := m.keyHintsShortcut(ctrlT)
			if handled != want || m.keyHints.pending != want || m.keyHints.hidden {
				t.Fatalf("shortcut handling=%v hidden=%v, want %v", handled, m.keyHints.hidden, want)
			}
		})
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
			if hasAction(targets, "help.open") {
				t.Fatal("hiding the footer restored a header help button")
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

func TestHelpCanRestoreHintsWithMouseAfterKeyboardOpen(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {60, 20}, {20, 7}, {10, 4}, {1, 1}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.backend = &keyHintsBackend{fakeBackend: &fakeBackend{}}
			m.width, m.height, m.busy, m.keyHints.hidden = size[0], size[1], true, true
			focus := m.focus
			m, _ = update(m, key("?"))
			if !m.helpOpen {
				t.Fatal("keyboard shortcut could not open help with hints hidden")
			}
			if size[1] >= 3 {
				toggle := findTarget(t, m, "key-hints.toggle")
				save := m.helpMouse(tea.MouseClickMsg{X: toggle.X, Y: toggle.Y, Button: tea.MouseLeft})
				m = finishKeyHintsSave(t, m, save)
				if m.keyHints.hidden || !m.helpOpen || !m.busy {
					t.Fatal("mouse recovery did not apply without closing help or disturbing the task")
				}
			}
			close := findTarget(t, m, "help.close")
			m.helpMouse(tea.MouseClickMsg{X: close.X, Y: close.Y, Button: tea.MouseLeft})
			if m.helpOpen || m.focus != focus {
				t.Fatal("mouse could not close help or previous focus was lost")
			}
			assertCanvas(t, m.screen, m.width, m.height)
			assertRegions(t, m.targets, m.width, m.height)
		})
	}
}
