package tui

import (
	"context"
	"errors"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

func TestOverviewWebsiteMenuUsesNameAndWorksWithStoppedCore(t *testing.T) {
	m, _ := overviewWebsiteDragModel(t, 3)
	m.snapshot.Service.Active, m.snapshot.Service.State = false, "inactive"
	m.layout()
	site := m.websites[1]
	right := websitePointer(t, m, websiteActionID(site))
	right.Button = tea.MouseRight
	m, _ = update(m, tea.MouseClickMsg(right))
	if m.menu.open {
		t.Fatal("a right-click on the result opened the name menu")
	}
	right = websitePointer(t, m, overviewWebsiteDragID(site))
	right.Button = tea.MouseRight
	m, _ = update(m, tea.MouseClickMsg(right))
	if !m.menu.open || m.websiteMenu != site || m.focus != websiteActionID(site) {
		t.Fatal("right-clicking the name should focus the site and open its menu")
	}
	var actions []string
	for _, item := range m.menu.items {
		actions = append(actions, item.ID)
	}
	if !slices.Equal(actions, []string{"edit", "remove"}) {
		t.Fatalf("website actions = %v, want edit and remove", actions)
	}
	m, _ = update(m, special(tea.KeyEscape))
	m, _ = update(m, key("m"))
	m, _ = update(m, special(tea.KeyEnter))
	if m.form != "website-editor" || m.websiteEditor.original != site || len(m.websiteTests.running) != 0 {
		t.Fatal("the keyboard menu should edit the focused, stopped site")
	}
}

func TestOverviewWebsiteMenuDismissesOnStaleSelection(t *testing.T) {
	m, _ := overviewWebsiteDragModel(t, 3)
	site := m.websites[1]
	m.focus = websiteActionID(site)
	m, _ = update(m, key("m"))
	m, _ = update(m, special(tea.KeyDown))
	if !m.menu.open || m.menu.items[m.menu.selected].ID != "remove" {
		t.Fatal("Remove should follow Edit in the site menu")
	}
	sites := slices.Clone(m.websites)
	sites[0], sites[1] = sites[1], sites[0]
	m.setWebsites(sites)
	m.reconcileMenu()
	if !m.menu.open || m.websiteMenu != site || m.menu.items[m.menu.selected].ID != "remove" {
		t.Fatal("reordering sites should preserve the selected menu action for the same site")
	}
	sites = slices.Clone(sites)
	sites[0].Name = "New name"
	m.setWebsites(sites)
	m.reconcileMenu()
	if m.menu.open || m.websiteMenu.URL != "" || m.form != "" {
		t.Fatal("an externally replaced site should not keep an actionable menu")
	}
}

func TestOverviewWebsiteMenuRemovesSelectedSiteImmediately(t *testing.T) {
	sites := app.DefaultWebsites()
	renamed := app.Website{Name: "Work", URL: sites[1].URL}
	stale := diagnostic.New("diagnostic.website.missing", "stale website")
	for _, tc := range []struct {
		name           string
		initial, saved []app.Website
		selected       app.Website
		err            error
		focus          string
	}{
		{"middle", sites, []app.Website{sites[0], sites[2]}, sites[1], nil, websiteActionID(sites[2])},
		{"last remaining", sites[:1], []app.Website{}, sites[0], nil, "website.add"},
		{"write failed", sites, nil, sites[1], errors.New("write failed"), websiteActionID(sites[1])},
		{"already removed", sites, sites[1:], sites[0], stale, websiteActionID(sites[1])},
		{"renamed and moved", sites, []app.Website{sites[0], sites[2], renamed}, sites[1], stale, websiteActionID(renamed)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, b := overviewWebsiteDragModel(t, len(tc.initial))
			calls := 0
			b.removeWebsite = func(_ context.Context, site app.Website) ([]app.Website, error) {
				calls++
				if site != tc.selected {
					t.Fatalf("removed %+v, want %+v", site, tc.selected)
				}
				return tc.saved, tc.err
			}
			m.focus = websiteActionID(tc.selected)
			m, _ = update(m, key("m"))
			m, _ = update(m, special(tea.KeyDown))
			m, cmd := update(m, special(tea.KeyEnter))
			if m.form != "" || m.menu.open || !m.busy || !slices.Equal(m.websites, tc.initial) {
				t.Fatal("choosing Remove should close the menu and submit the selected site without a dialog")
			}
			m = finishWebsiteAction(t, m, cmd)
			want := tc.saved
			if want == nil {
				want = tc.initial
			}
			if calls != 1 || m.busy || m.form != "" || m.focus != tc.focus || !slices.Equal(m.websites, want) || m.websites == nil {
				t.Fatalf("after removal got calls %d, sites %v, focus %q", calls, m.websites, m.focus)
			}
			if tc.err != nil && !slices.ContainsFunc(m.notification.items, func(item notificationItem) bool {
				return item.Kind == "warning" && item.Message == m.localizeError(tc.err)
			}) {
				t.Fatalf("missing removal error in %+v", m.notification.items)
			}
		})
	}
}
