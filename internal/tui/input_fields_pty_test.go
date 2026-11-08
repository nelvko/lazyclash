package tui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

// This opt-in fixture drives actual Model input views and Update methods. All
// backend operations use memory; the only writes are its explicit JSON report.
func TestInputFieldsPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_INPUT_FIELDS_PTY_REPORT")
	if report == "" {
		t.Skip("explicit input-fields PTY report required")
	}
	scenario := os.Getenv("LAZYCLASH_INPUT_FIELDS_PTY_SCENARIO")
	m, id, writes := inputFieldsScenario(t, scenario, os.Getenv("LAZYCLASH_INPUT_FIELDS_PTY_LANGUAGE"))
	fixture := inputFieldsPTY{model: m, scenario: scenario, fieldID: id, report: report, writes: writes}
	fixture.writeState()
	result, err := tea.NewProgram(fixture, tea.WithoutSignalHandler()).Run()
	if err != nil {
		t.Fatal(err)
	}
	final := result.(inputFieldsPTY)
	final.model.stopRuntime()
	final.model.stopLogs()
	final.model.stopRules()
	final.model.stopRuleProviders()
	final.writeState()
}

func inputFieldsScenario(t *testing.T, scenario, language string) (Model, string, *atomic.Int64) {
	t.Helper()
	writes := &atomic.Int64{}
	if strings.HasPrefix(scenario, "search-") {
		page := strings.TrimPrefix(scenario, "search-")
		var m Model
		var id string
		if page == "providers" {
			m, _ = ruleProvidersModel(t, 3)
			id = "rules.providers-search"
			m.ruleProviders.editing = true
			m.ruleProviders.search.Focus()
			m.focus = id
		} else {
			m, id = searchFieldModel(t, page, 160, 45)
			m, _ = click(m, findTarget(t, m, id))
		}
		m.locale = i18n.New(language)
		m.width, m.height = 160, 45
		m.layout()
		return m, id, writes
	}
	write := func(context.Context, string) error { writes.Add(1); return nil }
	backend := &fakeBackend{snapshot: app.Snapshot{Installed: true, ConfigReady: true, HasRun: true,
		Version: "v1.19.33", Profiles: []string{"input-fixture"}, ProfileDetails: profileFixtures("input-fixture"),
		Service: service.Status{State: "inactive"}, Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}}},
		control: write, refresh: write, deleteProfile: write,
		add:             func(context.Context, string, string) error { writes.Add(1); return nil },
		addSubscription: func(context.Context, string, string) error { writes.Add(1); return nil },
		createProfile:   func(context.Context, app.ProfileDraft) error { writes.Add(1); return nil },
		save:            func(context.Context, downloadnet.Config) error { writes.Add(1); return nil },
		applySettings: func(context.Context, app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
			writes.Add(1)
			return app.RuntimeSettingsResult{}, nil
		}}
	m := ready(backend)
	m.locale = i18n.New(language)
	m.width, m.height, m.page, m.focus = 160, 45, "profiles", "profiles.url"
	id := ""
	switch scenario {
	case "profile-url":
		id = "profiles.url"
		m.profilesUI.editing = true
		m.profilesUI.url.Focus()
	case "generic-form", "masked-form":
		m.openForm()
		id = "import-input"
		if scenario == "masked-form" {
			// No current product network-password field exists. This explicit
			// harness checks EchoPassword through the real generic form view.
			m.input.EchoMode, m.input.EchoCharacter = textinput.EchoPassword, '•'
		}
	case "rename-form", "delete-form":
		form := "profile-rename"
		if scenario == "delete-form" {
			form = "profile-delete"
		}
		m.openProfileEdit(form, "input-fixture")
		id, m.focus = "import-input", "import-input"
		if scenario == "delete-form" {
			m.profileDeleteSetFocus(id)
		}
	case "create-name", "create-description", "create-source":
		m.openProfileCreate()
		field := map[string]int{"create-name": profileCreateName, "create-description": profileCreateDescription, "create-source": profileCreateSource}[scenario]
		m.profileCreateSetField(field)
		id = profileCreateFieldID(field)
	case "ports-mixed", "ports-controller":
		m.page, m.focus = "settings", "runtime-settings"
		m.ports = newPortsState()
		m.ports.open, m.ports.step = true, "edit"
		m.ports.current = app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}
		m.ports.mixed.SetValue("7897")
		m.ports.controller.SetValue("9097")
		id = "ports." + strings.TrimPrefix(scenario, "ports-")
		m.ports.focus, m.ports.reveal = id, true
	case "wizard-local", "wizard-version", "wizard-prefix":
		m.openManagementWizard(true)
		id = "local"
		if scenario == "wizard-version" {
			m.wizard.localMode, m.wizard.specific = false, true
			m.wizard.local.Blur()
			m.wizard.version.Focus()
			m.wizard.focus, id = "version-input", "version-input"
		} else if scenario == "wizard-prefix" {
			m.openNetwork(false)
			m.wizard.draft.Method = "accelerated"
			m.wizard.local.Blur()
			m.wizard.prefix.Focus()
			m.wizard.focus, id = "prefix", "prefix"
		}
	case "inline-local", "inline-version":
		m.snapshot.Installed = false
		m.page, m.wizard.inline, m.wizard.open = "overview", true, false
		m.wizard.step, m.wizard.more = stepVersion, true
		m.wizard.localMode = scenario == "inline-local"
		m.wizard.specific = !m.wizard.localMode
		id = "setup.local"
		m.wizard.focus = "local"
		m.wizard.local.Focus()
		if m.wizard.specific {
			id, m.wizard.focus = "setup.version-input", "version-input"
			m.wizard.local.Blur()
			m.wizard.version.Focus()
		}
		m.focus = id
	default:
		t.Fatalf("unknown input-fields PTY scenario %q", scenario)
	}
	m.layout()
	return m, id, writes
}

type inputFieldsPTY struct {
	model                     Model
	scenario, fieldID, report string
	writes                    *atomic.Int64
}

func (m inputFieldsPTY) Init() tea.Cmd  { return textinput.Blink }
func (m inputFieldsPTY) View() tea.View { return m.model.View() }
func (m inputFieldsPTY) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.model.Update(msg)
	m.model = next.(Model)
	m.writeState()
	return m, cmd
}

func (m *inputFieldsPTY) input() *textinput.Model {
	switch m.scenario {
	case "search-connections":
		return &m.model.connections.search
	case "search-rules":
		return &m.model.rules.search
	case "search-logs":
		return &m.model.logs.search
	case "search-providers":
		return &m.model.ruleProviders.search
	case "profile-url":
		return &m.model.profilesUI.url
	case "generic-form", "masked-form", "rename-form", "delete-form":
		return &m.model.input
	case "create-name":
		return &m.model.profileCreate.name
	case "create-description":
		return &m.model.profileCreate.description
	case "create-source":
		return &m.model.profileCreate.source
	case "ports-mixed":
		return &m.model.ports.mixed
	case "ports-controller":
		return &m.model.ports.controller
	case "wizard-local", "inline-local":
		return &m.model.wizard.local
	case "wizard-version", "inline-version":
		return &m.model.wizard.version
	case "wizard-prefix":
		return &m.model.wizard.prefix
	}
	panic("scenario has no input")
}

func (m inputFieldsPTY) writeState() {
	input := m.input()
	hash := sha256.Sum256([]byte(input.Value()))
	state := map[string]any{"scenario": m.scenario, "field_id": m.fieldID, "focus": m.model.focus, "hover": m.model.hover,
		"value_hash": hex.EncodeToString(hash[:]), "runes": len([]rune(input.Value())), "position": input.Position(),
		"focused": input.Focused(), "char_limit": input.CharLimit, "width": m.model.width, "height": m.model.height,
		"writes": m.writes.Load(), "targets": m.model.targets}
	clearID := searchClearID(m.fieldID)
	if clearID == "" {
		clearID = m.fieldID + ".clear"
	}
	state["clear_id"] = clearID
	if m.scenario == "search-rules" {
		state["matching"] = []bool{m.model.rules.caseSensitive, m.model.rules.wholeWord, m.model.rules.regex}
	}
	for _, target := range m.model.targets {
		if target.ID == m.fieldID {
			state["field"] = target
			break
		}
	}
	if cursor := m.model.View().Cursor; cursor != nil {
		state["cursor"] = map[string]int{"x": cursor.X, "y": cursor.Y, "shape": int(cursor.Shape)}
	}
	value, err := json.Marshal(state)
	if err == nil {
		err = os.WriteFile(m.report+".tmp", value, 0600)
	}
	if err == nil {
		err = os.Rename(m.report+".tmp", m.report)
	}
	if err != nil {
		panic(fmt.Sprintf("write input PTY state: %v", err))
	}
}
