package tui

import "strings"

// An unreadable library is not an empty library. Keep the first-load failure
// separate from the last successful snapshot, which survives refresh errors.
func (m Model) unavailableProfiles(width int) (string, []hitRegion) {
	height := dashboardGeometry(m.dashboardOptions()).height
	if m.busy {
		body := m.emptyState(width, height, emptyStateOptions{Title: m.tr("profiles.loading")})
		return strings.Join(body.lines, "\n"), body.targets
	}
	body := m.emptyState(width, height, emptyStateOptions{
		Title: m.tr("profiles.unavailable"), Detail: m.tr("profiles.read_failure"), Error: true,
		ActionID: "page.refresh", ActionLabel: m.tr("profiles.retry"),
	})
	return strings.Join(body.lines, "\n"), body.targets
}
