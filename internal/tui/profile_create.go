package tui

import (
	"context"
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/configuration"
)

const (
	profileCreateName = iota
	profileCreateDescription
	profileCreateSource
	profileCreateSave
	profileCreateCancel
	profileCreateControls
)

type profileCreateState struct {
	name        textinput.Model
	description textinput.Model
	source      textinput.Model
	field       int
	submitting  bool
}

func newProfileCreateState() profileCreateState {
	name := textinput.New()
	description := textinput.New()
	source := textinput.New()
	name.CharLimit = configuration.MaxNameRunes
	description.CharLimit = configuration.MaxDescriptionRunes
	source.CharLimit = 8192
	name.Prompt, description.Prompt, source.Prompt = "", "", ""
	return profileCreateState{name: name, description: description, source: source}
}

func (m *Model) openProfileCreate() tea.Cmd {
	m.form = "profile-create"
	m.failure = ""
	m.profileCreate = newProfileCreateState()
	m.profileCreate.name.Placeholder = m.tr("profiles.create_name_placeholder")
	m.profileCreate.description.Placeholder = m.tr("profiles.create_description_placeholder")
	m.profileCreate.source.Placeholder = m.tr("profiles.create_source_placeholder")
	styleInput(&m.profileCreate.name, inputBackground, false)
	styleInput(&m.profileCreate.description, inputBackground, false)
	styleInput(&m.profileCreate.source, inputBackground, false)
	m.profileCreate.field = profileCreateName
	m.profileCreate.name.Focus()
	m.paths.reset()
	m.focus = "profile-create.name"
	return textinput.Blink
}

func (m *Model) closeProfileCreate() {
	m.form = ""
	m.failure = ""
	m.profileCreate = profileCreateState{}
	m.paths.reset()
	m.focus = "profiles.create"
}

func (m *Model) profileCreateActive() *textinput.Model {
	switch m.profileCreate.field {
	case profileCreateDescription:
		return &m.profileCreate.description
	case profileCreateSource:
		return &m.profileCreate.source
	case profileCreateName:
		return &m.profileCreate.name
	}
	return nil
}

func (m *Model) profileCreateSetField(field int) {
	field = (field + profileCreateControls) % profileCreateControls
	m.profileCreate.name.Blur()
	m.profileCreate.description.Blur()
	m.profileCreate.source.Blur()
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
	case profileCreateSave:
		return "profile-create.save"
	case profileCreateCancel:
		return "profile-create.cancel"
	default:
		return "profile-create.name"
	}
}

func (m *Model) profileCreateKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.form != "profile-create" {
		return false, nil
	}
	if m.busy || m.quitting {
		if k.String() == "esc" && m.busy {
			m.cancelTask(false)
		}
		return true, nil
	}
	switch k.String() {
	case "esc":
		m.closeProfileCreate()
		m.prepareNotification("input.cancel", m.tr("profiles.input_canceled"))
		m.message = m.tr("profiles.input_canceled")
		m.messageInformational = true
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
		m.profileCreateSetField(m.profileCreate.field + delta)
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
		m.profileCreateSetField(m.profileCreate.field + delta)
		return true, textinput.Blink
	case "enter":
		switch m.profileCreate.field {
		case profileCreateCancel:
			m.closeProfileCreate()
			return true, nil
		case profileCreateName, profileCreateDescription:
			m.profileCreateSetField(m.profileCreate.field + 1)
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
	draft := app.ProfileDraft{
		Name:           strings.TrimSpace(m.profileCreate.name.Value()),
		Description:    strings.TrimSpace(m.profileCreate.description.Value()),
		Address:        strings.TrimSpace(m.profileCreate.source.Value()),
		RememberSource: true,
	}
	if draft.Address == "" {
		m.failure = m.tr("profiles.value_required")
		return nil
	}
	source, err := resolveLocalPath(draft.Address)
	if err != nil {
		m.failure = m.localizeError(err)
		return nil
	}
	if err := validateProfileSource(source); err != nil {
		m.failure = m.localizeError(err)
		return nil
	}
	draft.Address = source
	m.failure = ""
	m.profileCreate.submitting = true
	return m.begin(m.tr("profiles.creating"), m.tr("profiles.created"), func(ctx context.Context) error {
		return m.backend.CreateProfile(ctx, draft)
	}, notificationOperation{Key: notificationKey("profile.create", draft.Address), Context: m.tr("profiles.creating")})
}

func (m *Model) profileCreateMouse(id string) tea.Cmd {
	if m.form != "profile-create" || m.busy || m.quitting {
		return nil
	}
	switch id {
	case "profile-create.name":
		m.profileCreateSetField(profileCreateName)
	case "profile-create.description":
		m.profileCreateSetField(profileCreateDescription)
	case "profile-create.source":
		m.profileCreateSetField(profileCreateSource)
	case "profile-create.cancel":
		m.closeProfileCreate()
	case "profile-create.save":
		return m.submitProfileCreate()
	}
	return textinput.Blink
}

func (m Model) profileCreateType() string {
	value := strings.TrimSpace(m.profileCreate.source.Value())
	if value == "" {
		return m.tr("profiles.create_unknown")
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return m.tr("profiles.create_remote")
	}
	return m.tr("profiles.create_local")
}

func profileCreateInputView(input *textinput.Model, width int, invalid bool) string {
	styleInput(input, inputBackground, invalid)
	input.SetWidth(max(1, width-2-ansi.StringWidth(input.Prompt)))
	input.SetCursor(input.Position())
	return input.View()
}

func (m Model) renderProfileCreateForm(base string) (string, []hitRegion) {
	boxW := min(112, max(1, m.width-2))
	boxH := min(20, max(1, m.height-2))
	if boxW < 38 || boxH < 12 {
		return overlay(base, m.tr("profiles.enlarge"), 0, 0, m.width, m.height), nil
	}
	inner := boxW - 4
	m.profileCreate.name.SetWidth(max(1, inner-2))
	m.profileCreate.description.SetWidth(max(1, inner-2))
	m.profileCreate.source.SetWidth(max(1, inner-2))
	m.profileCreate.name.SetCursor(m.profileCreate.name.Position())
	m.profileCreate.description.SetCursor(m.profileCreate.description.Position())
	m.profileCreate.source.SetCursor(m.profileCreate.source.Position())
	name := profileCreateInputView(&m.profileCreate.name, inner, false)
	description := profileCreateInputView(&m.profileCreate.description, inner, false)
	source := profileCreateInputView(&m.profileCreate.source, inner, m.failure != "" && m.profileCreate.field == profileCreateSource)
	x, y := (m.width-boxW)/2, (m.height-boxH)/2
	lines := []string{themed(cellCrop(m.tr("profiles.create_title"), inner), text, raised, false)}
	if boxH >= 16 {
		lines = append(lines, themed(cellCrop(m.tr("profiles.create_hint"), inner), muted, raised, false))
	}
	if boxH >= 13 {
		lines = append(lines, themed(cellCrop(m.tr("profiles.create_type", m.profileCreateType()), inner), muted, raised, false))
	}
	var targets []hitRegion
	for _, field := range []struct{ id, label, value string }{
		{"profile-create.name", m.tr("profiles.create_name"), name},
		{"profile-create.description", m.tr("profiles.create_description"), description},
		{"profile-create.source", m.tr("profiles.create_source"), source},
	} {
		lines = append(lines, cellCrop(field.label, inner), field.value)
		targets = append(targets, hitRegion{ID: field.id, X: x + 2, Y: y + len(lines), W: inner, H: 1})
	}
	// Always reserve visible rows for the actions, keyboard hint and result.
	// Completion rows use the remaining space, so resizing cannot hide buttons
	// while leaving their mouse targets over unrelated content.
	if m.profileCreate.field == profileCreateSource {
		rows := max(0, boxH-2-len(lines)-3)
		lines = append(lines, m.paths.view(inner, rows, m.locale)...)
	}
	buttonRow := len(lines)
	createButton := renderButton(m.tr("profiles.create_save"), buttonOptions{Kind: buttonPrimary, Surface: raised,
		Focused: m.focus == "profile-create.save", Hovered: m.hover == "profile-create.save", Busy: m.busy,
		Disabled: m.quitting, Plain: m.buttonPlain()})
	cancelButton := renderButton(m.tr("profiles.create_cancel"), buttonOptions{Surface: raised,
		Focused: m.focus == "profile-create.cancel", Hovered: m.hover == "profile-create.cancel",
		Disabled: m.busy || m.quitting, Plain: m.buttonPlain()})
	lines = append(lines, cancelButton+"  "+createButton, themed(cellCrop(m.tr("profiles.create_footer"), inner), muted, raised, false))
	if m.failure != "" {
		lines = append(lines, themed(cellCrop(m.failure, inner), errorColor, raised, false))
	} else if m.busy {
		lines = append(lines, themed(cellCrop(m.spinner.View()+" "+m.message, inner), accent, raised, false))
	}
	box := lipgloss.NewStyle().Background(lipgloss.Color(raised)).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(focus)).Width(boxW).Height(boxH).Padding(0, 1).Render(strings.Join(lines, "\n"))
	if m.busy || m.quitting {
		targets = nil
	} else {
		targets = append(targets,
			hitRegion{ID: "profile-create.cancel", X: x + 2, Y: y + buttonRow + 1, W: ansi.StringWidth(cancelButton), H: 1},
			hitRegion{ID: "profile-create.save", X: x + 2 + ansi.StringWidth(cancelButton) + 2, Y: y + buttonRow + 1, W: ansi.StringWidth(createButton), H: 1})
	}
	return overlay(base, themed(box, text, raised, false), x, y, m.width, m.height), targets
}
