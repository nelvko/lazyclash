package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestSettingsStartPageMenu(t *testing.T) {
	const trigger = "settings.startPage"
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	m := ready(b)
	directionCleanup(t, &m)
	m.width, m.height, m.page, m.focus = 80, 24, "settings", "settings.language"
	m.locale = i18n.New(i18n.Chinese)
	m.layout()
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != trigger || !strings.Contains(m.contextHelp(), m.tr("settings.chooseStartPage")) {
		t.Fatal("expected start page selector after the language selector")
	}
	m, _ = update(m, special(tea.KeyEnter))
	pages := app.StartPages()
	if !m.menu.open || len(m.menu.items) != len(pages) {
		t.Fatalf("wrong number of start pages: %+v", m.menu.items)
	}
	for i, page := range pages {
		want := menuItem{ID: page, Label: pageTitle(page, m.locale), Checked: page == app.StartPageOverview}
		if got := m.menu.items[i]; got != want {
			t.Fatalf("option %d = %+v, want %+v", i, got, want)
		}
	}
	id := m.id
	m, _ = update(m, special(tea.KeyDown))
	m, _ = update(m, special(tea.KeyEscape))
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, special(tea.KeyEnter))
	if m.menu.open || m.busy || m.id != id || m.focus != trigger {
		t.Fatal("cancelling and reselecting the default should leave the preference alone")
	}

	// A failed write leaves the saved selection checked so the user can retry.
	b.saveStartPage = func(context.Context, string) error { return errors.New("disk full") }
	for attempt := range 2 {
		m, _ = click(m, findTarget(t, m, trigger))
		if !m.menu.items[0].Checked {
			t.Fatalf("default page no longer checked on attempt %d", attempt+1)
		}
		var cmd tea.Cmd
		m, cmd = update(m, menuMouse(m.menu, 1, tea.MouseLeft))
		if !m.busy || m.menu.open || m.navigationOptions().settingsDropdown(trigger).Value != app.StartPageOverview {
			t.Fatal("start page changed before the save finished")
		}
		var result tea.Msg = cmd()
		for batch, ok := result.(tea.BatchMsg); ok; batch, ok = result.(tea.BatchMsg) {
			result = batch[0]()
		}
		m, _ = update(m, result)
		if attempt == 0 {
			if !strings.Contains(m.failure, "disk full") || m.busy {
				t.Fatal("failed start page save lost its error")
			}
			b.saveStartPage = nil
		}
	}
	m, _ = update(m, startPageSavedMsg{id: id, page: "rules"})
	m, _ = update(m, special(tea.KeyEnter))
	if m.page != "settings" || m.startPage != "proxies" || b.prefs.StartPage != m.startPage || !m.menu.items[1].Checked {
		t.Fatal("saved start page should be checked without navigating away")
	}
}
