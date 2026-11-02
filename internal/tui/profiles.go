package tui

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/store"
)

type profilesState struct {
	url           textinput.Model
	urlError      string
	urlErrorValue string
	editing       bool
	detail        string
	importing     bool
	menuProfile   string
}

func newProfilesState(locales ...i18n.Localizer) profilesState {
	var locale i18n.Localizer
	if len(locales) > 0 {
		locale = locales[0]
	}
	input := textinput.New()
	input.CharLimit = 8192
	input.Placeholder = locale.T("profiles.url_placeholder")
	styleInput(&input, inputBackground, false)
	return profilesState{url: input}
}

func (m *Model) profileURLKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.page != "profiles" || m.focus != "profiles.url" || m.form != "" || m.sidebarOpen || m.busy || m.quitting {
		return false, nil
	}
	m.profilesUI.editing = true
	m.profilesUI.url.Focus()
	switch k.String() {
	case "tab", "shift+tab":
		m.profilesUI.editing = false
		m.profilesUI.url.Blur()
		return false, nil
	case "esc":
		m.profilesUI.editing = false
		m.profilesUI.url.Blur()
		m.focus = "profiles.add"
		return true, nil
	case "enter":
		return true, m.beginProfileURLImport()
	}
	var cmd tea.Cmd
	m.profilesUI.url, cmd = m.profilesUI.url.Update(k)
	m.reconcileProfileValidation()
	return true, cmd
}

func (m *Model) beginProfileURLImport() tea.Cmd {
	source := strings.TrimSpace(m.profilesUI.url.Value())
	issue := ""
	if source == "" {
		issue = m.tr("profiles.value_required")
	} else if err := validateProfileSource(source); err != nil {
		issue = m.localizeError(err)
	}
	if issue != "" {
		m.profilesUI.urlError, m.profilesUI.urlErrorValue = issue, m.profilesUI.url.Value()
		m.focus, m.profilesUI.editing = "profiles.url", true
		return m.profilesUI.url.Focus()
	}
	m.clearProfileValidation()
	m.profilesUI.editing = false
	m.profilesUI.url.Blur()
	m.profilesUI.importing = true
	m.focus = "profiles.add"
	return m.begin(m.tr("profiles.fetching"), m.tr("profiles.saved_select"), func(ctx context.Context) error {
		return m.backend.AddSubscription(ctx, "", source)
	}, notificationOperation{Key: notificationKey("profile.add", source), Context: m.tr("profiles.fetching")})
}

func validateProfileSource(source string) error {
	if filepath.IsAbs(source) {
		return nil
	}
	if !strings.HasPrefix(source, "https://") && !strings.HasPrefix(source, "http://") {
		return diagnostic.New("diagnostic.source.address", "subscription source must be an HTTP(S) URL or absolute file path")
	}
	u, err := url.ParseRequestURI(source)
	if err != nil || u.Hostname() == "" {
		return diagnostic.New("diagnostic.source.invalidURL", "invalid subscription URL")
	}
	return nil
}

func (m *Model) clearProfileValidation() {
	m.profilesUI.urlError, m.profilesUI.urlErrorValue = "", ""
}

func (m *Model) reconcileProfileValidation() {
	if m.page != "profiles" || m.profilesUI.url.Value() != m.profilesUI.urlErrorValue {
		m.clearProfileValidation()
	}
}

func (m *Model) focusProfileControl(id string) {
	m.focus = id
	_, hits := m.pageBody(dashboardBodyWidth(dashboardOptions{Width: m.width, Height: m.height, Page: m.page}))
	for _, hit := range hits {
		if hit.ID == id {
			m.revealBodyTarget(hit)
			break
		}
	}
}

// Detail actions follow a profile's identity when another interface changes the
// library order. A stale numeric focus must never apply a different profile.
func (m *Model) reconcileProfiles() {
	m.reconcileProfileValidation()
	name := m.profilesUI.detail
	if name == "" {
		return
	}
	for i, profile := range m.profiles() {
		if profile != name {
			continue
		}
		for _, prefix := range []string{"profiles.apply.", "profiles.update.", "profiles.rename.", "profiles.description.", "profiles.url-edit.", "profiles.overrides.", "profiles.delete."} {
			if strings.HasPrefix(m.focus, prefix) {
				m.focus = fmt.Sprintf("%s%d", prefix, i)
			}
		}
		return
	}
	m.profilesUI.detail = ""
	if m.page == "profiles" && strings.HasPrefix(m.focus, "profiles.") {
		m.focusProfileControl("profile.0")
	}
}

func profileActionIndex(id, prefix string, size int) (int, bool) {
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, prefix))
	return n, err == nil && n >= 0 && n < size
}

// Mouse clicks apply cards directly; keyboard activation keeps details reachable.
func (m *Model) profileCardClick(id string) (bool, tea.Cmd) {
	index, card := profileActionIndex(id, "profile.", len(m.profiles()))
	if m.page != "profiles" || !card {
		return false, nil
	}
	if m.busy || m.quitting {
		return true, nil
	}
	if !m.snapshot.Installed {
		return m.profileAction(id)
	}
	m.profilesUI.editing = false
	m.profilesUI.url.Blur()
	name := m.profiles()[index]
	if name == m.snapshot.Applied && !m.profileInfo(name).Pending && !m.snapshot.ProfilePending {
		return true, nil
	}
	m.profilesUI.detail = ""
	return m.profileAction(fmt.Sprintf("profiles.apply.%d", index))
}

// profileAction routes interface actions into shared app use cases. It does
// not recreate schema or active-profile validation.
func (m *Model) profileAction(id string) (bool, tea.Cmd) {
	index, card := profileActionIndex(id, "profile.", len(m.profiles()))
	if !card && id != "subscription.add" && !strings.HasPrefix(id, "profiles.") &&
		!strings.HasPrefix(id, "profile.overrides.") && !strings.HasPrefix(id, "profile.delete.") {
		return false, nil
	}
	if m.busy || m.quitting {
		return true, nil
	}
	if index, ok := profileActionIndex(id, "profiles.menu.", len(m.profiles())); ok {
		m.openProfileMenuForCard(index)
		return true, nil
	}
	if id == "profiles.url" {
		m.focus, m.profilesUI.editing = id, true
		return true, m.profilesUI.url.Focus()
	}
	m.profilesUI.editing = false
	m.profilesUI.url.Blur()
	if card {
		name := m.profiles()[index]
		m.profilesUI.detail = name
		target := fmt.Sprintf("profiles.apply.%d", index)
		if !m.snapshot.Installed {
			target = "profiles.close"
		}
		m.focusProfileControl(target)
		return true, nil
	}
	switch id {
	case "profiles.add":
		return true, m.beginProfileURLImport()
	case "profiles.create":
		return true, m.openProfileCreate()
	case "subscription.add":
		cmd := m.openForm()
		m.rememberSource = true
		m.failure = ""
		return true, cmd
	case "profiles.update-all":
		return true, m.begin(m.tr("profiles.updating_all"), m.tr("profiles.updated_all"), m.backend.RefreshAllProfiles,
			notificationOperation{Key: "profile.refresh-all", Context: m.tr("profiles.updating_all")})
	case "profiles.close":
		name := m.profilesUI.detail
		m.profilesUI.detail = ""
		for i, profile := range m.profiles() {
			if profile == name {
				m.focusProfileControl(fmt.Sprintf("profile.%d", i))
				break
			}
		}
		return true, nil
	}
	profiles := m.profiles()
	for _, action := range []struct{ prefix, operation string }{
		{"profiles.apply.", "apply"}, {"profiles.refresh.", "refresh"}, {"profiles.update.", "refresh"},
		{"profiles.overrides.", "overrides"}, {"profile.overrides.", "overrides"},
		{"profiles.rename.", "rename"}, {"profiles.description.", "description"}, {"profiles.url-edit.", "url-edit"},
		{"profiles.delete.", "delete"}, {"profile.delete.", "delete"},
	} {
		i, ok := profileActionIndex(id, action.prefix, len(profiles))
		if !ok {
			continue
		}
		name := profiles[i]
		switch action.operation {
		case "apply":
			return true, m.begin(m.tr("profiles.applying"), m.tr("profiles.applied"), func(ctx context.Context) error { return m.backend.Apply(ctx, name) },
				m.profileNotification("apply", m.tr("profiles.applying"), name))
		case "refresh":
			return true, m.begin(m.tr("profiles.updating"), m.tr("profiles.updated"), func(ctx context.Context) error { return m.backend.Refresh(ctx, name) },
				m.profileNotification("refresh", m.tr("profiles.updating"), name))
		case "overrides":
			return true, m.openProfileEdit("profile-overrides", name)
		case "rename":
			return true, m.openProfileEdit("profile-rename", name)
		case "description":
			return true, m.openProfileEdit("profile-description", name)
		case "url-edit":
			return true, m.openProfileEdit("profile-url", name)
		case "delete":
			return true, m.openProfileEdit("profile-delete", name)
		}
	}
	return true, nil
}

func (m Model) profileNotification(operation, label, name string) notificationOperation {
	return notificationOperation{
		Key:     notificationKey("profile."+operation, name),
		Context: label + " · " + clean(snapshotProfileName(m.snapshot, name, m.locale)),
	}
}

func (m Model) profileEditForm() bool {
	return m.form == "profile-rename" || m.form == "profile-description" || m.form == "profile-url" || m.form == "profile-overrides" || m.form == "profile-delete"
}

func (m *Model) openProfileEdit(form, name string) tea.Cmd {
	m.form, m.name, m.rememberSource = form, name, false
	m.failure = ""
	m.input.Reset()
	m.paths.reset()
	m.input.EchoMode = textinput.EchoNormal
	m.input.Placeholder = m.tr("profiles.delete_placeholder")
	if form == "profile-rename" {
		m.input.Placeholder = m.tr("profiles.display_name")
		m.input.SetValue(m.profileInfo(name).Name)
	}
	if form == "profile-description" {
		m.input.Placeholder = m.tr("profiles.description")
		m.input.SetValue(m.profileInfo(name).Description)
	}
	if form == "profile-url" {
		m.input.Placeholder = m.tr("profiles.address_placeholder")
	}
	if form == "profile-overrides" {
		m.input.Placeholder = m.tr("profiles.overrides_path")
	}
	m.input.Focus()
	return textinput.Blink
}

func (m *Model) closeProfileForm() {
	m.form, m.name, m.rememberSource = "", "", false
	m.input.Reset()
	m.input.Blur()
}

func (m *Model) profileFormKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	if !m.profileEditForm() {
		return false, nil
	}
	switch k.String() {
	case "esc":
		m.closeProfileForm()
		m.message = m.tr("profiles.input_canceled")
		m.prepareNotification("input.cancel", m.tr("profiles.input_canceled"))
		m.messageTransient = false
		m.messageInformational = true
		m.failure = ""
		return true, nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		if value == "" && m.form != "profile-description" {
			m.failure = m.tr("profiles.value_required")
			return true, nil
		}
		form, name := m.form, m.name
		if m.pathForm() {
			var err error
			value, err = resolveLocalPath(value)
			if err != nil {
				m.failure = m.localizeError(err)
				return true, nil
			}
		}
		if form == "profile-url" {
			if err := validateProfileSource(value); err != nil {
				m.failure = m.localizeError(err)
				return true, nil
			}
		}
		expected := name
		if form == "profile-delete" {
			expected = m.profileInfo(name).Name
		}
		if form == "profile-delete" && value != expected {
			m.failure = m.tr("profiles.delete_placeholder")
			return true, nil
		}
		m.closeProfileForm()
		switch form {
		case "profile-rename":
			return true, m.begin(m.tr("profiles.renaming"), m.tr("profiles.name_saved"), func(ctx context.Context) error { return m.backend.RenameProfile(ctx, name, value) },
				m.profileNotification("rename", m.tr("profiles.renaming"), name))
		case "profile-description":
			return true, m.begin(m.tr("profiles.updating_description"), m.tr("profiles.description_saved"), func(ctx context.Context) error { return m.backend.UpdateProfileDescription(ctx, name, value) },
				m.profileNotification("description", m.tr("profiles.updating_description"), name))
		case "profile-url":
			return true, m.begin(m.tr("profiles.updating_address"), m.tr("profiles.address_saved"), func(ctx context.Context) error { return m.backend.UpdateProfileAddress(ctx, name, value) },
				m.profileNotification("address", m.tr("profiles.updating_address"), name))
		case "profile-overrides":
			return true, m.begin(m.tr("profiles.importing_overrides"), m.tr("profiles.overrides_saved"), func(ctx context.Context) error {
				raw, err := readProfileOverrides(value)
				if err != nil {
					return err
				}
				return m.backend.SetProfileOverrides(ctx, name, raw)
			}, m.profileNotification("overrides", m.tr("profiles.importing_overrides"), name))
		case "profile-delete":
			return true, m.begin(m.tr("profiles.deleting"), m.tr("profiles.deleted"), func(ctx context.Context) error {
				return m.backend.DeleteProfile(ctx, name)
			}, m.profileNotification("delete", m.tr("profiles.deleting"), name))
		}
	}
	var cmd tea.Cmd
	before := m.input.Value()
	m.input, cmd = m.input.Update(k)
	if before != m.input.Value() {
		m.failure = ""
	}
	return true, cmd
}

func readProfileOverrides(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, diagnostic.New("diagnostic.overrides_absolute", "overrides input must be an absolute file path")
	}
	raw, err := store.Read(path)
	if err != nil {
		return nil, diagnostic.New("diagnostic.overrides_read", "cannot read overrides; use a regular file of at most 8 MiB")
	}
	if len(raw) > 8<<20 {
		return nil, diagnostic.New("diagnostic.overrides_size", "overrides input exceeds 8 MiB")
	}
	return raw, nil
}

func (m *Model) renderProfileForm(base string) (string, []hitRegion) {
	if m.form == "profile-create" {
		return m.renderProfileCreateForm(base)
	}
	boxW, boxH := min(76, m.width), min(12, m.height)
	if m.pathForm() {
		boxH = min(18, m.height)
	}
	x, y := (m.width-boxW)/2, (m.height-boxH)/2
	if boxW < 24 || boxH < 7 {
		return overlay("", m.tr("profiles.enlarge"), 0, 0, m.width, m.height), nil
	}
	title, label, hint := m.tr("profiles.add_subscription"), m.tr("profiles.source_label"), m.tr("profiles.source_hint")
	if m.form == "source" && !m.rememberSource {
		title, hint = m.tr("profiles.import_title"), m.tr("profiles.import_hint")
	}
	switch m.form {
	case "profile-rename":
		title, label = m.tr("profiles.rename_title", clean(m.profileInfo(m.name).Name)), m.tr("profiles.display_name")
		hint = m.tr("profiles.name_hint")
	case "profile-description":
		title, label = m.tr("profiles.description_title", clean(m.profileInfo(m.name).Name)), m.tr("profiles.description")
		hint = m.tr("profiles.description_hint")
	case "profile-url":
		title, label = m.tr("profiles.address_title", clean(m.profileInfo(m.name).Name)), m.tr("profiles.new_address")
		hint = m.tr("profiles.address_hint")
	case "profile-overrides":
		title, label = m.tr("profiles.overrides_title", clean(m.profileInfo(m.name).Name)), m.tr("profiles.overrides_path")
		hint = m.tr("profiles.overrides_hint")
	case "profile-delete":
		title, label = m.tr("profiles.delete_title", clean(m.profileInfo(m.name).Name)), m.tr("profiles.delete_label")
		hint = m.tr("profiles.delete_hint")
	}
	m.input.SetWidth(max(1, boxW-7-ansi.StringWidth(m.input.Prompt)))
	m.input.SetCursor(m.input.Position())
	styleInput(&m.input, inputBackground, m.failure != "")
	lines := []string{
		themed(cellCrop(title, boxW-4), text, raised, false),
		themed(cellCrop(hint, boxW-4), muted, raised, false),
		cellCrop(label, boxW-4), "",
		themed(m.input.View(), text, inputBackground, false),
	}
	inputRow := 5
	if boxH < 10 {
		lines = []string{themed(cellCrop(title, boxW-4), text, raised, false), themed(m.input.View(), text, inputBackground, false)}
		inputRow = 2
	}
	footer := m.tr("profiles.form_footer")
	if m.pathForm() {
		rows := max(0, boxH-len(lines)-5)
		lines = append(lines, m.paths.view(boxW-4, rows, m.locale)...)
		footer = m.tr("profiles.path_footer")
		if boxW < 60 {
			footer = m.tr("profiles.path_footer_short")
		}
		if boxW < 36 {
			footer = m.tr("profiles.path_footer_tiny")
		}
	}
	lines = append(lines, "", themed(cellCrop(footer, boxW-4), muted, raised, false))
	if m.failure != "" {
		lines = append(lines, themed(cellCrop(m.failure, boxW-4), errorColor, raised, false))
	}
	box := lipgloss.NewStyle().Background(lipgloss.Color(raised)).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(focus)).Width(boxW).Height(boxH).Padding(0, 1).Render(strings.Join(lines, "\n"))
	return overlay(base, themed(box, text, raised, false), x, y, m.width, m.height), []hitRegion{{ID: "import-input", X: x + 2, Y: y + inputRow, W: max(1, boxW-4), H: 1}}
}
