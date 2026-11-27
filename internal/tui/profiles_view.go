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
	body := m.profilesToolbar(width)
	body.blank()
	return body
}

func (m *Model) paintProfileRow(layout profileGridLayout, first int) dashboardBlock {
	var cards []dashboardBlock
	for index := first; index < min(first+layout.columns, len(layout.names)); index++ {
		cards = append(cards, m.paintProfileCard(layout.names[index], index, layout.cardWidth, layout.compact))
	}
	var row dashboardBlock
	gap := themed(strings.Repeat(" ", profileColumnGap), text, bg, false)
	for y := range cards[0].lines {
		line := ""
		for column, card := range cards {
			if column > 0 {
				line += gap
			}
			line += card.lines[y]
		}
		row.lines = append(row.lines, cellPad(line, layout.width))
	}
	for column, card := range cards {
		for _, hit := range card.targets {
			hit.X += column * (layout.cardWidth + profileColumnGap)
			row.targets = append(row.targets, hit)
		}
	}
	return row
}

func (m *Model) profilesToolbar(width int) dashboardBlock {
	var body dashboardBlock
	input := m.profilesUI.url
	input.Placeholder = m.tr("profiles.url_placeholder")
	importID := "profiles.add"
	if m.profilesUI.importSource() == "" {
		importID = ""
	}
	buttons := []profileButton{{importID, m.tr("profiles.add"), true}, {"profiles.create", m.tr("profiles.create"), false}}
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
	paste := renderButton("⎘", buttonOptions{Kind: buttonIcon, Surface: bg, Compact: true, Plain: m.buttonPlain(),
		Hovered: m.hover == pasteID, Focused: m.focus == pasteID, Disabled: disabled})
	var trailing dashboardBlock
	if pasteWidth := ansi.StringWidth(paste); pasteWidth <= fieldWidth {
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
			if b.id != "" && !m.busy && !m.quitting {
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
		body.append(m.profileButtons(buttons, width, bg))
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
		kind = buttonIcon
	} else if strings.HasPrefix(b.id, "profile-settings.") {
		kind = buttonText
	} else if strings.HasPrefix(b.id, "profiles.delete.") {
		kind = buttonDanger
	}
	return renderButton(b.label, buttonOptions{Kind: kind, Surface: surface,
		Focused: m.focus == b.id && b.id != "", Hovered: m.hover == b.id && b.id != "",
		Disabled: b.id == "" || m.busy || m.quitting, Plain: b.id != "" && m.buttonPlain()})
}

func (m *Model) profileButtons(buttons []profileButton, width int, surface string) dashboardBlock {
	body := dashboardBlock{lines: []string{""}}
	x, y := 0, 0
	for _, b := range buttons {
		value := m.profileButtonView(b, surface)
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
	if info := snapshotProfileDetails(&m.snapshot, name); info != nil {
		return *info
	}
	return app.ProfileInfo{ID: name, Name: name}
}

func snapshotProfileDetails(s *app.Snapshot, id string) *app.ProfileInfo {
	if id == "" {
		return nil
	}
	for i := range s.ProfileDetails {
		if s.ProfileDetails[i].ID == id {
			return &s.ProfileDetails[i]
		}
	}
	return nil
}

func snapshotProfileName(snapshot app.Snapshot, id string, locales ...i18n.Localizer) string {
	if id == "" {
		return dashboardLocale(locales).T("profiles.none_selected")
	}
	if id == snapshot.Applied && snapshot.AppliedName != "" {
		return snapshot.AppliedName
	}
	if info := snapshotProfileDetails(&snapshot, id); info != nil {
		return info.Name
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
	surface := gridCardBackground
	if m.hover == id || m.hover == refreshID || m.hover == menuID || m.focus == id || m.focus == refreshID || m.focus == menuID {
		surface = raised
	}
	inner := max(1, width-4)
	prefix := ""
	if m.focus == id {
		prefix = "› "
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
	title := themed(lipgloss.NewStyle().Bold(true).Render(cellCrop(prefix+clean(info.Name), max(1, titleWidth))), text, surface, false)
	title = cellPad(title, titleWidth)
	for _, control := range controlViews {
		title += control
	}
	status := profileWarning(info, &m.snapshot, m.locale)
	lines := append([]string{title}, profileCardDetails(info, time.Now(), m.locale, inner, surface, status)...)
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
		return themed(marker+cellPad(" "+cellCrop(line, max(0, width-4)), max(0, width-1)), text, surface, false)
	}
	var rows []string
	if !compact {
		rows = append(rows, row(""))
	}
	for _, line := range lines {
		rows = append(rows, row(line))
	}
	return rows
}
