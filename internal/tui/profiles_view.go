package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

// Bubbles supplies the URL editor; the responsive card grid is a small custom
// view because a vertical list/table would not preserve its spatial navigation.
func (m Model) profilesBody(width int) (string, []hitRegion) {
	width = max(1, width)
	if !m.snapshotReady {
		return m.unavailableProfiles(width)
	}
	body := m.profilesToolbar(width)
	body.blank()
	count := len(m.profiles()) - 1
	summary := fmt.Sprintf(m.tr("profiles.summary"), count)
	if m.snapshot.Applied != "" {
		summary += m.tr("profiles.selected_name", clean(snapshotProfileName(m.snapshot, m.snapshot.Applied, m.locale)))
	}
	body.lines = append(body.lines, themed(cellCrop(summary, width), muted, bg, false))
	body.blank()
	profiles := m.profiles()
	columns := 1
	if width >= 86 {
		columns = 2
	}
	cardWidth := (width - 2*(columns-1)) / columns
	for first := 0; first < len(profiles); first += columns {
		cards := []dashboardBlock{}
		for column := 0; column < columns && first+column < len(profiles); column++ {
			cards = append(cards, m.profileCard(first+column, cardWidth))
		}
		row := dashboardBlock{}
		for y := range len(cards[0].lines) {
			line := ""
			for column, card := range cards {
				if column > 0 {
					line += themed("  ", text, bg, false)
				}
				line += card.lines[y]
			}
			row.lines = append(row.lines, cellPad(line, width))
		}
		for column, card := range cards {
			for _, hit := range card.targets {
				hit.X += column * (cardWidth + 2)
				row.targets = append(row.targets, hit)
			}
		}
		body.append(row)
		body.blank()
		for _, name := range profiles[first:min(first+columns, len(profiles))] {
			if m.profilesUI.detail == name {
				body.append(m.profileDetailsPane(name, width))
				body.blank()
			}
		}
	}
	if count == 0 {
		body.lines = append(body.lines, themed(cellCrop(m.tr("profiles.empty_hint"), width), muted, bg, false), "")
	}
	return strings.Join(body.lines, "\n"), body.targets
}

func (m Model) profilesToolbar(width int) dashboardBlock {
	var body dashboardBlock
	input := m.profilesUI.url
	input.Placeholder = m.tr("profiles.url_placeholder")
	buttons := []profileButton{{"profiles.add", m.tr("profiles.add"), true}, {"profiles.create", m.tr("profiles.create"), false}}
	buttonWidth := 0
	for _, b := range buttons {
		buttonWidth += ansi.StringWidth(m.profileButtonView(b, bg)) + 2
	}
	inline := width >= 70
	fieldWidth := width
	if inline {
		fieldWidth -= buttonWidth + 2
	}
	// Bubbles reserves one additional cell for the cursor, outside Width.
	input.SetWidth(max(1, fieldWidth-5-ansi.StringWidth(input.Prompt)))
	input.SetCursor(input.Position())
	invalid := m.profilesUI.urlError != "" && m.profilesUI.urlErrorValue == input.Value()
	styleInput(&input, inputBackground, invalid)
	edge := border
	if m.focus == "profiles.url" {
		edge = focus
	}
	if invalid {
		edge = errorColor
	}
	field := profileInputFrame([]string{input.View()}, fieldWidth, edge, inputBackground)
	body.lines = append(body.lines, field...)
	body.targets = append(body.targets, hitRegion{ID: "profiles.url", W: fieldWidth, H: len(field)})
	if inline {
		x := fieldWidth + 2
		for _, b := range buttons {
			value := m.profileButtonView(b, bg)
			body.lines[1] += themed("  ", text, bg, false) + value
			if !m.busy && !m.quitting {
				body.targets = append(body.targets, hitRegion{ID: b.id, X: x, Y: 1, W: ansi.StringWidth(value), H: 1})
			}
			x += ansi.StringWidth(value) + 2
		}
		body.lines[1] = cellCrop(body.lines[1], width)
	}
	if invalid {
		for _, line := range strings.Split(wrapText(clean(m.profilesUI.urlError), fieldWidth), "\n") {
			body.lines = append(body.lines, themed(line, errorColor, bg, false))
		}
	}
	if !inline {
		body.append(m.profileButtons(buttons, width))
	}
	return body
}

type profileButton struct {
	id, label string
	primary   bool
}

func (m Model) profileButtonView(b profileButton, surface string) string {
	kind := buttonSecondary
	if b.primary {
		kind = buttonPrimary
	} else if strings.HasPrefix(b.id, "profiles.delete.") {
		kind = buttonDanger
	}
	return renderButton(b.label, buttonOptions{Kind: kind, Surface: surface,
		Focused: m.focus == b.id && b.id != "", Hovered: m.hover == b.id && b.id != "",
		Disabled: b.id == "" || m.busy || m.quitting, Plain: m.buttonPlain()})
}

func (m Model) profileButtons(buttons []profileButton, width int) dashboardBlock {
	body := dashboardBlock{lines: []string{""}}
	x, y := 0, 0
	for _, b := range buttons {
		value := m.profileButtonView(b, bg)
		w := min(width, ansi.StringWidth(value))
		if x > 0 && x+w > width {
			body.lines = append(body.lines, "")
			x, y = 0, y+1
		}
		if x > 0 {
			body.lines[y] += themed("  ", text, bg, false)
		}
		body.lines[y] += cellCrop(value, w)
		if b.id != "" && !m.busy && !m.quitting {
			body.targets = append(body.targets, hitRegion{ID: b.id, X: x, Y: y, W: w, H: 1})
		}
		x += w + 2
	}
	return body
}

func (m Model) profileInfo(name string) app.ProfileInfo {
	for _, info := range m.snapshot.ProfileDetails {
		if info.ID == name {
			if name == "default" {
				info.Name = m.tr("profiles.builtin_name")
			}
			return info
		}
	}
	if name == "default" {
		return app.ProfileInfo{ID: name, Name: m.tr("profiles.builtin_name")}
	}
	return app.ProfileInfo{}
}

func snapshotProfileName(snapshot app.Snapshot, id string, locales ...i18n.Localizer) string {
	if id == "default" {
		return dashboardLocale(locales).T("profiles.builtin_name")
	}
	if id == snapshot.Applied && snapshot.AppliedName != "" {
		return snapshot.AppliedName
	}
	for _, info := range snapshot.ProfileDetails {
		if info.ID == id {
			return info.Name
		}
	}
	return id
}

func (m Model) profileCard(index, width int) dashboardBlock {
	name := m.profiles()[index]
	info := m.profileInfo(name)
	id := fmt.Sprintf("profile.%d", index)
	refreshID := fmt.Sprintf("profiles.refresh.%d", index)
	menuID := fmt.Sprintf("profiles.menu.%d", index)
	selected := name == m.snapshot.Applied
	surface := panel
	if m.hover == id || m.hover == refreshID || m.hover == menuID || m.focus == id || m.focus == refreshID || m.focus == menuID {
		surface = raised
	}
	inner := max(1, width-4)
	prefix := ""
	if m.focus == id {
		prefix = "› "
	}
	titleColor := text
	if selected {
		titleColor = accent
	}
	var controls []profileButton
	if info.Refreshable && width >= 16 {
		controls = append(controls, profileButton{id: refreshID, label: "↻"})
	}
	if width >= 12 {
		controls = append(controls, profileButton{id: menuID, label: "⋯"})
	}
	controlViews := make([]string, len(controls))
	controlsWidth := 0
	for i, control := range controls {
		controlViews[i] = m.profileButtonView(control, surface)
		controlsWidth += ansi.StringWidth(controlViews[i])
	}
	titleWidth := max(1, inner-controlsWidth)
	title := themed(lipgloss.NewStyle().Bold(true).Render(cellCrop(prefix+clean(info.Name), max(1, titleWidth))), titleColor, surface, false)
	title = cellPad(title, titleWidth)
	for _, control := range controlViews {
		title += control
	}
	origin, kind := m.tr("profiles.builtin_origin"), m.tr("profiles.kind")
	if name != "default" {
		origin = info.Origin
		if info.HasOverrides {
			kind = m.tr("profiles.kind_overrides", kind)
		}
	}
	status, statusColor := m.tr("profiles.ready"), muted
	if !info.Validated && name != "default" {
		status, statusColor = m.tr("profiles.validation_pending"), warning
	}
	if selected {
		status, statusColor = m.tr("profiles.selected_next_start"), accent
		if m.snapshot.ServiceError != "" || m.snapshot.APIError != "" {
			status = m.tr("profiles.selected_unavailable")
		} else if m.snapshot.Service.Active {
			status = m.tr("profiles.in_use")
		}
	}
	if info.Pending {
		status, statusColor = m.tr("profiles.changes_pending"), warning
	}
	stamp := m.tr("profiles.builtin_stamp")
	if name != "default" {
		stamp = profileUpdated(info.UpdatedAt, time.Now(), m.locale)
	}
	lines := []string{title, themed(cellCrop(clean(origin), inner), muted, surface, false), themed(cellCrop(kind, inner), muted, surface, false),
		themed(cellCrop(stamp, inner), muted, surface, false), themed(cellCrop(status, inner), statusColor, surface, false)}
	block := dashboardBlock{lines: profileCardSurface(lines, width, surface, selected)}
	// Card controls win hit testing over the containing card.
	x := 2 + titleWidth
	for i, control := range controls {
		w := ansi.StringWidth(controlViews[i])
		if !m.busy && !m.quitting {
			block.targets = append(block.targets, hitRegion{ID: control.id, X: x, Y: 1, W: w, H: 1})
		}
		x += w
	}
	block.targets = append(block.targets, hitRegion{ID: id, W: width, H: len(block.lines)})
	return block
}

func profileCardSurface(lines []string, width int, surface string, selected bool) []string {
	marker := " "
	if selected {
		marker = themed("▌", accent, surface, false)
	}
	row := func(line string) string {
		return themed(cellPad(marker+" "+cellPad(line, max(0, width-4))+"  ", width), text, surface, false)
	}
	rows := []string{row("")}
	for _, line := range lines {
		rows = append(rows, row(line))
	}
	return append(rows, row(""))
}

func profileInputFrame(lines []string, width int, edge, surface string) []string {
	if width < 4 {
		var out []string
		for _, line := range lines {
			out = append(out, cellCrop(line, width))
		}
		return out
	}
	rows := []string{themed("╭"+strings.Repeat("─", width-2)+"╮", edge, surface, false)}
	for _, line := range lines {
		rows = append(rows, themed("│ "+cellPad(line, width-4)+" │", edge, surface, false))
	}
	return append(rows, themed("╰"+strings.Repeat("─", width-2)+"╯", edge, surface, false))
}

func profileUpdated(updated, now time.Time, locales ...i18n.Localizer) string {
	var locale i18n.Localizer
	if len(locales) > 0 {
		locale = locales[0]
	}
	if updated.IsZero() {
		return locale.T("profiles.no_history")
	}
	age := now.Sub(updated)
	switch {
	case age < time.Minute:
		return locale.T("profiles.just_updated")
	case age < time.Hour:
		return fmt.Sprintf(locale.T("profiles.updated_minutes"), int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf(locale.T("profiles.updated_hours"), int(age/time.Hour))
	case age < 30*24*time.Hour:
		return fmt.Sprintf(locale.T("profiles.updated_days"), int(age/(24*time.Hour)))
	default:
		return locale.T("profiles.updated_date", updated.Format("2006-01-02"))
	}
}

func (m Model) profileDetailsPane(name string, width int) dashboardBlock {
	info := m.profileInfo(name)
	index := 0
	for i, profile := range m.profiles() {
		if profile == name {
			index = i
			break
		}
	}
	body := dashboardBlock{lines: []string{themed(cellCrop(m.tr("profiles.details_title", clean(info.Name)), width), focus, bg, false)}}
	if name == "default" {
		body.lines = append(body.lines, themed(cellCrop(m.tr("profiles.builtin_description"), width), muted, bg, false))
	} else {
		body.lines = append(body.lines, themed(cellCrop(clean(info.Origin), width), muted, bg, false))
		if info.Description != "" {
			body.lines = append(body.lines, themed(cellCrop(clean(info.Description), width), muted, bg, false))
		}
	}
	applyID, label := fmt.Sprintf("profiles.apply.%d", index), m.tr("profiles.use")
	if name == m.snapshot.Applied {
		label = m.tr("profiles.reapply")
	}
	if info.Pending {
		label = m.tr("profiles.apply_changes")
	}
	if !m.snapshot.Installed {
		applyID, label = "", m.tr("profiles.install_to_apply")
	}
	buttons := []profileButton{{applyID, label, true}}
	if info.Refreshable {
		buttons = append(buttons, profileButton{fmt.Sprintf("profiles.update.%d", index), m.tr("profiles.update"), false})
	}
	if name != "default" {
		buttons = append(buttons, profileButton{fmt.Sprintf("profiles.rename.%d", index), m.tr("profiles.rename"), false})
		buttons = append(buttons, profileButton{fmt.Sprintf("profiles.description.%d", index), m.tr("profiles.edit_description"), false})
		buttons = append(buttons, profileButton{fmt.Sprintf("profiles.url-edit.%d", index), m.tr("profiles.edit_url"), false})
		buttons = append(buttons, profileButton{fmt.Sprintf("profiles.overrides.%d", index), m.tr("profiles.overrides"), false}, profileButton{fmt.Sprintf("profiles.delete.%d", index), m.tr("profiles.delete"), false})
	}
	buttons = append(buttons, profileButton{"profiles.close", m.tr("profiles.close"), false})
	body.append(m.profileButtons(buttons, width))
	return body
}
