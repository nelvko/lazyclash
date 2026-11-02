package tui

import "strings"

// An unreadable library is not an empty library. Keep the first-load failure
// separate from the last successful snapshot, which survives refresh errors.
func (m Model) unavailableProfiles(width int) (string, []hitRegion) {
	if m.busy {
		return themed(cellCrop(m.tr("profiles.loading"), width), muted, bg, false), nil
	}
	body := dashboardBlock{lines: []string{
		themed(cellCrop(m.tr("profiles.unavailable"), width), errorColor, bg, false), "",
	}}
	body.lines = append(body.lines, strings.Split(wrapText(m.tr("profiles.read_failure"), width), "\n")...)
	body.blank()
	body.append(m.profileButtons([]profileButton{{id: "page.refresh", label: m.tr("profiles.retry")}}, width))
	return strings.Join(body.lines, "\n"), body.targets
}
