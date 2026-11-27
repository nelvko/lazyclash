package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func finishWebsiteEdit(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		switch msg := cmd().(type) {
		case websiteListSavedMsg:
			m, _ = update(m, msg)
			return m
		case tea.BatchMsg:
			cmd = msg[0]
		default:
			t.Fatalf("unexpected website save message %T", msg)
		}
	}
	t.Fatal("website editor did not submit")
	return m
}

func TestWebsiteEditorKeepsFailedDraftAndRestoresManageFocus(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		b := &fakeBackend{snapshot: settingsTestSnapshot()}
		got := app.Website{Name: "Status", URL: "status.example"}
		added := app.Website{Name: got.Name, URL: "https://status.example/"}
		calls := 0
		b.addWebsite = func(_ context.Context, site app.Website) ([]app.Website, error) {
			calls++
			if site != got {
				t.Fatalf("incorrect draft: %+v", site)
			}
			if calls == 1 {
				return nil, errors.New("save failed")
			}
			return append(app.DefaultWebsites(), added), nil
		}
		m := ready(b)
		directionCleanup(t, &m)
		m.locale, m.width, m.height = i18n.New(language), 40, 16
		m.activate("website.manage")
		if !m.menu.dropdownOpen("website.manage") {
			t.Fatal("website menu was unavailable with a stopped core")
		}
		m, _ = update(m, special(tea.KeyEnter))
		if m.form != "website-editor" || m.focus != "websiteEditor.address" || m.inputCursor() == nil {
			t.Fatalf("unexpected initial form focus %s/%s", m.form, m.focus)
		}
		m, _ = update(m, special(tea.KeyEnter))
		if m.focus != "websiteEditor.address" || !strings.Contains(ansi.Strip(m.screen), m.tr("websiteEditor.required")) {
			t.Fatal("blank address did not keep focus and explain the required input")
		}
		m, _ = update(m, tea.PasteMsg{Content: "q"})
		m, _ = click(m, findTarget(t, m, "websiteEditor.address.clear"))
		if m.websiteEditor.address.Value() != "" || m.quitting {
			t.Fatal("failed to clear pasted text")
		}
		m, _ = update(m, tea.PasteMsg{Content: got.URL})
		m, _ = update(m, special(tea.KeyEnter))
		m, _ = update(m, tea.PasteMsg{Content: got.Name})
		var cmd tea.Cmd
		m, cmd = click(m, findTarget(t, m, "websiteEditor.save"))
		m = finishWebsiteEdit(t, m, cmd)
		if m.form != "website-editor" || !strings.Contains(ansi.Strip(m.screen), "save failed") ||
			m.websiteEditor.address.Value() != got.URL || m.websiteEditor.name.Value() != got.Name || !slices.Equal(m.websites, app.DefaultWebsites()) {
			t.Fatal("failed save lost the draft or changed the website list")
		}
		m, cmd = click(m, findTarget(t, m, "websiteEditor.save"))
		m = finishWebsiteEdit(t, m, cmd)
		if m.form != "" || m.focus != "website.manage" || !hasAction(m.targets, m.focus) || !slices.Contains(m.websites, added) {
			t.Fatalf("successful edit did not return to Manage: %s/%s", m.form, m.focus)
		}
	}
}

func TestWebsiteEditorRemovalRequiresConfirmationAndSupportsLastSite(t *testing.T) {
	initial := app.DefaultWebsites()[:1]
	calls := 0
	b := &fakeBackend{snapshot: settingsTestSnapshot(), removeWebsite: func(_ context.Context, site app.Website) ([]app.Website, error) {
		if site != initial[0] {
			t.Fatalf("removed unexpected site %+v", site)
		}
		calls++
		return []app.Website{}, nil
	}}
	m := ready(b)
	directionCleanup(t, &m)
	m.width, m.height = 40, 16
	m.setWebsites(initial)
	for _, confirm := range []bool{false, true} {
		m.activate("website.manage")
		m, _ = update(m, special(tea.KeyDown))
		m, _ = update(m, special(tea.KeyEnter))
		if m.form != "website-editor" || m.focus != "websiteEditor.cancel" || !strings.Contains(ansi.Strip(m.screen), initial[0].URL) {
			t.Fatal("removal did not identify the website and default to cancel")
		}
		if confirm {
			m, _ = update(m, special(tea.KeyTab))
		}
		var cmd tea.Cmd
		m, cmd = update(m, special(tea.KeyEnter))
		if confirm {
			m = finishWebsiteEdit(t, m, cmd)
		} else if m.form != "" || !slices.Equal(m.websites, initial) || calls != 0 {
			t.Fatal("cancellation changed website list")
		}
	}
	if m.form != "" || m.websites == nil || len(m.websites) != 0 || calls != 1 {
		t.Fatal("removing last website did not leave a manageable empty list")
	}
	m.activate("website.manage")
	if !m.menu.open || len(m.menu.items) != 1 || m.menu.items[0].ID != "add" {
		t.Fatal("empty list lost add action")
	}
}

func TestWebsiteEditorRefreshesStaleRemoval(t *testing.T) {
	initial := app.DefaultWebsites()
	current := initial[1:]
	b := &fakeBackend{snapshot: settingsTestSnapshot(), removeWebsite: func(context.Context, app.Website) ([]app.Website, error) {
		return current, diagnostic.New("diagnostic.website.missing", "website was removed")
	}}
	m := ready(b)
	m.locale = i18n.New(i18n.Chinese)
	directionCleanup(t, &m)
	m.activate("website.manage")
	m, _ = update(m, special(tea.KeyDown))
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, special(tea.KeyTab))
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishWebsiteEdit(t, m, cmd)
	if m.form != "" || m.focus != "website.manage" || !slices.Equal(m.websites, current) {
		t.Fatal("stale confirmation did not restore the current list and focus")
	}
	if !slices.ContainsFunc(m.notification.items, func(item notificationItem) bool {
		return item.Kind == "warning" && item.Message == m.tr("diagnostic.website.missing")
	}) {
		t.Fatal("stale removal did not explain the changed list")
	}
	m.activate("website.manage")
	if len(m.menu.items) != len(current)+1 || m.menu.items[1].ID != current[0].URL {
		t.Fatal("reopened menu still offers the removed site")
	}
}

func TestWebsiteEditorCancelsPendingSave(t *testing.T) {
	for _, committed := range []bool{false, true} {
		entered := make(chan struct{})
		added := app.Website{Name: "New", URL: "https://status.example/"}
		b := &fakeBackend{snapshot: settingsTestSnapshot(), addWebsite: func(ctx context.Context, site app.Website) ([]app.Website, error) {
			close(entered)
			<-ctx.Done()
			if committed {
				return append(app.DefaultWebsites(), added), nil
			}
			return nil, ctx.Err()
		}}
		m := ready(b)
		directionCleanup(t, &m)
		m.activate("website.manage")
		m, _ = update(m, special(tea.KeyEnter))
		m.websiteEditor.address.SetValue(added.URL)
		cmd := m.websiteEditorAction("websiteEditor.save")
		done := make(chan tea.Msg, 1)
		go func() { done <- cmd() }()
		<-entered
		m, _ = update(m, special(tea.KeyEscape))
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(time.Second):
			t.Fatal("Escape did not cancel the blocked save")
		}
		m, _ = update(m, msg)
		if m.busy || committed != slices.Contains(m.websites, added) {
			t.Fatal("editor ignored the result of a save racing with cancellation")
		}
		if committed && (m.form != "" || m.focus != "website.manage") {
			t.Fatal("published save remained editable for duplicate submission")
		}
		if !committed && (m.form != "website-editor" || m.websiteEditor.address.Value() != added.URL || !strings.Contains(ansi.Strip(m.screen), m.tr("websiteEditor.notSaved"))) {
			t.Fatal("cancellation lost the draft or failed to acknowledge it")
		}
	}
}
