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
				m = profileMenuRightClick(t, m, "profile.1")
			case "refresh":
				m = profileMenuRightClick(t, m, "profiles.refresh.1")
			case "button":
				m, _ = click(m, findTarget(t, m, "profiles.menu.1"))
			case "m":
				m.focus = "profile.1"
				m, _ = update(m, key("m"))
			case "shift+f10":
				m.focus = "profile.1"
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
			outside := findTarget(t, m, "profile.2")
			m = profileMenuRightClick(t, m, "profile.1")
			if closing == "escape" {
				m, _ = update(m, special(tea.KeyEscape))
			} else {
				m, _ = click(m, outside)
			}
			if m.menu.open || m.busy || len(b.calls) != 0 || m.snapshot.Applied != "default" || m.focus != "profile.1" {
				t.Fatal("closing the menu applied the underlying card or lost its originating focus")
			}
		})
	}
}

func TestProfileMenuOwnsPasteAndPageShortcuts(t *testing.T) {
	for _, msg := range []tea.Msg{tea.PasteMsg{Content: "https://example.invalid/private"}, key("q"), key("2")} {
		m, b := profileClickModel(t, true, true, false, false)
		m = profileMenuRightClick(t, m, "profile.1")
		m, _ = update(m, msg)
		if m.quitting || m.page != "profiles" || m.profilesUI.url.Value() != "" || m.form != "" || m.busy || len(b.calls) != 0 {
			t.Fatalf("menu input %T escaped to the page or its URL field", msg)
		}
	}
}

func TestProfileMenuOnlyOffersSupportedActions(t *testing.T) {
	tests := []struct {
		name, target, disabled string
		installed, current     bool
		forgetAddress          bool
	}{
		{name: "uninstalled", target: "profile.1", disabled: "apply"},
		{name: "selected", target: "profile.1", disabled: "delete", installed: true, current: true},
		{name: "one-time import", target: "profile.1", disabled: "refresh", installed: true, forgetAddress: true},
		{name: "built-in", target: "profile.0", installed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, b := profileClickModel(t, tt.installed, false, tt.current, false)
			if tt.forgetAddress {
				m.snapshot.ProfileDetails[1].Refreshable = false
				m.layout()
			}
			m = profileMenuRightClick(t, m, tt.target)
			if tt.disabled != "" {
				index := profileMenuItemIndex(t, m, tt.disabled)
				if m.menu.items[index].DisabledReason == "" {
					t.Fatalf("unsupported action %q has no disabled reason", tt.disabled)
				}
				m.menu.selected = index
				m, _ = update(m, special(tea.KeyEnter))
				if m.busy || len(b.calls) != 0 || m.form != "" {
					t.Fatalf("disabled action %q still executed", tt.disabled)
				}
			} else {
				for _, item := range m.menu.items {
					switch item.ID {
					case "rename", "address", "overrides", "delete":
						t.Fatalf("built-in profile exposes %q", item.ID)
					}
				}
			}
		})
	}
}

func TestProfileMenuDispatchesSharedActionsByStableID(t *testing.T) {
	for _, action := range []string{"refresh", "rename", "address", "overrides", "details", "delete", "apply"} {
		t.Run(action, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			var refreshed string
			b.refresh = func(_ context.Context, id string) error { refreshed = id; return nil }
			m = profileMenuRightClick(t, m, "profile.1")
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
			case "details":
				if m.profilesUI.detail != "p-stable-profile-id" || len(b.calls) != 0 || m.busy {
					t.Fatal("View details applied a profile or lost its stable identity")
				}
			default:
				want := map[string]string{"rename": "profile-rename", "address": "profile-url", "overrides": "profile-overrides", "delete": "profile-delete"}[action]
				if m.form != want || m.name != "p-stable-profile-id" || m.busy || len(b.calls) != 0 {
					t.Fatalf("%s did not open its shared form for the correct ID", action)
				}
				if action == "rename" && m.input.Value() != "Office display name" {
					t.Fatal("Rename did not prefill the existing display name")
				}
				if action == "delete" && m.input.Value() != "" {
					t.Fatal("Delete bypassed explicit name confirmation")
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
			m = profileMenuRightClick(t, m, "profile.1")
			next := m.snapshot
			next.Profiles = []string{"new-profile", "p-stable-profile-id"}
			next.ProfileDetails = append([]app.ProfileInfo{{ID: "new-profile", Name: "New profile", Refreshable: true}}, next.ProfileDetails...)
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
	for _, state := range []string{"busy", "quitting", "form", "help", "ports", "wizard", "sidebar"} {
		t.Run(state, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			r := findTarget(t, m, "profile.1")
			switch state {
			case "busy":
				m.busy = true
			case "quitting":
				m.quitting = true
			case "form":
				m.openForm()
			case "help":
				m.helpOpen = true
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
	m.message = "Subscription saved; select its card to apply"
	m.messageTransient = false
	m.syncNotification()
	m.layout()
	options := m.dashboardOptions()
	_, options.BodyTargets = m.pageBody(dashboardGeometry(options).width)
	options.Scroll = m.scroll
	_, toast, _ := notificationToast(options)
	var card hitRegion
	for _, region := range m.profileMenuCardRegions() {
		if region.ID == "profile.1" {
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
