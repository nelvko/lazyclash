package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func profileDeleteDialogText(m Model) string {
	screen, _ := m.renderProfileDeleteForm("")
	var content []string
	for _, line := range strings.Split(ansi.Strip(screen), "\n") {
		if _, rest, ok := strings.Cut(line, "│"); ok {
			body, _, _ := strings.Cut(rest, "│")
			content = append(content, strings.TrimSpace(body))
		}
	}
	return strings.Join(content, " ")
}

func TestDeleteCurrentProfileShowsDirectRoutingBeforeConfirm(t *testing.T) {
	for _, language := range []string{i18n.Chinese, i18n.English} {
		for _, running := range []bool{true, false} {
			m, b := profileClickModel(t, true, running, true, true)
			m.locale = i18n.New(language)
			m = profileMenuRightClick(t, m, "profile.0")
			m, _ = chooseProfileMenuItem(t, m, "delete")
			body := profileDeleteDialogText(m)
			for _, msg := range []string{m.tr("profiles.delete_direct"), m.tr("profiles.confirm_direct")} {
				if !strings.Contains(body, msg) {
					t.Fatalf("%s: missing deletion scope %q in %s", language, msg, body)
				}
			}
			if !running && !strings.Contains(body, m.tr("profiles.delete_stopped")) {
				t.Fatal("stopped instance did not explain when new routing takes effect", body)
			}
			if m.profilesUI.deleteFocus != deleteCancel || hasAction(m.targets, deleteChoice) {
				t.Fatal("last profile requires only a single explicit confirmation")
			}
			calls := 0
			b.deleteAndSwitch = func(_ context.Context, id, replacement string) error {
				calls++
				if id != "p-stable-profile-id" || replacement != "default" {
					t.Fatalf("unexpected removal %s -> %s", id, replacement)
				}
				return nil
			}
			m, cmd := click(m, findTarget(t, m, deleteConfirm))
			_ = finishProfileJob(t, m, cmd)
			if calls != 1 {
				t.Fatal("single confirmation did not run the shared deletion exactly once")
			}
		}
	}
}

func TestReplacementSelectorKeepsIdentityAndMakesChoiceExplicit(t *testing.T) {
	m, b := profileClickModel(t, true, true, true, false)
	m.snapshot.Profiles = append(m.snapshot.Profiles, "alt-b", "alt-c")
	m.snapshot.ProfileDetails = append(m.snapshot.ProfileDetails, app.ProfileInfo{ID: "alt-b", Name: "Beta"}, app.ProfileInfo{ID: "alt-c", Name: "Gamma"})
	m.openProfileEdit("profile-delete", "p-stable-profile-id")
	m.layout()
	if hasAction(m.targets, deleteConfirm) || m.profilesUI.deleteReplacement != "" {
		t.Fatal("multiple saved alternatives must not pick arbitrary routing")
	}
	m, _ = click(m, findTarget(t, m, deleteChoice))
	if !m.menu.open {
		t.Fatal("chooser did not open")
	}
	m, _ = update(m, special(tea.KeyDown))
	m.snapshot.Profiles = []string{"alt-c", "p-stable-profile-id", "alt-b"}
	m.reconcileMenu()
	m, _ = update(m, special(tea.KeyEnter))
	if m.profilesUI.deleteReplacement != "alt-c" || m.menu.open || m.busy {
		t.Fatal("choosing a replacement should retain its ID through reordering and only update the dialog")
	}
	if !strings.Contains(ansi.Strip(m.screen), m.tr("profiles.delete_switch", "Gamma")) {
		t.Fatal("deletion did not identify the reviewed replacement", ansi.Strip(m.screen))
	}
	var got string
	b.deleteAndSwitch = func(_ context.Context, id, replacement string) error {
		if id != "p-stable-profile-id" {
			t.Fatal("changed deletion target")
		}
		got = replacement
		return nil
	}
	m, cmd := click(m, findTarget(t, m, deleteConfirm))
	m = finishProfileJob(t, m, cmd)
	if len(m.notification.items) != 1 || !strings.Contains(m.notification.items[0].Message, "Office display name") ||
		!strings.Contains(m.notification.items[0].Message, "Gamma") {
		t.Fatal("combined deletion and replacement lost a profile or split the result", m.notification.items)
	}
	if got != "alt-c" {
		t.Fatalf("unexpected replacement %q", got)
	}
}

func TestStaleProfileDeletionCannotSwitchToAnotherProfile(t *testing.T) {
	m, _ := profileClickModel(t, true, true, true, false)
	m.snapshot.Profiles = append(m.snapshot.Profiles, "only-alternative")
	m.openProfileEdit("profile-delete", "p-stable-profile-id")
	m.layout()
	if m.profilesUI.deleteReplacement != "only-alternative" || !hasAction(m.targets, deleteConfirm) {
		t.Fatal("sole candidate should be preselected")
	}
	m.profileDeleteSetFocus(deleteConfirm)
	m.snapshot.Profiles = m.snapshot.Profiles[:1]
	m.layout()
	if hasAction(m.targets, deleteConfirm) {
		t.Fatal("silently substituted direct routing after candidate disappeared")
	}
	if cmd := m.formKey(special(tea.KeyEnter)); cmd != nil || m.busy {
		t.Fatal("stale focus accepted deletion")
	}
	m.snapshot.Profiles = append(m.snapshot.Profiles, "only-alternative")
	m.snapshot.Applied = "only-alternative"
	m.layout()
	if hasAction(m.targets, deleteConfirm) || !strings.Contains(profileDeleteDialogText(m), "review again") {
		t.Fatal("stale switch still actionable", ansi.Strip(m.screen))
	}
}

func TestReplacementPickerSupportsMouseAndEscapeAcrossResize(t *testing.T) {
	m, _ := profileClickModel(t, true, true, true, false)
	m.snapshot.Profiles = append(m.snapshot.Profiles, "office")
	m.snapshot.ProfileDetails = append(m.snapshot.ProfileDetails, app.ProfileInfo{ID: "office", Name: "Office"})
	m.openProfileEdit("profile-delete", "p-stable-profile-id")
	m.layout()
	m, _ = click(m, findTarget(t, m, deleteChoice))
	m, _ = update(m, tea.WindowSizeMsg{Width: 60, Height: 20})
	if !m.menu.open {
		t.Fatal("resize unexpectedly closed replacement picker")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.form != "profile-delete" || m.profilesUI.deleteReplacement != "office" || m.menu.open {
		t.Fatal("canceling picker changed deletion or replacement")
	}
	m, _ = click(m, findTarget(t, m, deleteChoice))
	m, _ = update(m, menuMouse(m.menu, 1, tea.MouseLeft))
	if m.menu.open || m.busy || m.profilesUI.deleteReplacement != "default" || !strings.Contains(profileDeleteDialogText(m), m.tr("profiles.delete_direct")) {
		t.Fatal("choosing direct routing must show its consequence and await confirmation")
	}
}
