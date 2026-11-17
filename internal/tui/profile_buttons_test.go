package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestProfileCardControlsKeepGeometryAcrossFocusAndColor(t *testing.T) {
	for _, width := range []int{12, 16, 28, 55} {
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
			t.Run(fmt.Sprintf("%d/%s", width, profile), func(t *testing.T) {
				m := profilesPreview(false)
				m.colorProfile = profile
				m.focus, m.hover = "", ""
				before := m.profileCard(1, width)
				for _, target := range before.targets {
					if !strings.HasPrefix(target.ID, "profiles.") {
						continue
					}
					m.focus = target.ID
					after := m.profileCard(1, width)
					for i, hit := range before.targets {
						if hit != after.targets[i] {
							t.Fatalf("focus moved a control: before=%+v after=%+v", hit, after.targets[i])
						}
					}
					painted := ansi.Strip(ansi.Cut(after.lines[target.Y], target.X, target.X+target.W))
					if !strings.HasPrefix(painted, " ›") || target.X+target.W > width {
						t.Fatalf("control hit does not match focused button: %+v %q", target, painted)
					}
				}
			})
		}
	}
}

func TestProfileDeleteDefaultsToCancelAndRequiresExplicitConfirmation(t *testing.T) {
	calls := 0
	m := ready(&fakeBackend{snapshot: app.Snapshot{Profiles: []string{"work"}, ProfileDetails: profileFixtures("default", "work")},
		deleteProfile: func(context.Context, string) error { calls++; return nil }})
	m.page = "profiles"
	m.openProfileEdit("profile-delete", "work")
	if m.profilesUI.deleteFocus != "profile-delete.cancel" || m.input.Focused() {
		t.Fatal("delete confirmation did not default to Cancel")
	}
	if cmd := m.formKey(special(tea.KeyEnter)); cmd != nil || m.form != "" || calls != 0 {
		t.Fatal("default Enter did not cancel deletion")
	}
	m.openProfileEdit("profile-delete", "work")
	m.formKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.profilesUI.deleteFocus != "profile-delete.confirm" || m.input.Focused() {
		t.Fatal("Shift+Tab did not reach the confirmation button")
	}
	m = finishProfileJob(t, m, m.formKey(special(tea.KeyEnter)))
	if calls != 1 || m.form != "" {
		t.Fatal("explicit confirmation did not perform exactly one deletion")
	}
}

func TestProfileDeleteButtonsStayVisibleAndMouseConfirmsWithoutName(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}, {30, 12}, {24, 7}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size[0], size[1]), func(t *testing.T) {
				calls := 0
				m := ready(&fakeBackend{snapshot: app.Snapshot{Profiles: []string{"work"}, ProfileDetails: profileFixtures("default", "work")},
					deleteProfile: func(context.Context, string) error { calls++; return nil }})
				m.page, m.width, m.height = "profiles", size[0], size[1]
				m.locale = i18n.New(language)
				m.openProfileEdit("profile-delete", "work")
				m.layout()
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
				rows := strings.Split(m.screen, "\n")
				for _, target := range m.targets {
					if strings.HasPrefix(target.ID, "import-input") {
						t.Fatal("delete confirmation retained a name input")
					}
				}
				for id, label := range map[string]string{"profile-delete.cancel": m.tr("profiles.create_cancel"), "profile-delete.confirm": m.tr("profiles.delete_confirm")} {
					hit := findTarget(t, m, id)
					if !strings.Contains(ansi.Strip(ansi.Cut(rows[hit.Y], hit.X, hit.X+hit.W)), label) {
						t.Fatalf("button hidden or hit misplaced: %+v", hit)
					}
				}
				m, cmd := click(m, findTarget(t, m, "profile-delete.confirm"))
				m = finishProfileJob(t, m, cmd)
				if calls != 1 || m.form != "" {
					t.Fatal("mouse confirmation did not delete exactly once")
				}
			})
		}
	}
}

func TestProfileDeleteOwnsInputAndCancellation(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{special(tea.KeyTab), {Code: tea.KeyTab, Mod: tea.ModShift}, special(tea.KeyLeft), special(tea.KeyRight)} {
		m := ready(&fakeBackend{})
		m.page = "profiles"
		m.openProfileEdit("profile-delete", "work")
		m, _ = update(m, key)
		if m.profilesUI.deleteFocus != "profile-delete.confirm" {
			t.Fatal("confirmation button is not keyboard accessible")
		}
		for _, text := range []string{"q", "3", "work"} {
			m, _ = update(m, tea.KeyPressMsg{Code: rune(text[0]), Text: text})
		}
		m, _ = update(m, tea.PasteMsg{Content: "must not edit or confirm"})
		if m.input.Value() != "" || m.input.Focused() || m.busy || m.quitting || m.page != "profiles" || m.form != "profile-delete" {
			t.Fatal("text input escaped or activated the confirmation")
		}
		m, _ = update(m, special(tea.KeyEsc))
		if m.form != "" || m.busy {
			t.Fatal("Escape did not cancel the confirmation")
		}
	}
}

func TestProfileDeleteCannotConfirmWhileBusyQuittingOrTooSmall(t *testing.T) {
	for _, state := range []string{"busy", "quitting", "small"} {
		t.Run(state, func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.openProfileEdit("profile-delete", "work")
			m.profileDeleteSetFocus("profile-delete.confirm")
			m.busy, m.quitting = state == "busy", state == "quitting"
			if state == "small" {
				m.width, m.height = 20, 5
			}
			id := m.id
			if cmd := m.formKey(special(tea.KeyEnter)); cmd != nil || m.id != id || m.form != "profile-delete" {
				t.Fatal("unavailable confirmation started deletion")
			}
			if _, hits := m.renderProfileDeleteForm(""); len(hits) != 0 {
				t.Fatal("unavailable confirmation exposed clickable actions")
			}
		})
	}
}
