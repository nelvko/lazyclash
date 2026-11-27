package tui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
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
