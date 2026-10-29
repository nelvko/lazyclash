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
	"github.com/nelvko/lazyclash/internal/store"
)

func (m Model) profilesBody(width int) (string, []hitRegion) {
	var lines []string
	var hits []hitRegion
	explain := func(value string) {
		lines = append(lines, strings.Split(wrapText(value, max(1, width)), "\n")...)
	}
	add := func(id, label string) {
		row := len(lines)
		prefix, foreground, background := "  ", text, bg
		if m.focus == id {
			prefix, foreground = "> ", focus
		}
		if m.hover == id {
			background = hoverBackground
		}
		lines = append(lines, themed(cellCrop(prefix+clean(label), width), foreground, background, false))
		hits = append(hits, hitRegion{ID: id, Y: row, W: width, H: 1})
	}
	explain("Saved configurations")
	add("add", "One-time import (URL or local YAML)")
	add("subscription.add", "Add subscription (remember source)")
	explain("Add subscription saves the private URL/path for refresh. Imports and edits do not activate a profile.")
	if !m.snapshot.Installed {
		explain("You may import now. Engine validation runs before application after core installation.")
	}
	lines = append(lines, "")
	for i, name := range m.profiles() {
		mark := ""
		if name == m.snapshot.Applied {
			mark = " [applied]"
			if m.snapshot.ProfilePending {
				mark += " [changes pending; apply to load]"
			}
		}
		for _, pending := range m.snapshot.UnvalidatedProfiles {
			if pending == name {
				mark += " [engine validation pending]"
				break
			}
		}
		add(fmt.Sprintf("profile.%d", i), name+mark+" · Apply")
		if name != "default" {
			add(fmt.Sprintf("profile.overrides.%d", i), "    Import overrides from file · "+name)
			add(fmt.Sprintf("profile.delete.%d", i), "    Delete recipe · "+name)
		}
	}
	lines = append(lines, "")
	explain("Subscription sources")
	if len(m.snapshot.Sources) == 0 {
		explain("No subscription sources saved.")
	}
	for i, source := range m.snapshot.Sources {
		explain(clean(source.Name))
		if source.Refreshable {
			add(fmt.Sprintf("profile.source.refresh.%d", i), "    Refresh source · "+source.Name)
		}
		add(fmt.Sprintf("profile.source.delete.%d", i), "    Delete source · "+source.Name)
	}
	return strings.Join(lines, "\n"), hits
}

func profileActionIndex(id, prefix string, size int) (int, bool) {
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, prefix))
	return n, err == nil && n >= 0 && n < size
}

// profileAction routes interface actions into shared app use cases. It does
// not recreate source-reference, schema or active-profile validation.
func (m *Model) profileAction(id string) (bool, tea.Cmd) {
	if id != "subscription.add" && !strings.HasPrefix(id, "profile.overrides.") &&
		!strings.HasPrefix(id, "profile.delete.") && !strings.HasPrefix(id, "profile.source.") {
		return false, nil
	}
	if m.busy || m.quitting {
		return true, nil
	}
	if id == "subscription.add" {
		cmd := m.openForm()
		m.rememberSource = true
		m.failure = ""
		return true, cmd
	}
	if i, ok := profileActionIndex(id, "profile.source.refresh.", len(m.snapshot.Sources)); ok {
		name := m.snapshot.Sources[i].Name
		return true, m.begin("Refreshing subscription source…", "Source refreshed; apply a profile to load changes", func(ctx context.Context) error {
			return m.backend.RefreshSource(ctx, name)
		})
	}
	if i, ok := profileActionIndex(id, "profile.source.delete.", len(m.snapshot.Sources)); ok {
		return true, m.openProfileEdit("source-delete", m.snapshot.Sources[i].Name)
	}
	profiles := m.profiles()
	if i, ok := profileActionIndex(id, "profile.overrides.", len(profiles)); ok {
		return true, m.openProfileEdit("profile-overrides", profiles[i])
	}
	if i, ok := profileActionIndex(id, "profile.delete.", len(profiles)); ok {
		return true, m.openProfileEdit("profile-delete", profiles[i])
	}
	return true, nil
}

func (m Model) profileEditForm() bool {
	return m.form == "profile-overrides" || m.form == "profile-delete" || m.form == "source-delete"
}

func (m *Model) openProfileEdit(form, name string) tea.Cmd {
	m.form, m.name, m.rememberSource = form, name, false
	m.failure = ""
	m.input.Reset()
	m.input.EchoMode = textinput.EchoNormal
	m.input.Placeholder = "Enter the exact name to confirm deletion"
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
		if form != "profile-overrides" && value != name {
			m.failure = "Enter the exact name to confirm deletion"
			return true, nil
		}
		m.closeProfileForm()
		switch form {
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
		case "source-delete":
			return true, m.begin("Deleting subscription source…", "Subscription source deleted", func(ctx context.Context) error {
				return m.backend.DeleteSource(ctx, name)
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
	title, label, hint := "Add subscription", "Configuration name", "The private source URL/path will be saved for refresh."
	if m.form == "source" {
		label = "Subscription URL or local YAML path (hidden)"
	}
	switch m.form {
	case "profile-overrides":
		title, label = "Import overrides · "+clean(m.name), "Absolute path to overrides YAML"
		hint = "Saves a draft; apply the profile separately to load it."
	case "profile-delete":
		title, label = "Delete recipe · "+clean(m.name), "Enter the exact profile name to confirm"
		hint = "The selected profile is protected by the application."
	case "source-delete":
		title, label = "Delete source · "+clean(m.name), "Enter the exact source name to confirm"
		hint = "Sources referenced by a profile cannot be deleted."
	}
	m.input.SetWidth(max(1, boxW-8))
	styleInput(&m.input, inputBackground, m.failure != "")
	lines := []string{
		themed(cellCrop(title, boxW-4), text, raised, false),
		themed(cellCrop(hint, boxW-4), muted, raised, false),
		cellCrop(label, boxW-4), "",
		themed("› "+m.input.View(), text, inputBackground, false), "",
		themed(cellCrop("Enter Continue / Save · Esc Cancel", boxW-4), muted, raised, false),
	}
	if m.failure != "" {
		lines = append(lines, themed(cellCrop(m.failure, boxW-4), errorColor, raised, false))
	}
	box := lipgloss.NewStyle().Background(lipgloss.Color(raised)).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(focus)).Width(boxW-2).Height(boxH-2).Padding(0, 1).Render(strings.Join(lines, "\n"))
	return overlay(base, themed(box, text, raised, false), x, y, m.width, m.height), []hitRegion{{ID: "import-input", X: x + 2, Y: y + 5, W: max(1, boxW-4), H: 1}}
}
