package tui

import (
	"os"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/configuration"
)

const (
	profileCreateName = iota
	profileCreateDescription
	profileCreateSource
	profileCreateUserAgent
	profileCreateSave
	profileCreateCancel
	profileSettingsImport
	profileSettingsDeactivate
)

type profileCreateState struct {
	name         textinput.Model
	description  textinput.Model
	source       textinput.Model
	userAgent    textinput.Model
	field        int
	submitting   bool
	settings     *app.ProfileSettings
	firstRow     int
	pageRows     int
	manualScroll bool
}

func newProfileCreateState() profileCreateState {
	name := textinput.New()
	description := textinput.New()
	source := textinput.New()
	userAgent := textinput.New()
	name.CharLimit = configuration.MaxNameRunes
	description.CharLimit = configuration.MaxDescriptionRunes
	source.CharLimit = 8192
	for _, input := range []*textinput.Model{&name, &description, &source, &userAgent} {
		input.Prompt = ""
		input.SetVirtualCursor(false)
	}
	return profileCreateState{name: name, description: description, source: source, userAgent: userAgent}
}

func (p *profileCreateState) remoteSource() bool {
	return configuration.IsRemoteSource(strings.TrimSpace(p.source.Value()))
}

func (p *profileCreateState) effectiveUserAgent() string {
	if p.remoteSource() {
		return p.userAgent.Value()
	}
	return ""
}

func (m *Model) openProfileCreate() tea.Cmd {
	m.openProfileFields("profile-create")
	return textinput.Blink
}

func (m *Model) openProfileFields(form string) {
	m.form = form
	m.failure = ""
	m.profileCreate = newProfileCreateState()
	m.profileCreate.name.Placeholder = m.tr("profiles.create_name_placeholder")
	m.profileCreate.description.Placeholder = m.tr("profiles.create_description_placeholder")
	m.profileCreate.source.Placeholder = m.tr("profiles.create_source_placeholder")
	m.profileCreate.userAgent.Placeholder = m.tr("profiles.user_agent_placeholder")
	styleInput(&m.profileCreate.name, inputBackground, false)
	styleInput(&m.profileCreate.description, inputBackground, false)
	styleInput(&m.profileCreate.source, inputBackground, false)
	styleInput(&m.profileCreate.userAgent, inputBackground, false)
	m.profileCreateSetField(m.profileFieldOrder()[0])
}

func (m *Model) closeProfileCreate() {
	id := m.name
	settings := m.form == "profile-settings"
	m.name = ""
	m.form = ""
	m.failure = ""
	m.profileCreate = profileCreateState{}
	m.paths.reset()
	m.focus = "profiles.create"
	if settings {
		m.focusProfileByID(id)
	}
}

func (m *Model) profileCreateActive() *textinput.Model {
	return m.profileCreate.input(m.profileCreate.field)
}

func (p *profileCreateState) input(field int) *textinput.Model {
	switch field {
	case profileCreateDescription:
		return &p.description
	case profileCreateSource:
		return &p.source
	case profileCreateUserAgent:
		return &p.userAgent
	case profileCreateName:
		return &p.name
	}
	return nil
}

func (m *Model) profileFieldOrder() []int {
	settings := m.form == "profile-settings"
	order := make([]int, 0, 8)
	if settings {
		order = append(order, profileCreateName, profileCreateDescription)
	}
	order = append(order, profileCreateSource)
	if m.profileCreate.remoteSource() || (!settings && strings.TrimSpace(m.profileCreate.source.Value()) == "") {
		order = append(order, profileCreateUserAgent)
	}
	if !settings {
		order = append(order, profileCreateName, profileCreateDescription)
	}
	order = append(order, profileCreateSave, profileCreateCancel)
	if settings {
		order = append(order, profileSettingsImport)
		if m.canUseDefaultFrom(m.name) {
			order = append(order, profileSettingsDeactivate)
		}
	}
	return order
}

func (m *Model) moveProfileField(delta int) {
	order := m.profileFieldOrder()
	i := slices.Index(order, m.profileCreate.field)
	m.profileCreateSetField(order[(i+delta+len(order))%len(order)])
}

func (m *Model) profileCreateSetField(field int) {
	m.profileCreate.manualScroll = false
	m.profileCreate.name.Blur()
	m.profileCreate.description.Blur()
	m.profileCreate.source.Blur()
	m.profileCreate.userAgent.Blur()
	m.profileCreate.field = field
	if input := m.profileCreateActive(); input != nil {
		input.Focus()
	}
	m.paths.reset()
	m.focus = profileCreateFieldID(field)
	if field == profileCreateSource {
		m.profileCreate.source.CursorEnd()
	}
}

func profileCreateFieldID(field int) string {
	switch field {
	case profileCreateDescription:
		return "profile-create.description"
	case profileCreateSource:
		return "profile-create.source"
	case profileCreateUserAgent:
		return "profile-create.user-agent"
	case profileCreateSave:
		return "profile-create.save"
	case profileCreateCancel:
		return "profile-create.cancel"
	case profileSettingsImport:
		return "profile-settings.overrides"
	case profileSettingsDeactivate:
		return "profile-settings.deactivate"
	default:
		return "profile-create.name"
	}
}

func (m *Model) profileCreateKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if !m.profileFieldsForm() {
		return false, nil
	}
	if m.busy || m.quitting {
		if k.String() == "esc" && m.busy {
			m.cancelTask(false)
		}
		return true, nil
	}
	switch k.String() {
	case "pgup", "pgdown":
		if m.form == "profile-settings" {
			step := max(1, m.profileCreate.pageRows)
			if k.String() == "pgup" {
				step = -step
			}
			m.scrollProfileSettings(step)
			return true, nil
		}
	case "esc":
		m.closeProfileCreate()
		m.message = ""
		return true, nil
	case "tab", "shift+tab":
		if k.String() == "tab" && m.profileCreate.field == profileCreateSource {
			if value, ok := m.paths.selectedPath(); ok && value != strings.TrimSpace(m.profileCreate.source.Value()) {
				m.profileCreate.source.SetValue(value)
				m.profileCreate.source.CursorEnd()
				m.paths.reset()
				return true, nil
			}
		}
		delta := 1
		if k.String() == "shift+tab" {
			delta = -1
		}
		m.moveProfileField(delta)
		return true, textinput.Blink
	case "up", "down":
		if m.profileCreate.field == profileCreateSource && m.paths.active {
			delta := 1
			if k.String() == "up" {
				delta = -1
			}
			m.paths.move(delta)
			return true, nil
		}
		delta := 1
		if k.String() == "up" {
			delta = -1
		}
		m.moveProfileField(delta)
		return true, textinput.Blink
	case "enter":
		switch m.profileCreate.field {
		case profileSettingsImport, profileSettingsDeactivate:
			return true, m.profileSettingsAction(m.profileCreate.field)
		case profileCreateCancel:
			m.closeProfileCreate()
			return true, nil
		case profileCreateName, profileCreateDescription, profileCreateUserAgent:
			m.moveProfileField(1)
			return true, textinput.Blink
		}
		if handled, _ := m.profileCreatePathEnter(); handled {
			return true, nil
		}
		return true, m.submitProfileCreate()
	}
	var cmd tea.Cmd
	input := m.profileCreateActive()
	if input == nil {
		return true, nil
	}
	m.profileCreate.manualScroll = false
	before := input.Value()
	*input, cmd = input.Update(k)
	if before != input.Value() {
		m.failure = ""
		if m.profileCreate.field == profileCreateSource {
			m.paths.reset()
		}
	}
	return true, cmd
}

func (m *Model) profileCreatePathEnter() (bool, tea.Cmd) {
	if m.profileCreate.field != profileCreateSource {
		return false, nil
	}
	value := strings.TrimSpace(m.profileCreate.source.Value())
	if value == "" || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return false, nil
	}
	path, err := resolveLocalPath(value)
	if err != nil {
		m.failure = m.localizeError(err)
		return true, nil
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		if !strings.HasSuffix(value, "/") {
			m.profileCreate.source.SetValue(value + "/")
			m.profileCreate.source.CursorEnd()
		}
		return true, nil
	}
	return false, nil
}

func (m *Model) submitProfileCreate() tea.Cmd {
	if m.busy || m.quitting || m.profileCreate.submitting {
		return nil
	}
	if m.form == "profile-settings" {
		return m.saveProfileSettings()
	}
	draft := app.ProfileDraft{
		Name:           strings.TrimSpace(m.profileCreate.name.Value()),
		Description:    strings.TrimSpace(m.profileCreate.description.Value()),
		Address:        strings.TrimSpace(m.profileCreate.source.Value()),
		UserAgent:      m.profileCreate.effectiveUserAgent(),
		RememberSource: true,
	}
	source, err := resolveLocalPath(draft.Address)
	if err == nil {
		err = validateProfileSource(source)
	}
	if err != nil {
		m.profileCreateSetField(profileCreateSource)
		if draft.Address == "" {
			m.failure = m.tr("profiles.value_required")
		} else {
			m.failure = m.localizeError(err)
		}
		return nil
	}
	draft.Address = source
	m.failure = ""
	m.profileCreate.submitting = true
	return m.beginProfileImport(draft, "profile.create", m.tr("profiles.creating"))
}

func (m *Model) profileCreateMouse(id string) tea.Cmd {
	if !m.profileFieldsForm() || m.busy || m.quitting {
		return nil
	}
	switch id {
	case "profile-create.name":
		m.profileCreateSetField(profileCreateName)
	case "profile-create.description":
		m.profileCreateSetField(profileCreateDescription)
	case "profile-create.source":
		m.profileCreateSetField(profileCreateSource)
	case "profile-create.user-agent":
		m.profileCreateSetField(profileCreateUserAgent)
	case "profile-settings.overrides":
		return m.profileSettingsAction(profileSettingsImport)
	case "profile-settings.deactivate":
		return m.profileSettingsAction(profileSettingsDeactivate)
	case "profile-create.cancel":
		m.closeProfileCreate()
	case "profile-create.save":
		return m.submitProfileCreate()
	}
	return textinput.Blink
}
