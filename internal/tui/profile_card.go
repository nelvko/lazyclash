package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

// The subscription grid reserves all three detail rows. The overview uses the
// same rows when populated and places its status directly below the name.
func profileCardDetails(info app.ProfileInfo, now time.Time, locale i18n.Localizer, inner int, surface, originColor, status string) []string {
	age := profileAge(info.UpdatedAt, now, locale)
	subtitle := strings.TrimSpace(clean(info.Description))
	if subtitle == "" {
		subtitle = profileOrigin(info, locale)
		if subtitle != "" && info.Kind == "remote" && originColor != muted {
			subtitle = themed(subtitle, originColor, surface, false)
		}
	}
	metadata := profileCardPair(subtitle, age, inner)
	quota, expiresAt := profileAllowance(info)
	expiry := profileCardExpiry(expiresAt, now, locale)
	// Keep the numeric balance intact when the date would displace it.
	// The detail pane always has room for both, on separate rows.
	if quota != "" && ansi.StringWidth(quota)+2+ansi.StringWidth(expiry) > inner {
		expiry = ""
	}
	allowance := profileCardPair(quota, expiry, inner)
	footer := themed(cellCrop(status, inner), warning, surface, false)
	if info.Quota != nil && info.Quota.TotalBytes > 0 {
		barWidth := inner
		if status != "" {
			barWidth -= ansi.StringWidth(status) + 2
		}
		if barWidth >= 4 {
			bar := progress.New(progress.WithWidth(barWidth), progress.WithoutPercentage(), progress.WithFillCharacters('━', '─'))
			bar.FullColor, bar.EmptyColor = lipgloss.Color(success), lipgloss.Color(border)
			total := info.Quota.TotalBytes
			left := total - min(total, info.Quota.UsedBytes)
			graph := bar.ViewAs(float64(left) / float64(total))
			if status != "" {
				graph += "  " + footer
			}
			footer = graph
		}
	}
	return []string{themed(metadata, muted, surface, false), themed(allowance, text, surface, false), footer}
}

// The right-hand value shares the card controls' right margin.
func profileCardPair(left, right string, width int) string {
	if right == "" {
		return cellCrop(left, width)
	}
	right = cellCrop(right, width)
	available := width - ansi.StringWidth(right)
	return cellPad(cellCrop(left, max(0, available-1)), available) + right
}

func profileAllowance(info app.ProfileInfo) (amount string, expiresAt int64) {
	if q := info.Quota; q != nil {
		if q.TotalBytes > 0 {
			remaining := q.TotalBytes - min(q.TotalBytes, q.UsedBytes)
			amount = formatBytes(remaining) + " / " + formatBytes(q.TotalBytes)
		}
		expiresAt = q.ExpiresAt
	}
	return amount, expiresAt
}

func profileCardExpiry(expiresAt int64, now time.Time, locale i18n.Localizer) string {
	if expiresAt <= 0 {
		return ""
	}
	if now.Unix() >= expiresAt {
		return locale.T("profiles.expired")
	}
	return time.Unix(expiresAt, 0).Format(time.DateOnly)
}

func profileWarning(info app.ProfileInfo, snapshot *app.Snapshot, locale i18n.Localizer) string {
	switch {
	case info.ID == snapshot.Applied && (snapshot.ServiceError != "" || (snapshot.Service.Active && snapshot.APIError != "")):
		return locale.T("profiles.selected_unavailable")
	case info.Refreshable && (info.Pending || (info.ID == snapshot.Applied && snapshot.ProfilePending)):
		return locale.T("profiles.refresh_failed")
	case !info.Validated:
		return locale.T("profiles.validation_pending")
	default:
		return ""
	}
}

func profileAge(updated, now time.Time, locale i18n.Localizer) string {
	if updated.IsZero() {
		return ""
	}
	age := now.Sub(updated)
	switch {
	case age < time.Minute:
		return locale.T("profiles.just_updated")
	case age < time.Hour:
		return locale.T("profiles.updated_minutes", int(age/time.Minute))
	case age < 24*time.Hour:
		return locale.T("profiles.updated_hours", int(age/time.Hour))
	default:
		return locale.T("profiles.updated_days", int(age/(24*time.Hour)))
	}
}

func profileOrigin(info app.ProfileInfo, locale i18n.Localizer) string {
	switch info.Kind {
	case "local":
		return locale.T("profiles.create_local")
	case "imported":
		return locale.T("profiles.imported_origin")
	default:
		return strings.TrimSpace(clean(info.Origin))
	}
}
