package tui

import (
	"cmp"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func overviewModeCard(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
	inner := overviewInnerWidth(width)
	action := overviewControlButton{id: "card.proxies", label: tr("dashboard.overview.proxiesAction"), enabled: s.Installed && !o.Busy}
	panelWith := func(content dashboardBlock) dashboardBlock {
		return overviewActionPanel(o, width, tr("dashboard.overview.route"), centerContentBlock(content, inner, len(content.lines)), action)
	}
	if o.StateUnavailable {
		return panelWith(overviewTextBlock([]string{themed(tr("dashboard.overview.selectionUnavailable"), muted, panel, o.Dim)}, inner))
	}
	if !s.Installed {
		return panelWith(overviewTextBlock([]string{themed(tr("dashboard.control.modeAfterInstall"), muted, panel, o.Dim)}, inner))
	}
	issue := runtimeControlIssue(s, o.Locale)
	if issue == "" && s.Service.Active {
		if s.Mode.Error != "" {
			issue = clean(o.Locale.Diagnostic(s.Mode.Error, s.Mode.ErrorDiagnostic))
		} else if s.APIError != "" {
			issue = clean(o.Locale.Diagnostic(s.APIError, s.APIDiagnostic))
		}
	}
	content := renderProxyModeSegments(o, proxyModeChoices(o.Locale, issue == ""), inner, panel)
	add := func(value, color string) {
		content.append(overviewTextBlock([]string{themed(value, color, panel, o.Dim)}, inner))
	}
	known := s.Service.Active && s.ServiceError == "" && s.APIError == "" && s.Mode.Error == "" && mihomo.ValidMode(s.Mode.Actual)
	if !known || s.Mode.Desired != s.Mode.Actual {
		saved, actual := tr("dashboard.status.unknown"), tr("dashboard.status.unknown")
		if mihomo.ValidMode(s.Mode.Desired) {
			saved = dashboardModeLabel(s.Mode.Desired, o.Locale)
		}
		if known {
			actual = dashboardModeLabel(s.Mode.Actual, o.Locale)
		}
		add(tr("dashboard.control.savedMode", saved), muted)
		if s.Service.Active {
			add(tr("dashboard.control.runningMode", actual), warning)
		}
	}
	if issue != "" {
		add(issue, warning)
	}

	content.blank()
	switch {
	case !known:
		if s.ServiceError == "" && !s.Service.Active {
			add(tr("dashboard.groups.startHint"), muted)
		} else {
			add(tr("dashboard.overview.selectionUnavailable"), muted)
		}
	case s.Mode.Actual == "direct":
		add(tr("dashboard.overview.modeDirect"), muted)
	default:
		group := overviewRouteGroup(s.Groups, s.Mode.Actual, o.SelectedGroup)
		if group != nil {
			add(tr("dashboard.overview.groupSelection"), muted)
			content.lines = append(content.lines, overviewGroupSelection(o, *group, inner)...)
		} else if s.Mode.Actual == "global" {
			add(tr("dashboard.overview.selectionUnavailable"), muted)
		} else {
			add(tr("dashboard.groups.empty"), muted)
		}
	}
	return panelWith(content)
}

// Rule mode can route through many groups. Show the group being browsed, or
// the preferred rule group, while reserving GLOBAL for the global mode.
func overviewRouteGroup(groups []mihomo.Group, mode, selected string) *mihomo.Group {
	var proxy, selector, first *mihomo.Group
	for i := range groups {
		g := &groups[i]
		if (g.Name == "GLOBAL") != (mode == "global") {
			continue
		}
		if mode == "global" || g.Name == selected {
			return g
		}
		if g.Name == "PROXY" {
			proxy = g
		}
		if selector == nil && g.Type == "Selector" {
			selector = g
		}
		if first == nil {
			first = g
		}
	}
	return cmp.Or(proxy, selector, first)
}

func overviewGroupSelection(o dashboardOptions, group mihomo.Group, width int) []string {
	member := clean(group.Now)
	color := text
	switch {
	case group.Type == "LoadBalance" || group.Type == "Relay":
		member, color = o.Locale.T("proxies.distributed"), muted
	case member == "":
		member, color = o.Locale.T("proxies.no_selection"), muted
	}
	name := themed(clean(group.Name), text, panel, o.Dim)
	value := themed(lipgloss.NewStyle().Bold(color == text).Render(member), color, panel, o.Dim)
	path := name + themed(" → ", muted, panel, o.Dim) + value
	if ansi.StringWidth(path) <= width {
		return []string{path}
	}
	return []string{cellCrop(name, width), cellCrop(themed("→ ", muted, panel, o.Dim)+value, width)}
}
