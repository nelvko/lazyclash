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
func (m *Model) profilesBody(width int) (string, []hitRegion) {
	layout := m.layoutProfiles(width)
	body := m.paintProfiles(layout, 0, layout.total)
	return strings.Join(body.lines, "\n"), body.targets
}

func (m *Model) profilesIntro(width int) dashboardBlock {
	var body dashboardBlock
	if !m.compactProfileCards() && m.searchToolbarGap() > 0 {
		body.blank()
	}
	body.append(m.profilesToolbar(width))
	body.blank()
	return body
}

func (m *Model) paintProfileRow(layout profileGridLayout, first int) dashboardBlock {
	var cards []dashboardBlock
	for index := first; index < min(first+layout.columns, len(layout.names)); index++ {
		cards = append(cards, m.paintProfileCard(layout.names[index], index, layout.cardWidth, layout.compact))
	}
	var row dashboardBlock
	for y := range cards[0].lines {
		line := ""
		for column, card := range cards {
			if column > 0 {
				line += themed("  ", text, bg, false)
			}
			line += card.lines[y]
		}
		row.lines = append(row.lines, cellPad(line, layout.width))
	}
	for column, card := range cards {
		for _, hit := range card.targets {
			hit.X += column * (layout.cardWidth + 2)
			row.targets = append(row.targets, hit)
		}
	}
	return row
}

func (m *Model) profilesToolbar(width int) dashboardBlock {
	var body dashboardBlock
	input := m.profilesUI.url
	input.Placeholder = m.tr("profiles.url_placeholder")
	buttons := []profileButton{{"profiles.add", m.tr("profiles.add"), true}, {"profiles.create", m.tr("profiles.create"), false}}
	buttonWidth := 0
	for _, b := range buttons {
		buttonWidth += ansi.StringWidth(m.profileButtonView(b, bg))
	}
	inline := width >= 70
	fieldWidth := min(width, inputImportMaxWidth)
	if inline {
		fieldWidth = min(inputImportMaxWidth, width-buttonWidth-2)
	}
	invalid := m.profilesUI.urlError != "" && m.profilesUI.urlErrorValue == input.Value()
	const pasteID = "profiles.url.paste"
	disabled := m.busy || m.quitting
	paste := renderButton("⎘", buttonOptions{Kind: buttonText, Surface: bg, Compact: true,
		Hovered: m.hover == pasteID, Focused: m.focus == pasteID, Disabled: disabled})
	var trailing dashboardBlock
	if pasteWidth := ansi.StringWidth(paste); inputFieldInlineTrailing(fieldWidth, pasteWidth, true) {
		trailing = dashboardBlock{lines: []string{paste}, targets: []hitRegion{{ID: pasteID, W: pasteWidth, H: 1}}}
	}
	body = renderInputField(input, inputFieldOptions{ID: "profiles.url", ClearID: "profiles.url.clear", ClearHovered: m.hover == "profiles.url.clear", ClearFocused: m.focus == "profiles.url.clear", Width: fieldWidth, Surface: bg,
		Focused: m.focus == "profiles.url", Hovered: m.hover == "profiles.url", Invalid: invalid,
		Disabled: disabled, Trailing: trailing})
	if inline {
		x := fieldWidth + 2
		body.lines[1] += themed("  ", text, bg, false)
		for _, b := range buttons {
			value := m.profileButtonView(b, bg)
			body.lines[1] += value
			if !m.busy && !m.quitting {
				body.targets = append(body.targets, hitRegion{ID: b.id, X: x, Y: 1, W: ansi.StringWidth(value), H: 1})
			}
			x += ansi.StringWidth(value)
		}
		body.lines[1] = cellCrop(body.lines[1], width)
	}
	if m.profilesUI.pasteUnavailable {
		for _, line := range strings.Split(wrapText(m.tr("profiles.clipboard_unavailable"), fieldWidth), "\n") {
			body.lines = append(body.lines, themed(line, muted, bg, false))
		}
	} else if invalid {
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

func (m *Model) profileButtonView(b profileButton, surface string) string {
	kind := buttonSecondary
	if b.primary {
		kind = buttonPrimary
	} else if strings.HasPrefix(b.id, "profiles.refresh.") || strings.HasPrefix(b.id, "profiles.menu.") {
		kind = buttonText
	} else if strings.HasPrefix(b.id, "profiles.delete.") {
		kind = buttonDanger
	}
	return renderButton(b.label, buttonOptions{Kind: kind, Surface: surface,
		Focused: m.focus == b.id && b.id != "", Hovered: m.hover == b.id && b.id != "",
		Disabled: b.id == "" || m.busy || m.quitting, Plain: m.buttonPlain()})
}

func (m *Model) profileButtons(buttons []profileButton, width int) dashboardBlock {
	body := dashboardBlock{lines: []string{""}}
	x, y := 0, 0
	for _, b := range buttons {
		value := m.profileButtonView(b, bg)
		w := min(width, ansi.StringWidth(value))
		if x > 0 && x+w > width {
			body.lines = append(body.lines, "")
			x, y = 0, y+1
		}
		body.lines[y] += cellCrop(value, w)
		if b.id != "" && !m.busy && !m.quitting {
			body.targets = append(body.targets, hitRegion{ID: b.id, X: x, Y: y, W: w, H: 1})
		}
		x += w
	}
	return body
}

func (m *Model) profileInfo(name string) app.ProfileInfo {
	for _, info := range m.snapshot.ProfileDetails {
		if info.ID == name {
			return info
		}
	}
	return app.ProfileInfo{ID: name, Name: name}
}

func snapshotProfileName(snapshot app.Snapshot, id string, locales ...i18n.Localizer) string {
	if id == "" {
		return dashboardLocale(locales).T("profiles.none_selected")
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

func (m *Model) profileCard(index, width int) dashboardBlock {
	return m.paintProfileCard(m.profiles()[index], index, width, m.compactProfileCards())
}

func (m *Model) paintProfileCard(name string, index, width int, compact bool) dashboardBlock {
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
	controls := profileCardControls(refreshID, menuID, width, info.Refreshable)
	controlViews := make([]string, len(controls))
	controlsWidth := 0
	widths := make(map[string]int, len(controls))
	for i, control := range controls {
		controlViews[i] = m.profileButtonView(control, surface)
		widths[control.label] = ansi.StringWidth(controlViews[i])
		controlsWidth += widths[control.label]
	}
	titleWidth := max(1, inner-controlsWidth)
	title := themed(lipgloss.NewStyle().Bold(true).Render(cellCrop(prefix+clean(info.Name), max(1, titleWidth))), titleColor, surface, false)
	title = cellPad(title, titleWidth)
	for _, control := range controlViews {
		title += control
	}
	metadata := m.profileOrigin(info)
	if metadata != "" {
		metadata += " · "
	}
	metadata += profileUpdated(info.UpdatedAt, time.Now(), m.locale)
	status, statusColor := m.tr("profiles.ready"), text
	if !info.Validated {
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
	// Reserve two auxiliary rows for every card, so long origins can wrap
	// without shifting another card's status or the following grid row.
	metadataLines := strings.Split(wrapText(metadata, inner), "\n")
	if len(metadataLines) > 2 {
		metadataLines[1] = cellCrop(strings.Join(metadataLines[1:], " "), inner)
	}
	lines := []string{title}
	for row := range 2 {
		value := ""
		if row < len(metadataLines) {
			value = metadataLines[row]
		}
		lines = append(lines, themed(value, muted, surface, false))
	}
	lines = append(lines, themed(cellCrop(status, inner), statusColor, surface, false))
	picture := profileCardSurface(lines, width, surface, selected, compact)
	parent := hitRegion{ID: id, W: width, H: len(picture)}
	return dashboardBlock{lines: picture, targets: m.profileCardTargets(parent, controls, widths, compact)}
}

func profileCardControls(refreshID, menuID string, width int, refreshable bool) []profileButton {
	var controls []profileButton
	if refreshable && width >= 16 {
		controls = append(controls, profileButton{id: refreshID, label: "↻"})
	}
	if width >= 12 {
		controls = append(controls, profileButton{id: menuID, label: "⋯"})
	}
	return controls
}

func (m *Model) profileCardTargets(card hitRegion, controls []profileButton, widths map[string]int, compact bool) []hitRegion {
	var hits []hitRegion
	if len(controls) > 0 && !m.busy && !m.quitting {
		totalWidth := 0
		for _, control := range controls {
			totalWidth += widths[control.label]
		}
		x := card.X + 2 + max(1, max(1, card.W-4)-totalWidth)
		y := card.Y
		if !compact {
			y++
		}
		for _, control := range controls {
			w := widths[control.label]
			hits = append(hits, hitRegion{ID: control.id, X: x, Y: y, W: w, H: 1})
			x += w
		}
	}
	// Card controls win hit testing over the containing card.
	return append(hits, card)
}

func (m *Model) compactProfileCards() bool {
	return dashboardBodyWidth(m.dashboardOptions()) < 86 || m.height <= 24
}

func profileCardSurface(lines []string, width int, surface string, selected, compact bool) []string {
	marker := " "
	if selected {
		marker = themed("▌", accent, surface, false)
	}
	row := func(line string) string {
		return themed(cellPad(marker+" "+cellPad(line, max(0, width-4))+"  ", width), text, surface, false)
	}
	var rows []string
	if !compact {
		rows = append(rows, row(""))
	}
	for _, line := range lines {
		rows = append(rows, row(line))
	}
	if !compact {
		rows = append(rows, row(""))
	}
	return rows
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

func (m *Model) profileOrigin(info app.ProfileInfo) string {
	switch info.Kind {
	case "local":
		return m.tr("profiles.create_local")
	case "imported":
		return m.tr("profiles.imported_origin")
	default:
		return strings.TrimSpace(clean(info.Origin))
	}
}

func (m *Model) profileDetailsPane(name string, width int) dashboardBlock {
	for i, profile := range m.profiles() {
		if profile == name {
			return m.profileDetailsPaneAt(name, i, width)
		}
	}
	return dashboardBlock{}
}

func (m *Model) profileDetailsPaneAt(name string, index, width int) dashboardBlock {
	info := m.profileInfo(name)
	body := dashboardBlock{lines: []string{themed(cellCrop(m.tr("profiles.details_title", clean(info.Name)), width), focus, bg, false)}}
	kind := m.tr("profiles.kind")
	if info.HasOverrides {
		kind = m.tr("profiles.kind_overrides", kind)
	}
	body.lines = append(body.lines, themed(cellCrop(kind, width), muted, bg, false))
	if origin := m.profileOrigin(info); origin != "" {
		for _, line := range strings.Split(wrapText(origin, width), "\n") {
			body.lines = append(body.lines, themed(line, muted, bg, false))
		}
	}
	body.lines = append(body.lines, themed(cellCrop(profileUpdated(info.UpdatedAt, time.Now(), m.locale), width), muted, bg, false))
	if info.Description != "" {
		body.lines = append(body.lines, themed(cellCrop(clean(info.Description), width), muted, bg, false))
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
	buttons = append(buttons,
		profileButton{fmt.Sprintf("profiles.rename.%d", index), m.tr("profiles.rename"), false},
		profileButton{fmt.Sprintf("profiles.description.%d", index), m.tr("profiles.edit_description"), false},
		profileButton{fmt.Sprintf("profiles.url-edit.%d", index), m.tr("profiles.edit_url"), false},
		profileButton{fmt.Sprintf("profiles.overrides.%d", index), m.tr("profiles.overrides"), false},
		profileButton{fmt.Sprintf("profiles.delete.%d", index), m.tr("profiles.delete"), false})
	if m.canUseDefaultFrom(name) {
		buttons = append(buttons, profileButton{"profiles.use-default", m.tr("profiles.use_default"), false})
	}
	buttons = append(buttons, profileButton{"profiles.close", m.tr("profiles.close"), false})
	body.append(m.profileButtons(buttons, width))
	return body
}
