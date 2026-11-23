package tui

import (
	"time"

	"github.com/charmbracelet/x/ansi"
)

func overviewProfileCard(o dashboardOptions, width int) dashboardBlock {
	tr := o.Locale.T
	s := &o.Snapshot
	inner := overviewInnerWidth(width)
	name := snapshotProfileName(*s, s.Applied, o.Locale)
	lines := []string{overviewHeading(o, clean(name))}
	info := snapshotProfileDetails(s, s.Applied)
	switch {
	case s.Applied != "" && !s.ConfigReady:
		lines = append(lines, themed(tr("dashboard.profile.notApplied"), muted, panel, o.Dim))
	case info != nil:
		if status := profileWarning(*info, s, o.Locale); status != "" {
			lines = append(lines, themed(status, warning, panel, o.Dim))
		}
	}
	content := overviewTextBlock(lines, inner)
	if info != nil {
		now := o.Now
		if now.IsZero() {
			now = time.Now()
		}
		for _, row := range profileCardDetails(*info, now, o.Locale, inner, panel, "") {
			if ansi.StringWidth(row) > 0 {
				content.lines = append(content.lines, row)
			}
		}
	}
	return overviewActionPanel(o, width, tr("dashboard.profile.title"), content, overviewControlButton{
		id: "card.profiles", label: tr("dashboard.overview.profilesAction"), enabled: !o.Busy,
	})
}
