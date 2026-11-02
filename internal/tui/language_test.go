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
	m.width, m.height, m.page, m.focus = 120, 32, "settings", "language.zh-Hans"
	m.layout()
	return m
}

func TestSettingsLanguageSwitchPersistsBeforeChangingVisibleLanguage(t *testing.T) {
	b := &fakeBackend{}
	m := languageModel(t, b)
	m, cmd := click(m, findTarget(t, m, "language.zh-Hans"))
	if !m.busy || m.locale.Language() != i18n.English {
		t.Fatal("language changed before persistence completed")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || b.prefs.Language != i18n.Chinese || m.locale.Language() != i18n.Chinese {
		t.Fatal("language was not saved and applied")
	}
	plain := ansi.Strip(m.screen)
	if !strings.Contains(plain, "语言") || !strings.Contains(plain, "概览") {
		t.Fatalf("settings did not redraw in Chinese:\n%s", plain)
	}
	if m.logs.search.Placeholder == "Message or level" || !strings.Contains(m.profilesUI.url.Placeholder, "订阅") {
		t.Fatal("input placeholders did not follow locale")
	}
	m, cmd = click(m, findTarget(t, m, "language.en"))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.locale.Language() != i18n.English || b.prefs.Language != i18n.English || !strings.Contains(ansi.Strip(m.screen), "Settings") {
		t.Fatal("could not switch back to English")
	}
}

func TestSettingsLanguageFailureAndStaleResultPreserveCurrentLanguage(t *testing.T) {
	b := &fakeBackend{saveLanguage: func(context.Context, string) error { return errors.New("disk full") }}
	m := languageModel(t, b)
	m, cmd := click(m, findTarget(t, m, "language.zh-Hans"))
	m, _ = update(m, actionCmd(t, cmd)())
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
	m, cmd := click(m, findTarget(t, m, "language.zh-Hans"))
	save := actionCmd(t, cmd)
	m, quit := update(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if quit != nil || !m.busy {
		t.Fatal("quit before language save reached its cancellation boundary")
	}
	m, quit = update(m, save())
	if m.busy || quit == nil || m.locale.Language() != i18n.English {
		t.Fatal("cancelled save switched language or prevented exit")
	}
}
