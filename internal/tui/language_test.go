package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func languageModel(t *testing.T, b *fakeBackend) Model {
	t.Helper()
	t.Setenv("LAZYCLASH_LANG", "")
	t.Setenv("LC_ALL", "C")
	m := ready(b)
	m.width, m.height, m.page, m.focus = 120, 32, "settings", "settings.language"
	m.layout()
	return m
}

func chooseChineseLanguage(m Model) (Model, tea.Cmd) {
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, special(tea.KeyDown))
	return update(m, special(tea.KeyEnter))
}

func languageSaveResult(t *testing.T, cmd tea.Cmd) languageSavedMsg {
	t.Helper()
	for cmd != nil {
		switch msg := cmd().(type) {
		case languageSavedMsg:
			return msg
		case tea.BatchMsg:
			cmd = msg[0]
		default:
			t.Fatalf("expected language save result, got %T", msg)
		}
	}
	t.Fatal("missing language save command")
	return languageSavedMsg{}
}

func TestSettingsLanguageSwitchPersistsBeforeChangingVisibleLanguage(t *testing.T) {
	b := &fakeBackend{}
	m := languageModel(t, b)
	m, cmd := chooseChineseLanguage(m)
	if !m.busy || m.menu.open || m.locale.Language() != i18n.English {
		t.Fatal("language changed before persistence completed")
	}
	m, _ = update(m, languageSaveResult(t, cmd))
	if m.busy || b.prefs.Language != i18n.Chinese || m.locale.Language() != i18n.Chinese {
		t.Fatal("language was not saved and applied")
	}
	plain := ansi.Strip(m.screen)
	if !strings.Contains(plain, "简体中文 ▾") || !strings.Contains(plain, "概览") || m.focus != "settings.language" {
		t.Fatalf("settings did not redraw in Chinese:\n%s", plain)
	}
	if m.logs.search.Placeholder == "Message or level" || !strings.Contains(m.profilesUI.url.Placeholder, "订阅") {
		t.Fatal("input placeholders did not follow locale")
	}
	m, _ = click(m, findTarget(t, m, "settings.language"))
	if !m.menu.open || m.menu.selected != 1 || !m.menu.items[1].Checked {
		t.Fatal("reopening the language menu did not select the saved Chinese preference")
	}
	m, cmd = update(m, menuMouse(m.menu, 0, tea.MouseLeft))
	m, _ = update(m, languageSaveResult(t, cmd))
	if m.locale.Language() != i18n.English || b.prefs.Language != i18n.English || !strings.Contains(ansi.Strip(m.screen), "Settings") {
		t.Fatalf("switch back to English failed: language=%s stored=%s busy=%t", m.locale.Language(), b.prefs.Language, m.busy)
	}
}

func TestLanguageMenuCancelAndReselectSavedLanguage(t *testing.T) {
	b := &fakeBackend{prefs: app.Preferences{Language: i18n.English}}
	m := languageModel(t, b)
	id := m.id
	m, _ = update(m, special(tea.KeyEnter))
	want := []menuItem{{ID: i18n.English, Label: "English", Checked: true}, {ID: i18n.Chinese, Label: "简体中文"}}
	if !m.menu.open || len(m.menu.items) != len(want) || m.menu.items[0] != want[0] || m.menu.items[1] != want[1] {
		t.Fatalf("unexpected options for English locale: %+v", m.menu.items)
	}
	m, _ = update(m, special(tea.KeyDown))
	if m.menu.selected != 1 || !m.menu.items[0].Checked || m.locale.Language() != i18n.English {
		t.Fatal("moving the menu cursor altered the current language")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.menu.open || m.busy || m.id != id || m.focus != "settings.language" {
		t.Fatal("cancelling the language dropdown started a save or lost focus")
	}
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, special(tea.KeyEnter))
	if m.menu.open || m.busy || m.id != id || m.locale.Language() != i18n.English {
		t.Fatal("reselecting the saved language started a redundant save")
	}
}

func TestLanguageMenuCanPersistDisplayedLanguageOverride(t *testing.T) {
	b := &fakeBackend{prefs: app.Preferences{Language: i18n.Chinese}}
	m := languageModel(t, b) // ready starts the model with an explicit English override.
	m, _ = update(m, special(tea.KeyEnter))
	m, cmd := update(m, special(tea.KeyEnter))
	if !m.busy || m.locale.Language() != i18n.English || b.prefs.Language != i18n.Chinese {
		t.Fatal("confirming the displayed override did not start a save")
	}
	m, _ = update(m, languageSaveResult(t, cmd))
	if m.busy || b.prefs.Language != i18n.English {
		t.Fatal("displayed English override was not saved")
	}
}

func TestSettingsLanguageFailureAndStaleResultPreserveCurrentLanguage(t *testing.T) {
	b := &fakeBackend{saveLanguage: func(context.Context, string) error { return errors.New("disk full") }}
	m := languageModel(t, b)
	m, cmd := chooseChineseLanguage(m)
	m, _ = update(m, languageSaveResult(t, cmd))
	if m.locale.Language() != i18n.English || m.busy || !strings.Contains(m.failure, "disk full") {
		t.Fatal("save failure changed locale or lost error")
	}
	m, _ = update(m, languageSavedMsg{id: m.id - 1, language: i18n.Chinese})
	if m.locale.Language() != i18n.English {
		t.Fatal("stale save changed language")
	}
}

func TestSavedLanguageLoadsAndExplicitLanguageOverridesIt(t *testing.T) {
	for _, explicit := range []string{"", "en"} {
		t.Run("explicit="+explicit, func(t *testing.T) {
			t.Setenv("LAZYCLASH_LANG", "")
			t.Setenv("LC_ALL", "C")
			b := &fakeBackend{prefs: app.Preferences{Language: i18n.Chinese}}
			m := New(t.Context(), b, explicit)
			msg := m.snapshotCmd(m.id, m.opCtx, true)()
			m, _ = update(m, msg)
			want := i18n.Chinese
			if explicit != "" {
				want = i18n.English
			}
			if m.locale.Language() != want {
				t.Fatalf("language=%s want=%s", m.locale.Language(), want)
			}
		})
	}
}

func TestLanguageSaveWaitsForCancellationBeforeQuit(t *testing.T) {
	b := &fakeBackend{saveLanguage: func(ctx context.Context, _ string) error { <-ctx.Done(); return ctx.Err() }}
	m := languageModel(t, b)
	m, cmd := chooseChineseLanguage(m)
	m, quit := update(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if quit != nil || !m.busy {
		t.Fatal("quit before language save reached its cancellation boundary")
	}
	m, quit = update(m, languageSaveResult(t, cmd))
	if m.busy || quit == nil || m.locale.Language() != i18n.English {
		t.Fatal("cancelled save switched language or prevented exit")
	}
}
