package tui

import (
	"errors"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestInputClearMouseEditsWithoutSubmitting(t *testing.T) {
	for _, scenario := range []string{"profile-url", "generic-form", "create-name", "create-description", "create-source", "ports-mixed", "ports-controller", "wizard-local", "wizard-version", "wizard-prefix", "inline-local", "inline-version"} {
		t.Run(scenario, func(t *testing.T) {
			m, id, writes := inputFieldsScenario(t, scenario, "en")
			input := inputForClearTest(&m, id)
			input.SetValue("中文-long-value")
			input.SetCursor(2) // Clear also removes the text after the cursor.
			form, step, job, generation := m.form, m.wizard.step, m.job, m.id
			m.targets = []hitRegion{{ID: id + ".clear", X: 1, Y: 1, W: 3, H: 1}}
			m.mouse(tea.MouseClickMsg{X: 2, Y: 1, Button: tea.MouseLeft})
			if input.Value() != "" || !input.Focused() {
				t.Fatalf("clear value = %q, focused = %v", input.Value(), input.Focused())
			}
			if m.form != form || m.wizard.step != step || m.job != job || m.id != generation || m.busy || writes.Load() != 0 {
				t.Fatal("clearing submitted, advanced a form, or started backend work")
			}
		})
	}
}

func TestInputClearTabAndEnter(t *testing.T) {
	for _, scenario := range []string{"profile-url", "ports-mixed", "wizard-local", "wizard-prefix", "inline-version"} {
		t.Run(scenario, func(t *testing.T) {
			m, id, writes := inputFieldsScenario(t, scenario, "en")
			inputForClearTest(&m, id).SetValue("draft")
			m.layout()
			findTarget(t, m, id+".clear")
			mod := tea.ModShift
			if scenario == "profile-url" || scenario == "inline-version" {
				mod = 0 // Page controls follow their rendered row/column order.
			}
			m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: mod})
			focus := m.focus
			if m.ports.open {
				focus = m.ports.focus
			} else if m.wizard.open || m.wizard.networkOpen {
				focus = m.wizard.focus
			}
			if focus != id+".clear" || inputForClearTest(&m, id).Value() != "draft" {
				t.Fatalf("Tab focus = %q, want clear control", focus)
			}
			m, _ = update(m, special(tea.KeyEnter))
			if inputForClearTest(&m, id).Value() != "" || !inputForClearTest(&m, id).Focused() || m.busy || writes.Load() != 0 {
				t.Fatal("Enter on clear did not return to an empty field without submission")
			}
		})
	}
}

func inputForClearTest(m *Model, id string) *textinput.Model {
	switch id {
	case "profiles.url":
		return &m.profilesUI.url
	case "import-input":
		return &m.input
	case "profile-create.name":
		return &m.profileCreate.name
	case "profile-create.description":
		return &m.profileCreate.description
	case "profile-create.source":
		return &m.profileCreate.source
	case "ports.mixed":
		return &m.ports.mixed
	case "ports.controller":
		return &m.ports.controller
	case "local", "setup.local":
		return &m.wizard.local
	case "version-input", "setup.version-input":
		return &m.wizard.version
	case "prefix":
		return &m.wizard.prefix
	}
	panic("unknown input field " + id)
}

func TestInputClearInvalidatesErrorsCompletionAndPlans(t *testing.T) {
	t.Run("profile-url", func(t *testing.T) {
		m, id, _ := inputFieldsScenario(t, "profile-url", "en")
		m.profilesUI.url.SetValue("invalid")
		m.profilesUI.urlError, m.profilesUI.urlErrorValue = "invalid address", "invalid"
		m.clearInputField(id + ".clear")
		if m.profilesUI.urlError != "" || m.profilesUI.urlErrorValue != "" || !m.profilesUI.editing || m.focus != id {
			t.Fatal("clear retained URL validation or lost editing focus")
		}
	})
	t.Run("path", func(t *testing.T) {
		m, id, _ := inputFieldsScenario(t, "generic-form", "en")
		m.input.SetValue("/tmp/invalid")
		m.paths.active, m.paths.query, m.failure = true, "/tmp/invalid", "invalid file"
		generation := m.paths.generation
		m.clearInputField(id + ".clear")
		if m.paths.active || m.paths.query != "" || m.paths.generation <= generation || m.failure != "" {
			t.Fatal("clear retained completion or validation")
		}
	})
	t.Run("create", func(t *testing.T) {
		m, id, _ := inputFieldsScenario(t, "create-source", "en")
		m.profileCreate.name.SetValue("name")
		m.profileCreate.description.SetValue("description")
		m.profileCreate.source.SetValue("/tmp/profile")
		m.profileCreateSetField(profileCreateName)
		m.failure, m.paths.active = "invalid source", true
		m.clearInputField(id + ".clear")
		if m.profileCreate.name.Value() != "name" || m.profileCreate.description.Value() != "description" || m.profileCreate.field != profileCreateSource || m.failure != "" || m.paths.active {
			t.Fatal("clear altered another draft field or retained stale validation")
		}
	})
	t.Run("ports", func(t *testing.T) {
		m, id, _ := inputFieldsScenario(t, "ports-controller", "en")
		m.ports.plan = app.RuntimeSettingsPlan{Changed: true}
		m.ports.controller.Err, m.ports.err = errors.New("invalid port"), "invalid port"
		m.clearInputField(id + ".clear")
		if m.ports.plan.Changed || m.ports.err != "" || m.ports.controller.Err != nil || !m.ports.reveal || m.ports.mixed.Value() != "7897" {
			t.Fatal("clear retained reviewed ports or altered the other port")
		}
	})
	t.Run("install", func(t *testing.T) {
		m, id, _ := inputFieldsScenario(t, "wizard-version", "en")
		m.wizard.version.SetValue("v1.2.3")
		m.wizard.release = mihomo.Release{Asset: "stale"}
		m.wizard.plan = app.InstallPlan{BlockedReason: "stale"}
		m.wizard.installRequested, m.wizard.err = true, "invalid release"
		m.clearInputField(id + ".clear")
		if m.wizard.release.Asset != "" || m.wizard.plan.BlockedReason != "" || m.wizard.installRequested || m.wizard.err != "" {
			t.Fatal("clear retained release or install plan")
		}
	})
	t.Run("network", func(t *testing.T) {
		m, id, _ := inputFieldsScenario(t, "wizard-prefix", "en")
		m.wizard.prefix.SetValue("https://example.invalid/")
		m.wizard.draft.Prefix = m.wizard.prefix.Value()
		m.wizard.versionTest.Status, m.wizard.fileTest.Status = checkPassed, checkPassed
		m.wizard.err, m.wizard.testPending = "stale check", 2
		m.clearInputField(id + ".clear")
		if m.wizard.draft.Prefix != "" || m.wizard.err != "" || m.wizard.testPending != 0 || m.wizard.versionTest.Status != checkNotTested || m.wizard.fileTest.Status != checkNotTested {
			t.Fatal("clear retained network draft or check results")
		}
	})
}

func TestDeleteConfirmationClearKeepsDialogAndFocusesInput(t *testing.T) {
	m, _, writes := inputFieldsScenario(t, "generic-form", "en")
	m.form, m.name = "profile-delete", "input-fixture"
	m.input.SetValue("input-fixture")
	m.profileDeleteSetFocus("profile-delete.confirm")
	m.failure = "previous failure"
	m.clearInputField("import-input.clear")
	if m.form != "profile-delete" || m.name != "input-fixture" || m.profilesUI.deleteFocus != "import-input" || !m.input.Focused() || m.input.Value() != "" || m.failure != "" || m.busy || writes.Load() != 0 {
		t.Fatal("clearing a confirmation deleted a profile or closed the dialog")
	}
}

func TestInputClearCannotEditBusyOrQuittingForms(t *testing.T) {
	for _, state := range []string{"busy", "quitting"} {
		for _, scenario := range []string{"profile-url", "generic-form", "create-source", "ports-mixed", "wizard-local", "wizard-prefix", "inline-version"} {
			t.Run(state+"/"+scenario, func(t *testing.T) {
				m, id, _ := inputFieldsScenario(t, scenario, "en")
				input := inputForClearTest(&m, id)
				input.SetValue("draft")
				m.busy, m.quitting = state == "busy", state == "quitting"
				handled, cmd := m.clearInputField(id + ".clear")
				if !handled || cmd != nil || input.Value() != "draft" {
					t.Fatal("clear changed an in-flight or quitting draft")
				}
			})
		}
	}
}
