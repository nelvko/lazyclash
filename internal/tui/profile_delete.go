package tui

import (
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	deleteCancel  = "profile-delete.cancel"
	deleteConfirm = "profile-delete.confirm"
	deleteChoice  = "profile-delete.replacement"
)

func (m *Model) prepareProfileDelete() {
	m.profileDeleteSetFocus(deleteCancel)
	m.profilesUI.deleteCurrent = m.canUseDefaultFrom(m.name)
	m.profilesUI.deleteReplacement = ""
	if m.profilesUI.deleteCurrent {
		items := m.profileReplacementDropdown().Items
		// The final item is direct routing. Preselect the sole saved alternative,
		// or direct routing if the library has no alternative.
		if len(items) <= 2 {
			m.profilesUI.deleteReplacement = items[0].ID
		}
	}
}

func (m *Model) profileReplacementDropdown() dropdown {
	var items []menuItem
	for _, id := range m.profiles() {
		if id != m.name {
			items = append(items, menuItem{ID: id, Label: clean(m.profileInfo(id).Name)})
		}
	}
	items = append(items, menuItem{ID: "default", Label: m.tr("profiles.direct")})
	return dropdown{Items: items, Value: m.profilesUI.deleteReplacement, Placeholder: m.tr("profiles.choose_replacement")}
}

func (m *Model) renderReplacementMenu() {
	for _, hit := range m.targets {
		if hit.ID == deleteChoice {
			m.menu.setAnchor(hit)
			m.screen = m.renderMenu(m.screen, m.height)
			return
		}
	}
	m.menu.dismiss()
}

func (m *Model) profileDeleteMouse(id string) tea.Cmd {
	if m.form != "profile-delete" || m.busy || m.quitting {
		return nil
	}
	m.profileDeleteSetFocus(id)
	return m.profileDeleteKey(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func (m *Model) profileDeleteKey(k tea.KeyPressMsg) tea.Cmd {
	if m.busy || m.quitting {
		return nil
	}
	_, targets := m.renderProfileDeleteForm("")
	if len(targets) == 0 {
		return nil
	}
	idx := slices.IndexFunc(targets, func(hit hitRegion) bool { return hit.ID == m.profilesUI.deleteFocus })
	switch k.String() {
	case "tab", "right", "shift+tab", "left":
		step := 1
		if k.String() == "shift+tab" || k.String() == "left" {
			step = -1
		}
		m.profileDeleteSetFocus(targets[(idx+step+len(targets))%len(targets)].ID)
	case "enter":
		if idx < 0 {
			return nil
		}
		switch targets[idx].ID {
		case deleteCancel:
			m.closeProfileForm()
		case deleteChoice:
			m.profileReplacementDropdown().show(&m.menu, targets[idx])
		case deleteConfirm:
			name, replacement := m.name, m.profilesUI.deleteReplacement
			m.closeProfileForm()
			label := m.tr("profiles.deleting")
			message := m.tr("feedback.deleted", m.profileFeedbackName(name))
			if replacement == "default" {
				message += " · " + m.tr("profiles.none_selected")
			} else if replacement != "" {
				message += " · " + m.tr("feedback.selected", m.profileFeedbackName(replacement))
			}
			return m.begin(label, message, func(ctx context.Context) error {
				return m.backend.DeleteProfileWithReplacement(ctx, name, replacement)
			}, m.profileNotification("delete", m.tr("profiles.delete"), name))
		}
	}
	return nil
}

func (m *Model) renderProfileDeleteForm(base string) (string, []hitRegion) {
	width := min(inputFieldMaxWidth+4, m.width)
	if width < 24 || m.height < 7 {
		return overlay("", m.tr("profiles.enlarge"), 0, 0, m.width, m.height), nil
	}

	inner := width - 4
	active := m.profilesUI.deleteCurrent
	valid := slices.Contains(m.profiles(), m.name) && active == m.canUseDefaultFrom(m.name)
	hint, confirm := m.tr("profiles.delete_hint"), m.tr("profiles.delete_confirm")
	var choice dropdown
	if !valid {
		hint = m.tr("profiles.delete_stale")
	} else if active {
		replacements := m.profileReplacementDropdown()
		selected := slices.IndexFunc(replacements.Items, func(item menuItem) bool { return item.ID == replacements.Value })
		if selected < 0 {
			valid = false
			hint = replacements.Placeholder
		} else {
			if replacements.Value == "default" {
				hint = m.tr("profiles.delete_direct")
				confirm = m.tr("profiles.confirm_direct")
			} else {
				hint = m.tr("profiles.delete_switch", replacements.Items[selected].Label)
				confirm = m.tr("profiles.confirm_switch")
			}
			if !m.snapshot.Service.Active && m.snapshot.ServiceError == "" {
				hint += " " + m.tr("profiles.delete_stopped")
			}
		}
		if len(replacements.Items) > 1 || selected < 0 {
			choice = replacements
		}
	}
	title := m.tr("profiles.delete_title", clean(m.profileInfo(m.name).Name))
	lines := strings.Split(wrapText(hint, inner), "\n")
	var buttons dashboardBlock
	buttons.lines = []string{""}
	for _, b := range []struct {
		id, label string
		kind      buttonKind
		enabled   bool
	}{
		{deleteCancel, m.tr("profiles.create_cancel"), buttonSecondary, true},
		{deleteConfirm, confirm, buttonDangerConfirm, valid},
	} {
		enabled := b.enabled && !m.busy && !m.quitting
		value := renderButton(b.label, buttonOptions{Kind: b.kind, Surface: raised, Focused: m.profilesUI.deleteFocus == b.id, Hovered: m.hover == b.id, Disabled: !enabled, Plain: m.buttonPlain()})
		row := len(buttons.lines) - 1
		x := ansi.StringWidth(buttons.lines[row])
		if x > 0 && x+ansi.StringWidth(value) > inner {
			buttons.lines = append(buttons.lines, "")
			row++
			x = 0
		}
		if enabled {
			buttons.targets = append(buttons.targets, hitRegion{ID: b.id, X: x, Y: row, W: min(inner, ansi.StringWidth(value)), H: 1})
		}
		buttons.lines[row] += cellCrop(value, inner)
	}
	height := min(9, m.height)
	if active {
		height = 5 + len(lines) + len(buttons.lines)
		if len(choice.Items) > 0 {
			height++
		}
		// An active deletion changes routing. Its consequences must fit before
		// confirmation is offered, including when a terminal is resized.
		if height > m.height {
			return overlay("", m.tr("profiles.enlarge"), 0, 0, m.width, m.height), nil
		}
	}
	surface := m.newModalSurface(width, height, raised)
	put := func(row int, value string) { surface.put(2, row, cellCrop(value, inner)) }
	put(1, themed(cellCrop(title, inner), text, raised, false))
	buttonRow := height - 2 - len(buttons.lines)
	for i, line := range lines {
		if 2+i >= buttonRow {
			break
		}
		put(2+i, themed(line, muted, raised, false))
	}
	var hits []hitRegion
	if len(choice.Items) > 0 {
		row := buttonRow - 1
		o := buttonOptions{Surface: raised, Focused: m.profilesUI.deleteFocus == deleteChoice, Hovered: m.hover == deleteChoice, Disabled: m.busy || m.quitting, Plain: m.buttonPlain()}
		value, hit := choice.render(deleteChoice, o, m.menu.dropdownOpen(deleteChoice), inner)
		put(row, value)
		if !o.Disabled {
			hit.X += 2
			hit.Y += row
			hits = append(hits, surface.screen(hit))
		}
	}
	for i, line := range buttons.lines {
		put(buttonRow+i, line)
	}
	for _, hit := range buttons.targets {
		hit.X += 2
		hit.Y += buttonRow
		hits = append(hits, surface.screen(hit))
	}
	put(height-2, themed(cellCrop(m.profileDeleteContextHelp(), inner), muted, raised, false))
	return surface.render(base), hits
}
