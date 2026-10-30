package tui

import "strings"

// An unreadable library is not an empty library. Keep the first-load failure
// separate from the last successful snapshot, which survives refresh errors.
func (m Model) unavailableProfiles(width int) (string, []hitRegion) {
	if m.busy {
		return themed(cellCrop("Loading configurations…", width), muted, bg, false), nil
	}
	body := dashboardBlock{lines: []string{
		themed(cellCrop("Profiles unavailable", width), errorColor, bg, false), "",
	}}
	body.lines = append(body.lines, strings.Split(wrapText("Could not read saved configurations. Resolve the reported error, then retry.", width), "\n")...)
	body.blank()
	body.append(m.profileButtons([]profileButton{{id: "page.refresh", label: "Retry"}}, width))
	return strings.Join(body.lines, "\n"), body.targets
}
