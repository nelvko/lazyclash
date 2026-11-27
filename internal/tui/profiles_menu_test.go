package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

func profileMenuRightClick(t *testing.T, m Model, target string) Model {
	t.Helper()
	r := findTarget(t, m, target)
	m, _ = update(m, tea.MouseClickMsg{X: r.X + r.W/2, Y: r.Y + r.H/2, Button: tea.MouseRight})
	if !m.menu.open {
		t.Fatal("right click did not open the profile menu")
	}
	return m
}

func profileMenuItemIndex(t *testing.T, m Model, id string) int {
	t.Helper()
	for i, item := range m.menu.items {
		if item.ID == id {
			return i
		}
	}
	t.Fatalf("menu action %q missing: %+v", id, m.menu.items)
	return -1
}

func chooseProfileMenuItem(t *testing.T, m Model, id string) (Model, tea.Cmd) {
	t.Helper()
	m.menu.selected = profileMenuItemIndex(t, m, id)
	return update(m, special(tea.KeyEnter))
}

func TestProfileMenuMouseAndKeyboardEntrypointsDoNotApply(t *testing.T) {
	for _, entry := range []string{"card", "refresh", "button", "m", "shift+f10"} {
		t.Run(entry, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			switch entry {
			case "card":
				m = profileMenuRightClick(t, m, "profile.0")
			case "refresh":
				m = profileMenuRightClick(t, m, "profiles.refresh.0")
			case "button":
				m, _ = click(m, findTarget(t, m, "profiles.menu.0"))
			case "m":
				m.focus = "profile.0"
				m, _ = update(m, key("m"))
			case "shift+f10":
				m.focus = "profile.0"
				m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyF10, Mod: tea.ModShift})
			}
			if !m.menu.open || m.profilesUI.menuProfile != "p-stable-profile-id" || m.busy || len(b.calls) != 0 || m.snapshot.Applied != "default" {
				t.Fatalf("menu entry %q applied a profile or lost its identity", entry)
			}
		})
	}
}

func TestProfileMenuOutsideClickAndEscapeDoNotReachCards(t *testing.T) {
	for _, closing := range []string{"escape", "outside click"} {
		t.Run(closing, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			m.snapshot.Profiles = append(m.snapshot.Profiles, "outside")
			m.snapshot.ProfileDetails = append(m.snapshot.ProfileDetails, app.ProfileInfo{ID: "outside", Name: "Outside card", Validated: true})
			m.layout()
			outside := findTarget(t, m, "profile.1")
			m = profileMenuRightClick(t, m, "profile.0")
			if closing == "escape" {
				m, _ = update(m, special(tea.KeyEscape))
			} else {
				m, _ = click(m, outside)
			}
			if m.menu.open || m.busy || len(b.calls) != 0 || m.snapshot.Applied != "default" || m.focus != "profile.0" {
				t.Fatal("closing the menu applied the underlying card or lost its originating focus")
			}
		})
	}
}

func TestProfileMenuOwnsPasteAndPageShortcuts(t *testing.T) {
	for _, msg := range []tea.Msg{tea.PasteMsg{Content: "https://example.invalid/private"}, key("q"), key("2")} {
		m, b := profileClickModel(t, true, true, false, false)
		m = profileMenuRightClick(t, m, "profile.0")
		m, _ = update(m, msg)
		if m.quitting || m.page != "profiles" || m.profilesUI.url.Value() != "" || m.form != "" || m.busy || len(b.calls) != 0 {
			t.Fatalf("menu input %T escaped to the page or its URL field", msg)
		}
	}
}

func TestProfileMenuOnlyOffersSupportedActions(t *testing.T) {
	for _, tc := range []struct {
		name               string
		installed, current bool
	}{
		{"uninstalled", false, false},
		{"current", true, true},
		{"available", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, b := profileClickModel(t, tc.installed, false, tc.current, false)
			m.layout()
			m = profileMenuRightClick(t, m, "profile.0")
			present := map[string]bool{}
			for index, item := range m.menu.items {
				present[item.ID] = true
				if item.ID == "apply" && !tc.installed {
					if item.DisabledReason == "" {
						t.Fatal("Use must explain that installation is required")
					}
					m.menu.selected = index
					m, _ = update(m, special(tea.KeyEnter))
					if m.busy || len(b.calls) != 0 {
						t.Fatal("Use ran without an installed core")
					}
				}
			}
			if present["apply"] == tc.current || !present["refresh"] {
				t.Fatalf("unexpected available actions: %v", present)
			}
		})
	}
}

func TestProfileMenuDispatchesSharedActionsByStableID(t *testing.T) {
	for _, action := range []string{"refresh", "settings", "delete", "apply"} {
		t.Run(action, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			var refreshed string
			b.refresh = func(_ context.Context, id string) error { refreshed = id; return nil }
			m = profileMenuRightClick(t, m, "profile.0")
			m, cmd := chooseProfileMenuItem(t, m, action)
			if m.menu.open {
				t.Fatal("selected action left its context menu open")
			}
			switch action {
			case "refresh":
				m = finishProfileJob(t, m, cmd)
				if refreshed != "p-stable-profile-id" || len(b.calls) != 0 || m.snapshot.Applied != "default" {
					t.Fatal("Update applied a profile or refreshed the wrong identity")
				}
			case "apply":
				m = finishProfileJob(t, m, cmd)
				if len(b.calls) != 1 || b.calls[0] != "p-stable-profile-id" {
					t.Fatal("Use profile did not call the shared Apply use case")
				}
			case "settings":
				m = finishProfileSettingsLoad(t, m, cmd)
				if m.form != "profile-settings" || m.name != "p-stable-profile-id" || m.profileCreate.name.Value() != "Office display name" || len(b.calls) != 0 {
					t.Fatal("Settings did not open the selected profile's metadata")
				}
			case "delete":
				if m.form != "profile-delete" || m.name != "p-stable-profile-id" || m.busy || len(b.calls) != 0 {
					t.Fatal("Delete did not open a confirmation for the selected ID")
				}
			}
		})
	}
}

func TestProfileMenuSurvivesReorderAndClosesWhenTargetDisappears(t *testing.T) {
	for _, removed := range []bool{false, true} {
		t.Run(map[bool]string{false: "reordered", true: "removed"}[removed], func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			var refreshed string
			b.refresh = func(_ context.Context, id string) error { refreshed = id; return nil }
			m = profileMenuRightClick(t, m, "profile.0")
			next := m.snapshot
			next.Profiles = []string{"new-profile", "p-stable-profile-id"}
			next.ProfileDetails = append([]app.ProfileInfo{{ID: "new-profile", Name: "New profile"}}, next.ProfileDetails...)
			if removed {
				next.Profiles = []string{"new-profile"}
				next.ProfileDetails = next.ProfileDetails[:1]
			}
			b.snapshot = next
			m, _ = update(m, coreStatusMsg{generation: m.statusGeneration, snapshot: next})
			if removed {
				if m.menu.open || m.profilesUI.menuProfile != "" || len(b.calls) != 0 {
					t.Fatal("menu retained a deleted profile or applied its replacement")
				}
				return
			}
			if !m.menu.open || m.profilesUI.menuProfile != "p-stable-profile-id" {
				t.Fatal("reordering changed the menu's stable target")
			}
			m, cmd := chooseProfileMenuItem(t, m, "refresh")
			m = finishProfileJob(t, m, cmd)
			if refreshed != "p-stable-profile-id" || len(b.calls) != 0 {
				t.Fatal("reordered menu refreshed the new row occupant")
			}
		})
	}
}

func TestProfileMenuCannotOpenBehindDialogsOrWhileBusy(t *testing.T) {
	for _, state := range []string{"busy", "quitting", "form", "ports", "wizard", "sidebar"} {
		t.Run(state, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			r := findTarget(t, m, "profile.0")
			switch state {
			case "busy":
				m.busy = true
			case "quitting":
				m.quitting = true
			case "form":
				m.openForm()
			case "ports":
				m.ports.open = true
			case "wizard":
				m.wizard.open = true
			case "sidebar":
				m.sidebarOpen = true
			}
			m, _ = update(m, tea.MouseClickMsg{X: r.X + r.W/2, Y: r.Y + r.H/2, Button: tea.MouseRight})
			if m.menu.open || len(b.calls) != 0 || m.snapshot.Applied != "default" {
				t.Fatal("right click escaped the active task or overlay")
			}
		})
	}
}

func TestProfileMenuUsesUncoveredCardCellsWithoutClickingThroughToast(t *testing.T) {
	m, b := profileClickModel(t, true, true, false, false)
	publishNotice(&m, "profile.refresh", "error", "Subscription update failed")
	if len(m.notification.overlays) != 1 {
		t.Fatal("expected one visible notification")
	}
	toast := m.notification.overlays[0].Bounds
	var card hitRegion
	for _, region := range m.profileMenuCardRegions() {
		if region.ID == "profile.0" {
			card = region
		}
	}
	if regionsOverlap(card, toast) {
		t.Fatal("header toast moved down into the profile card")
	}
	m, _ = update(m, tea.MouseClickMsg{X: card.X, Y: card.Y, Button: tea.MouseRight})
	if !m.menu.open || m.profilesUI.menuProfile != "p-stable-profile-id" || len(b.calls) != 0 {
		t.Fatal("toast blocked a visible card's context menu")
	}
	m, _ = update(m, special(tea.KeyEscape))
	m, _ = update(m, tea.MouseClickMsg{X: toast.X, Y: toast.Y, Button: tea.MouseRight})
	if m.menu.open || len(b.calls) != 0 {
		t.Fatal("context click passed through the toast")
	}
}

func TestProfileMenuDoesNotOpenForURLInputOrBlankSpace(t *testing.T) {
	for _, input := range []bool{false, true} {
		m, b := profileClickModel(t, true, true, false, false)
		x, y := 0, 0
		if input {
			r := findTarget(t, m, "profiles.url")
			x, y = r.X+r.W/2, r.Y+r.H/2
		}
		m, _ = update(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight})
		if m.menu.open || m.busy || len(b.calls) != 0 {
			t.Fatal("non-card context opened a profile menu or applied a profile")
		}
	}
}
