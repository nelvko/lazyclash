package tui

import (
	"context"
	"errors"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

func TestSavedThemeResolvesTerminalBackgroundOnlyInAuto(t *testing.T) {
	for _, theme := range []string{app.ThemeLight, app.ThemeDark, app.ThemeAuto} {
		t.Run(theme, func(t *testing.T) {
			m := ready(&fakeBackend{prefs: app.Preferences{Theme: theme}})
			if m.theme != theme {
				t.Fatalf("loaded theme %q, want %q", m.theme, theme)
			}
			for _, tc := range []struct {
				background color.Color
				auto       string
			}{{nil, app.ThemeDark}, {color.White, app.ThemeLight}, {color.Black, app.ThemeDark}} {
				m, _ = update(m, tea.BackgroundColorMsg{Color: tc.background})
				want := theme
				if theme == app.ThemeAuto {
					want = tc.auto
				}
				if m.theme != theme || m.effectiveTheme() != want {
					t.Fatalf("preference %q resolved to %q, want %q", m.theme, m.effectiveTheme(), want)
				}
			}
		})
	}
}

func TestThemeSaveFailureAndStaleResultKeepCurrentPreference(t *testing.T) {
	b := &fakeBackend{saveTheme: func(context.Context, string) error { return errors.New("disk full") }}
	m := ready(b)
	m.page = "settings"
	cmd := m.changeTheme(app.ThemeAuto)
	m, _ = update(m, themeSavedMsg{id: m.id - 1, theme: app.ThemeLight})
	if !m.busy || m.theme != app.ThemeDark {
		t.Fatal("stale result finished the pending save or changed the theme")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.theme != app.ThemeDark || !strings.Contains(m.failure, "disk full") {
		t.Fatalf("failed save left busy=%v, theme=%q, error=%q", m.busy, m.theme, m.failure)
	}
}
