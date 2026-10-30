package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
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
	summary := fmt.Sprintf("Configurations · %d saved", count)
	if m.snapshot.Applied != "" {
		summary += " · Selected: " + clean(snapshotProfileName(m.snapshot, m.snapshot.Applied))
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
		body.lines = append(body.lines, themed(cellCrop("Add a subscription above, or import a local Mihomo configuration.", width), muted, bg, false), "")
	}
	return strings.Join(body.lines, "\n"), body.targets
}

func (m Model) profilesToolbar(width int) dashboardBlock {
	var body dashboardBlock
	input := m.profilesUI.url
	options := dashboardOptions{Focus: m.focus, Hover: m.hover}
	buttons := []profileButton{{"profiles.add", "Add", true}, {"add", "Import", false}}
	buttonWidth := 0
	for _, b := range buttons {
		buttonWidth += ansi.StringWidth(dashboardButton(b.id, b.label, options, b.primary)) + 2
	}
	inline := width >= 70
	fieldWidth := width
	if inline {
		fieldWidth -= buttonWidth + 2
	}
	// Bubbles reserves one additional cell for the cursor, outside Width.
	input.SetWidth(max(1, fieldWidth-5-ansi.StringWidth(input.Prompt)))
	input.SetCursor(input.Position())
	styleInput(&input, inputBackground, false)
	edge := border
	if m.focus == "profiles.url" {
		edge = focus
	}
	field := profileInputFrame([]string{input.View()}, fieldWidth, edge, inputBackground)
	body.lines = append(body.lines, field...)
	body.targets = append(body.targets, hitRegion{ID: "profiles.url", W: fieldWidth, H: len(field)})
	if inline {
		x := fieldWidth + 2
		for _, b := range buttons {
			value := dashboardButton(b.id, b.label, options, b.primary)
			body.lines[1] += themed("  ", text, bg, false) + value
			body.targets = append(body.targets, hitRegion{ID: b.id, X: x, Y: 1, W: ansi.StringWidth(value), H: 1})
			x += ansi.StringWidth(value) + 2
		}
		body.lines[1] = cellCrop(body.lines[1], width)
	} else {
		body.append(m.profileButtons(buttons, width))
	}
	return body
}

type profileButton struct {
	id, label string
	primary   bool
}

func (m Model) profileButtons(buttons []profileButton, width int) dashboardBlock {
	body := dashboardBlock{lines: []string{""}}
	x, y := 0, 0
	for _, b := range buttons {
		value := dashboardButton(b.id, b.label, dashboardOptions{Focus: m.focus, Hover: m.hover}, b.primary)
		if b.id == "" {
			value = themed("[ "+b.label+" ]", disabled, bg, false)
		}
		w := min(width, ansi.StringWidth(value))
		if x > 0 && x+w > width {
			body.lines = append(body.lines, "")
			x, y = 0, y+1
		}
		if x > 0 {
			body.lines[y] += themed("  ", text, bg, false)
		}
		body.lines[y] += cellCrop(value, w)
		if b.id != "" {
			body.targets = append(body.targets, hitRegion{ID: b.id, X: x, Y: y, W: w, H: 1})
		}
		x += w + 2
	}
	return body
}

func (m Model) profileInfo(name string) app.ProfileInfo {
	for _, info := range m.snapshot.ProfileDetails {
		if info.ID == name {
			return info
		}
	}
	return app.ProfileInfo{}
}

func snapshotProfileName(snapshot app.Snapshot, id string) string {
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
	selected := name == m.snapshot.Applied
	surface := panel
	if m.hover == id || m.hover == refreshID || m.focus == id || m.focus == refreshID {
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
	canUpdate := info.Refreshable
	titleWidth := inner
	if canUpdate {
		titleWidth -= 4
	}
	title := themed(lipgloss.NewStyle().Bold(true).Render(cellCrop(prefix+clean(info.Name), max(1, titleWidth))), titleColor, surface, false)
	if canUpdate {
		color := muted
		if m.focus == refreshID || m.hover == refreshID {
			color = focus
		}
		title = cellPad(title, max(1, inner-3)) + themed("[↻]", color, surface, false)
	}
	origin, kind := "Built-in · No subscription", "Mihomo configuration"
	if name != "default" {
		origin = info.Origin
		if info.HasOverrides {
			kind += " · Overrides"
		}
	}
	status, statusColor := "Ready", muted
	if !info.Validated && name != "default" {
		status, statusColor = "Engine validation pending", warning
	}
	if selected {
		status, statusColor = "SELECTED · Next start", accent
		if m.snapshot.ServiceError != "" || m.snapshot.APIError != "" {
			status = "SELECTED · Runtime unavailable"
		} else if m.snapshot.Service.Active {
			status = "IN USE"
		}
	}
	if info.Pending {
		status, statusColor = "CHANGES PENDING · Apply to load", warning
	}
	stamp := "Built-in configuration"
	if name != "default" {
		stamp = profileUpdated(info.UpdatedAt, time.Now())
	}
	lines := []string{title, themed(cellCrop(clean(origin), inner), muted, surface, false), themed(cellCrop(kind, inner), muted, surface, false),
		themed(cellCrop(stamp, inner), muted, surface, false), themed(cellCrop(status, inner), statusColor, surface, false)}
	block := dashboardBlock{lines: profileCardSurface(lines, width, surface, selected)}
	// The small update button wins hit testing over the containing card.
	if canUpdate && width >= 12 {
		block.targets = append(block.targets, hitRegion{ID: refreshID, X: width - 5, Y: 1, W: 3, H: 1})
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

func profileUpdated(updated, now time.Time) string {
	if updated.IsZero() {
		return "No update history"
	}
	age := now.Sub(updated)
	switch {
	case age < time.Minute:
		return "Updated just now"
	case age < time.Hour:
		return fmt.Sprintf("Updated %dm ago", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("Updated %dh ago", int(age/time.Hour))
	case age < 30*24*time.Hour:
		return fmt.Sprintf("Updated %dd ago", int(age/(24*time.Hour)))
	default:
		return "Updated " + updated.Format("2006-01-02")
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
	body := dashboardBlock{lines: []string{themed(cellCrop(clean(info.Name)+" · Configuration", width), focus, bg, false)}}
	if name == "default" {
		body.lines = append(body.lines, themed(cellCrop("Built-in local proxy configuration. No subscription required.", width), muted, bg, false))
	} else {
		body.lines = append(body.lines, themed(cellCrop(clean(info.Origin), width), muted, bg, false))
	}
	applyID, label := fmt.Sprintf("profiles.apply.%d", index), "Use profile"
	if name == m.snapshot.Applied {
		label = "Reapply"
	}
	if info.Pending {
		label = "Apply changes"
	}
	if !m.snapshot.Installed {
		applyID, label = "", "Install core to apply"
	}
	buttons := []profileButton{{applyID, label, true}}
	if info.Refreshable {
		buttons = append(buttons, profileButton{fmt.Sprintf("profiles.update.%d", index), "Update", false})
	}
	if name != "default" {
		buttons = append(buttons, profileButton{fmt.Sprintf("profiles.rename.%d", index), "Rename", false})
		buttons = append(buttons, profileButton{fmt.Sprintf("profiles.url-edit.%d", index), "Edit URL", false})
		buttons = append(buttons, profileButton{fmt.Sprintf("profiles.overrides.%d", index), "Overrides", false}, profileButton{fmt.Sprintf("profiles.delete.%d", index), "Delete", false})
	}
	buttons = append(buttons, profileButton{"profiles.close", "Close", false})
	body.append(m.profileButtons(buttons, width))
	return body
}
