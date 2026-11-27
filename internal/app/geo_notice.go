package app

import (
	"strings"

	"github.com/nelvko/lazyclash/internal/i18n"
)

// Summary renders the same per-file outcome for the installer and Settings.
func (g GeoPreparation) Summary(locale i18n.Localizer) string {
	var parts []string
	if g.Cancelled {
		parts = append(parts, locale.T("geo.preparation.cancelled"))
	}
	for _, item := range []struct {
		label string
		files []string
	}{
		{"geo.preparation.ready", g.Prepared},
		{"geo.preparation.present", g.Present},
		{"geo.preparation.skipped", g.Skipped},
		{"geo.preparation.failed", g.Failed},
	} {
		if len(item.files) > 0 {
			parts = append(parts, locale.T(item.label, strings.Join(item.files, ", ")))
		}
	}
	if g.Warning != "" {
		parts = append(parts, locale.Diagnostic(g.Warning, g.Diagnostic))
	}
	return strings.Join(parts, " · ")
}
