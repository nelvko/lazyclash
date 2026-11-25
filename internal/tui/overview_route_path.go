package tui

import (
	"slices"
	"strings"

	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// overviewRoutePath follows the selected members reported in one snapshot.
// The caller renders the initial group separately. The last item is either
// a reported outbound (hasEndpoint) or a localized explanation.
func overviewRoutePath(groups []mihomo.Group, group mihomo.Group, locale i18n.Localizer) (members []string, hasEndpoint bool) {
	byName := make(map[string]*mihomo.Group, len(groups))
	for i := range groups {
		byName[groups[i].Name] = &groups[i]
	}
	seen := make(map[string]bool)
	for {
		seen[group.Name] = true
		member := clean(group.Now)
		switch {
		case group.Type == "LoadBalance" || group.Type == "Relay":
			return append(members, locale.T("proxies.distributed")), false
		case strings.TrimSpace(member) == "":
			return append(members, locale.T("proxies.no_selection")), false
		case group.All != nil && !slices.Contains(group.All, group.Now):
			return append(members, locale.T("dashboard.overview.selectionUnavailable")), false
		case seen[group.Now]:
			return append(members, locale.T("proxies.cycle")), false
		}
		members = append(members, member)
		next := byName[group.Now]
		if next == nil {
			return members, true
		}
		group = *next
	}
}
