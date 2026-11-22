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
		items := m.profileReplacementItems()
		// The final item is direct routing. Preselect the sole saved alternative,
		// or direct routing if the library has no alternative.
		if len(items) <= 2 {
			m.profilesUI.deleteReplacement = items[0].ID
		}
	}
}

func (m *Model) profileReplacementItems() []menuItem {
	var items []menuItem
	for _, id := range m.profiles() {
		if id != m.name {
			items = append(items, menuItem{ID: id, Label: clean(m.profileInfo(id).Name)})
		}
	}
	items = append(items, menuItem{ID: "default", Label: m.tr("profiles.direct")})
	for i := range items {
		items[i].Checked = items[i].ID == m.profilesUI.deleteReplacement
	}
	return items
}

func (m *Model) renderReplacementMenu() {
	for _, hit := range m.targets {
		if hit.ID == deleteChoice {
			m.menu.setAnchor(hit)
			m.screen = m.menu.render(m.screen, m.width, m.height, m.height)
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
			m.menu.showSelection(m.profileReplacementItems(), m.profilesUI.deleteReplacement, targets[idx])
		case deleteConfirm:
			name, replacement := m.name, m.profilesUI.deleteReplacement
			m.closeProfileForm()
			label := m.tr("profiles.deleting")
			return m.begin(label, m.tr("profiles.deleted"), func(ctx context.Context) error {
				return m.backend.DeleteProfileWithReplacement(ctx, name, replacement)
			}, m.profileNotification("delete", label, name))
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
	replacement := m.profilesUI.deleteReplacement
	hint, confirm := m.tr("profiles.delete_hint"), m.tr("profiles.delete_confirm")
	var choice string
	if !valid {
		hint = m.tr("profiles.delete_stale")
	} else if active {
		items := m.profileReplacementItems()
		selected := slices.IndexFunc(items, func(item menuItem) bool { return item.ID == replacement })
		label := m.tr("profiles.choose_replacement")
		if selected < 0 {
			valid = false
			hint = label
		} else {
			label = items[selected].Label
			if replacement == "default" {
				hint = m.tr("profiles.delete_direct")
				confirm = m.tr("profiles.confirm_direct")
			} else {
				hint = m.tr("profiles.delete_switch", label)
				confirm = m.tr("profiles.confirm_switch")
			}
			if !m.snapshot.Service.Active && m.snapshot.ServiceError == "" {
				hint += " " + m.tr("profiles.delete_stopped")
			}
		}
		if len(items) > 1 || selected < 0 {
			choice = label + " ▾"
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
		if choice != "" {
			height++
		}
		// An active deletion changes routing. Its consequences must fit before
		// confirmation is offered, including when a terminal is resized.
		if height > m.height {
			return overlay("", m.tr("profiles.enlarge"), 0, 0, m.width, m.height), nil
		}
	}
	x, y := (m.width-width)/2, (m.height-height)/2
	rows := profileDialogCanvas(width, height)
	put := func(row int, value string) { putRows(rows, 2, row, cellPad(value, inner), width) }
	put(1, themed(cellCrop(title, inner), text, raised, false))
	buttonRow := height - 2 - len(buttons.lines)
	for i, line := range lines {
		if 2+i >= buttonRow {
			break
		}
		put(2+i, themed(line, muted, raised, false))
	}
	var hits []hitRegion
	if choice != "" {
		row := buttonRow - 1
		value := renderButton(choice, buttonOptions{Kind: buttonSecondary, Surface: raised, Focused: m.profilesUI.deleteFocus == deleteChoice, Hovered: m.hover == deleteChoice, Plain: m.buttonPlain()})
		put(row, cellCrop(value, inner))
		if !m.busy && !m.quitting {
			hits = append(hits, hitRegion{ID: deleteChoice, X: x + 2, Y: y + row, W: min(inner, ansi.StringWidth(value)), H: 1})
		}
	}
	for i, line := range buttons.lines {
		put(buttonRow+i, line)
	}
	for _, hit := range buttons.targets {
		hit.X += x + 2
		hit.Y += y + buttonRow
		hits = append(hits, hit)
	}
	put(height-2, themed(cellCrop(m.profileDeleteContextHelp(), inner), muted, raised, false))
	return overlay(base, strings.Join(rows, "\n"), x, y, m.width, m.height), hits
}
