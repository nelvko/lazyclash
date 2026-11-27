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
		m.beginWebsiteEdit(app.Website{})
		m.layout()
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
		if m.form != "website-manager" || m.siteManager.selected != len(m.websites)-1 || !slices.Contains(m.websites, added) {
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
	m.beginWebsiteEdit(initial[0])
	m.layout()
	m.websiteEditor.address.SetValue("unsaved.example")
	for _, confirm := range []bool{false, true} {
		m, _ = click(m, findTarget(t, m, "websiteEditor.remove"))
		if !m.websiteEditor.confirmRemove || m.focus != "websiteEditor.cancel" || !strings.Contains(ansi.Strip(m.screen), initial[0].URL) {
			t.Fatal("removal did not identify the saved website and default to cancel")
		}
		if confirm {
			m, _ = update(m, special(tea.KeyTab))
		}
		var cmd tea.Cmd
		m, cmd = update(m, special(tea.KeyEnter))
		if confirm {
			m = finishWebsiteEdit(t, m, cmd)
		} else if m.form != "website-editor" || m.websiteEditor.confirmRemove || m.focus != "websiteEditor.remove" || m.websiteEditor.address.Value() != "unsaved.example" || !slices.Equal(m.websites, initial) || calls != 0 {
			t.Fatal("canceling removal did not return to the same draft")
		}
	}
	if m.form != "website-manager" || m.websites == nil || len(m.websites) != 0 || calls != 1 {
		t.Fatal("removing last website did not leave a manageable empty list")
	}

}

func TestWebsiteEditorStaleSiteRestoresCurrentList(t *testing.T) {
	initial := app.DefaultWebsites()
	current := initial[1:]
	stale := diagnostic.New("diagnostic.website.missing", "website was removed")
	for _, operation := range []string{"save", "remove"} {
		t.Run(operation, func(t *testing.T) {
			b := &fakeBackend{snapshot: settingsTestSnapshot(),
				removeWebsite: func(context.Context, app.Website) ([]app.Website, error) { return current, stale },
				updateWebsite: func(context.Context, app.Website, app.Website) ([]app.Website, error) { return current, stale },
			}
			m := ready(b)
			m.locale = i18n.New(i18n.Chinese)
			directionCleanup(t, &m)
			m.beginWebsiteEdit(initial[0])
			m.layout()
			var cmd tea.Cmd
			if operation == "remove" {
				m, _ = click(m, findTarget(t, m, "websiteEditor.remove"))
				m, cmd = click(m, findTarget(t, m, "websiteEditor.confirm"))
			} else {
				m, cmd = click(m, findTarget(t, m, "websiteEditor.save"))
			}
			m = finishWebsiteEdit(t, m, cmd)
			if m.form != "website-manager" || !slices.Equal(m.websites, current) {
				t.Fatal("stale operation did not restore the current list and focus")
			}
			if m.siteManager.err != m.tr("diagnostic.website.missing") {
				t.Fatal("stale operation did not explain the changed list")
			}
		})
	}
}

func TestWebsiteEditorUpdatesExistingNameAndURL(t *testing.T) {
	initial := app.DefaultWebsites()
	changed := app.Website{Name: "Work", URL: "https://work.example/"}
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	calls := 0
	b.updateWebsite = func(_ context.Context, original, replacement app.Website) ([]app.Website, error) {
		calls++
		if original != initial[1] || replacement != changed {
			t.Fatalf("incorrect update %+v -> %+v", original, replacement)
		}
		return []app.Website{initial[0], changed, initial[2]}, nil
	}
	m := ready(b)
	directionCleanup(t, &m)
	m.width, m.height = 40, 16
	for _, cancel := range []bool{true, false} {
		m.siteManager.selected = 1
		m.beginWebsiteEdit(initial[1])
		m.layout()
		if m.form != "website-editor" || m.focus != "websiteEditor.address" || m.websiteEditor.address.Value() != initial[1].URL || m.websiteEditor.name.Value() != initial[1].Name || !strings.Contains(ansi.Strip(m.screen), m.tr("websiteEditor.edit")) {
			t.Fatal("existing site's editor did not open prefilled")
		}
		m, _ = click(m, findTarget(t, m, "websiteEditor.address.clear"))
		m, _ = update(m, tea.PasteMsg{Content: changed.URL})
		m, _ = click(m, findTarget(t, m, "websiteEditor.name.clear"))
		m, _ = update(m, tea.PasteMsg{Content: changed.Name})
		if cancel {
			m, _ = update(m, special(tea.KeyEscape))
			if m.form != "website-manager" || m.siteManager.selected != 1 || !slices.Equal(m.websites, initial) || calls != 0 {
				t.Fatal("cancel published draft or lost focus")
			}
		} else {
			var cmd tea.Cmd
			m, cmd = update(m, special(tea.KeyEnter))
			m = finishWebsiteEdit(t, m, cmd)
			if calls != 1 || len(m.websites) != 3 || m.websites[1] != changed || m.form != "website-manager" || m.siteManager.selected != 1 {
				t.Fatal("edit did not save both fields in place")
			}
		}
	}
}

func TestWebsiteEditorCancelsPendingSave(t *testing.T) {
	for _, tc := range []struct{ edit, committed bool }{{false, false}, {false, true}, {true, false}, {true, true}} {
		committed := tc.committed
		original := app.Website{}
		if tc.edit {
			original = app.DefaultWebsites()[0]
		}
		entered := make(chan struct{})
		added := app.Website{Name: "New", URL: "https://status.example/"}
		b := &fakeBackend{snapshot: settingsTestSnapshot()}
		publish := func(ctx context.Context, site app.Website) ([]app.Website, error) {
			if site != added {
				t.Errorf("incorrect draft: %+v", site)
			}
			close(entered)
			<-ctx.Done()
			if committed {
				sites := app.DefaultWebsites()
				if tc.edit {
					sites[0] = added
					return sites, nil
				}
				return append(sites, added), nil
			}
			return nil, ctx.Err()
		}
		b.addWebsite = publish
		b.updateWebsite = func(ctx context.Context, before, after app.Website) ([]app.Website, error) {
			if before != original {
				t.Errorf("submitted stale draft base %+v", before)
			}
			return publish(ctx, after)
		}
		m := ready(b)
		directionCleanup(t, &m)
		m.beginWebsiteEdit(original)
		m.layout()
		m.websiteEditor.address.SetValue(added.URL)
		m.websiteEditor.name.SetValue(added.Name)
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
		if committed && m.form != "website-manager" {
			t.Fatal("published save remained editable for duplicate submission")
		}
		if !committed && (m.form != "website-editor" || m.websiteEditor.address.Value() != added.URL || !strings.Contains(ansi.Strip(m.screen), m.tr("websiteEditor.notSaved"))) {
			t.Fatal("cancellation lost the draft or failed to acknowledge it")
		}
	}
}
