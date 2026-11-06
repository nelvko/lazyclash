package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestActionButtonGroupsKeepOneCellBeforeFocusAndNoneAfter(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, plain := range []bool{false, true} {
			for _, sample := range []struct {
				name, first, focused string
				render               func(*Model) (string, []hitRegion)
			}{
				{"header", "logs.follow", "logs.retry", func(m *Model) (string, []hitRegion) {
					m.page, m.focus = "logs", "logs.retry"
					m.layout()
					return m.screen, m.targets
				}},
				{"profile-toolbar", "profiles.add", "profiles.create", func(m *Model) (string, []hitRegion) {
					m.focus = "profiles.create"
					b := m.profilesToolbar(120)
					return strings.Join(b.lines, "\n"), b.targets
				}},
				{"profile-actions", "profiles.rename.1", "profiles.delete.1", func(m *Model) (string, []hitRegion) {
					m.focus = "profiles.delete.1"
					b := m.profileButtons([]profileButton{{"profiles.rename.1", m.tr("profiles.rename"), false}, {"profiles.delete.1", m.tr("profiles.delete"), false}}, 120)
					return strings.Join(b.lines, "\n"), b.targets
				}},
				{"profile-create", "profile-create.cancel", "profile-create.save", func(m *Model) (string, []hitRegion) {
					m.openProfileCreate()
					m.focus = "profile-create.save"
					return m.renderProfileCreateForm("")
				}},
				{"profile-delete", "profile-delete.cancel", "profile-delete.confirm", func(m *Model) (string, []hitRegion) {
					m.openProfileEdit("profile-delete", "work")
					m.profilesUI.deleteFocus = "profile-delete.confirm"
					return m.renderProfileDeleteForm("")
				}},
				{"host-confirmation", "tun.cancel", "tun.confirm", func(m *Model) (string, []hitRegion) {
					m.tunFocus = "tun.confirm"
					return m.renderTUNConfirmation("")
				}},
				{"ports", "ports.back", "ports.apply", func(m *Model) (string, []hitRegion) {
					m.ports = newPortsState()
					m.ports.open, m.ports.step, m.ports.focus = true, "review", "ports.apply"
					return m.renderPorts("")
				}},
				{"wizard", "done", "retry", func(m *Model) (string, []hitRegion) {
					m.openManagementWizard(false)
					m.abortLookup()
					m.busy, m.job = false, ""
					m.wizard.step, m.wizard.focus, m.wizard.err = stepResult, "retry", "Archive unavailable"
					m.wizard.failedStage = "extract"
					return m.renderWizard("")
				}},
				{"inline-install", "setup.done", "setup.later", func(m *Model) (string, []hitRegion) {
					m.focus = "setup.later"
					d := inlineInstallDocument{model: *m, width: 120}
					d.buttons(wizardButton{"done", m.tr("wizard.done")}, wizardButton{"later", m.tr("dashboard.guide.later")})
					return strings.Join(d.lines, "\n"), d.targets
				}},
				{"notification", "notification.panel.details.1", "notification.panel.dismiss.1", func(m *Model) (string, []hitRegion) {
					var panel notificationPanel
					panel.show(0)
					panel.focus = 1
					return panel.render([]notificationItem{{ID: 1, Kind: "done", Message: "Saved"}}, 90, 24, m.locale, m.noColor)
				}},
				{"get-started", "install", "guide.later", func(m *Model) (string, []hitRegion) {
					m.focus = "guide.later"
					b := getStarted(m.navigationOptions(), 120)
					return strings.Join(b.lines, "\n"), b.targets
				}},
			} {
				t.Run(fmt.Sprintf("%s/%s/plain=%v", sample.name, language, plain), func(t *testing.T) {
					m := ready(&fakeBackend{snapshot: app.Snapshot{Profiles: []string{"work"}, ProfileDetails: profileFixtures("default", "work")}})
					m.locale, m.noColor = i18n.New(language), plain
					m.width, m.height = 160, 45
					screen, hits := sample.render(&m)
					find := func(id string) hitRegion {
						t.Helper()
						for _, hit := range hits {
							if hit.ID == id {
								return hit
							}
						}
						t.Fatalf("missing action %q", id)
						return hitRegion{}
					}
					before, focused := find(sample.first), find(sample.focused)
					if before.Y != focused.Y || before.X+before.W != focused.X {
						t.Fatalf("adjacent buttons added another gap before the focus gutter: %+v %+v", before, focused)
					}
					row := strings.Split(screen, "\n")[focused.Y]
					if got := ansi.Strip(ansi.Cut(row, focused.X, focused.X+2)); got != " ›" {
						t.Fatalf("focus gutter must leave one cell before the arrow: %q", got)
					}
				})
			}
		}
	}
}

func TestBusyToolbarButtonsDoNotExposeMouseTargets(t *testing.T) {
	m := ready(&fakeBackend{})
	m.busy = true
	if hits := m.ruleFilterButtons().targets; len(hits) != 0 {
		t.Fatalf("busy rule buttons retained mouse targets: %+v", hits)
	}
	if hits := m.logsToolbar(120, 5).targets; len(hits) != 0 {
		t.Fatalf("busy log buttons retained mouse targets: %+v", hits)
	}
}
