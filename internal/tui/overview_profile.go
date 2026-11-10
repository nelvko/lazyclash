package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func overviewProfileCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := o.Snapshot
	name, status := tr("dashboard.profile.none"), tr("dashboard.profile.notApplied")
	if s.ConfigReady {
		name, status = snapshotProfileName(s, s.Applied, o.Locale), tr("dashboard.profile.ready")
		if name == "" {
			name = tr("profiles.builtin_name")
		}
		if !s.HasRun && (s.Applied == "default" || s.Applied == "") {
			status = tr("dashboard.profile.builtinReady")
		}
	}
	var info app.ProfileInfo
	for _, candidate := range s.ProfileDetails {
		if candidate.ID == s.Applied {
			info = candidate
			break
		}
	}
	statusColor := muted
	if s.ConfigReady && (s.ProfilePending || info.Pending) {
		status, statusColor = tr("profiles.changes_pending"), warning
	} else if s.ConfigReady && info.ID != "" && !info.Validated {
		status, statusColor = tr("profiles.validation_pending"), warning
	}
	lines := []string{
		themed(lipgloss.NewStyle().Bold(true).Render(clean(name)), text, panel, o.Dim),
		themed(status, statusColor, panel, o.Dim),
	}
	var details [][2]string
	if info.ID != "" {
		origin := clean(info.Origin)
		switch info.Kind {
		case "local":
			origin = tr("profiles.create_local")
		case "imported":
			origin = tr("profiles.imported_origin")
		case "builtin":
			origin = tr("profiles.builtin_origin")
		}
		if origin != "" {
			details = append(details, [2]string{tr("dashboard.overview.sourceLabel"), origin})
		}
		if !info.UpdatedAt.IsZero() {
			now := o.Now
			if now.IsZero() {
				now = time.Now()
			}
			details = append(details, [2]string{tr("dashboard.overview.updatedLabel"), overviewProfileUpdated(info.UpdatedAt, now, o.Locale)})
		}
	}
	if len(details) > 0 {
		lines = append(lines, "")
		lines = append(lines, overviewProfileDetailRows(o, details, max(1, width-4))...)
	}
	saved := 0
	for _, id := range s.Profiles {
		if id != "default" {
			saved++
		}
	}
	if !s.ConfigReady && saved == 0 {
		lines = append(lines, themed(tr("dashboard.profile.importHint"), muted, panel, o.Dim))
	}
	return overviewActionPanel(o, width, tr("dashboard.profile.title"), overviewTextBlock(lines, max(1, width-4)), overviewControlButton{
		id: "card.profiles", label: tr("dashboard.overview.profilesAction"), enabled: !o.Busy,
	})
}

func overviewProfileDetailRows(o dashboardOptions, details [][2]string, width int) []string {
	labelWidth := 0
	for _, field := range details {
		labelWidth = max(labelWidth, ansi.StringWidth(field[0]))
	}
	var lines []string
	for _, field := range details {
		valueWidth := width - labelWidth - 2
		if valueWidth < 8 {
			lines = append(lines, themed(field[0], muted, panel, o.Dim))
			for _, line := range strings.Split(wrapText(field[1], width), "\n") {
				lines = append(lines, themed(line, muted, panel, o.Dim))
			}
			continue
		}
		for row, line := range strings.Split(wrapText(field[1], valueWidth), "\n") {
			label := strings.Repeat(" ", labelWidth)
			if row == 0 {
				label = cellPad(field[0], labelWidth)
			}
			lines = append(lines, themed(label+"  "+line, muted, panel, o.Dim))
		}
	}
	return lines
}

func overviewProfileUpdated(updated, now time.Time, locale i18n.Localizer) string {
	age := now.Sub(updated)
	switch {
	case age < time.Minute:
		return locale.T("dashboard.overview.updatedNow")
	case age < time.Hour:
		return locale.T("dashboard.overview.updatedMinutes", int(age/time.Minute))
	case age < 24*time.Hour:
		return locale.T("dashboard.overview.updatedHours", int(age/time.Hour))
	case age < 30*24*time.Hour:
		return locale.T("dashboard.overview.updatedDays", int(age/(24*time.Hour)))
	default:
		return updated.Format("2006-01-02")
	}
}
