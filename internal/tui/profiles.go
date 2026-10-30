package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/store"
)

type profilesState struct {
	url       textinput.Model
	editing   bool
	detail    string
	importing bool
}

func newProfilesState() profilesState {
	input := textinput.New()
	input.CharLimit = 8192
	input.Placeholder = "Paste subscription URL"
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
	return true, cmd
}

func (m *Model) beginProfileURLImport() tea.Cmd {
	source := strings.TrimSpace(m.profilesUI.url.Value())
	if source == "" {
		m.focus, m.profilesUI.editing = "profiles.url", true
		return m.profilesUI.url.Focus()
	}
	m.profilesUI.editing = false
	m.profilesUI.url.Blur()
	m.profilesUI.importing = true
	m.focus = "profiles.add"
	return m.begin("Fetching and validating subscription…", "Subscription saved; select its card to apply", func(ctx context.Context) error {
		return m.backend.AddSubscription(ctx, "", source)
	})
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
	name := m.profilesUI.detail
	if name == "" {
		return
	}
	for i, profile := range m.profiles() {
		if profile != name {
			continue
		}
		for _, prefix := range []string{"profiles.apply.", "profiles.update.", "profiles.rename.", "profiles.url-edit.", "profiles.overrides.", "profiles.delete."} {
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
	case "subscription.add":
		cmd := m.openForm()
		m.rememberSource = true
		m.failure = ""
		return true, cmd
	case "profiles.update-all":
		return true, m.begin("Updating profiles…", "Profiles updated; apply a profile to load changes", m.backend.RefreshAllProfiles)
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
		{"profiles.rename.", "rename"}, {"profiles.url-edit.", "url-edit"},
		{"profiles.delete.", "delete"}, {"profile.delete.", "delete"},
	} {
		i, ok := profileActionIndex(id, action.prefix, len(profiles))
		if !ok {
			continue
		}
		name := profiles[i]
		switch action.operation {
		case "apply":
			return true, m.begin("Validating and applying configuration…", "Configuration applied", func(ctx context.Context) error { return m.backend.Apply(ctx, name) })
		case "refresh":
			return true, m.begin("Updating profile…", "Profile updated; apply the profile to load changes", func(ctx context.Context) error { return m.backend.Refresh(ctx, name) })
		case "overrides":
			return true, m.openProfileEdit("profile-overrides", name)
		case "rename":
			return true, m.openProfileEdit("profile-rename", name)
		case "url-edit":
			return true, m.openProfileEdit("profile-url", name)
		case "delete":
			return true, m.openProfileEdit("profile-delete", name)
		}
	}
	return true, nil
}

func (m Model) profileEditForm() bool {
	return m.form == "profile-rename" || m.form == "profile-url" || m.form == "profile-overrides" || m.form == "profile-delete"
}

func (m *Model) openProfileEdit(form, name string) tea.Cmd {
	m.form, m.name, m.rememberSource = form, name, false
	m.failure = ""
	m.input.Reset()
	m.input.EchoMode = textinput.EchoNormal
	m.input.Placeholder = "Enter the exact name to confirm deletion"
	if form == "profile-rename" {
		m.input.Placeholder = "Display name"
		m.input.SetValue(m.profileInfo(name).Name)
	}
	if form == "profile-url" {
		m.input.Placeholder = "New HTTP(S) URL or absolute YAML path"
	}
	if form == "profile-overrides" {
		m.input.Placeholder = "Absolute path to overrides YAML"
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
		m.message = "Input canceled"
		return true, nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		if value == "" {
			m.failure = "A value is required"
			return true, nil
		}
		form, name := m.form, m.name
		expected := name
		if form == "profile-delete" {
			expected = m.profileInfo(name).Name
		}
		if form == "profile-delete" && value != expected {
			m.failure = "Enter the exact name to confirm deletion"
			return true, nil
		}
		m.closeProfileForm()
		switch form {
		case "profile-rename":
			return true, m.begin("Renaming profile…", "Profile name saved", func(ctx context.Context) error { return m.backend.RenameProfile(ctx, name, value) })
		case "profile-url":
			return true, m.begin("Updating subscription address…", "Subscription saved; apply the profile to load changes", func(ctx context.Context) error { return m.backend.UpdateProfileAddress(ctx, name, value) })
		case "profile-overrides":
			return true, m.begin("Importing profile overrides…", "Overrides saved; apply the profile to load changes", func(ctx context.Context) error {
				raw, err := readProfileOverrides(value)
				if err != nil {
					return err
				}
				return m.backend.SetProfileOverrides(ctx, name, raw)
			})
		case "profile-delete":
			return true, m.begin("Deleting profile recipe…", "Profile recipe deleted", func(ctx context.Context) error {
				return m.backend.DeleteProfile(ctx, name)
			})
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return true, cmd
}

func readProfileOverrides(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("overrides input must be an absolute file path")
	}
	raw, err := store.Read(path)
	if err != nil {
		return nil, errors.New("cannot read overrides; use a regular file of at most 8 MiB")
	}
	if len(raw) > 8<<20 {
		return nil, errors.New("overrides input exceeds 8 MiB")
	}
	return raw, nil
}

func (m *Model) renderProfileForm(base string) (string, []hitRegion) {
	boxW, boxH := min(76, m.width), min(12, m.height)
	x, y := (m.width-boxW)/2, (m.height-boxH)/2
	if boxW < 24 || boxH < 7 {
		return overlay("", "Enlarge terminal · Esc Back", 0, 0, m.width, m.height), nil
	}
	title, label, hint := "Add subscription", "Subscription URL or local YAML path", "The private source URL/path will be saved for refresh."
	if m.form == "source" && !m.rememberSource {
		title, hint = "Import configuration", "Imports a copy; the original file is left unchanged."
	}
	switch m.form {
	case "profile-rename":
		title, label = "Rename profile · "+clean(m.profileInfo(m.name).Name), "Display name"
		hint = "Your name is retained when the subscription updates."
	case "profile-url":
		title, label = "Edit subscription · "+clean(m.profileInfo(m.name).Name), "New subscription URL or local YAML path"
		hint = "Downloads and validates before saving; apply changes separately."
	case "profile-overrides":
		title, label = "Import overrides · "+clean(m.profileInfo(m.name).Name), "Absolute path to overrides YAML"
		hint = "Saves a draft; apply the profile separately to load it."
	case "profile-delete":
		title, label = "Delete recipe · "+clean(m.profileInfo(m.name).Name), "Enter the exact profile name to confirm"
		hint = "The selected profile is protected by the application."
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
	footer := "Enter Continue / Save · Esc Cancel"
	lines = append(lines, "", themed(cellCrop(footer, boxW-4), muted, raised, false))
	if m.failure != "" {
		lines = append(lines, themed(cellCrop(m.failure, boxW-4), errorColor, raised, false))
	}
	box := lipgloss.NewStyle().Background(lipgloss.Color(raised)).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(focus)).Width(boxW).Height(boxH).Padding(0, 1).Render(strings.Join(lines, "\n"))
	return overlay(base, themed(box, text, raised, false), x, y, m.width, m.height), []hitRegion{{ID: "import-input", X: x + 2, Y: y + inputRow, W: max(1, boxW-4), H: 1}}
}
