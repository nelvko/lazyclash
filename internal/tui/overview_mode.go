package tui

import (
	"cmp"
	"strings"

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
	known := s.Service.Active && s.ServiceError == "" && s.APIError == "" && s.Mode.Error == "" && mihomo.ValidMode(s.Mode.Actual)
	selectedMode := s.Mode.Desired
	if known {
		selectedMode = s.Mode.Actual
	}
	content := renderProxyModeSegments(o, proxyModeChoices(o.Locale, issue == ""), inner, buttonRoutingSegment, selectedMode)
	add := func(value, color string) {
		content.append(overviewTextBlock([]string{themed(value, color, panel, o.Dim)}, inner))
	}
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
		if known && mihomo.ValidMode(s.Mode.Desired) {
			add(tr("diagnostic.snapshot.modeDrift"), warning)
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
		group, canChoose := overviewRouteGroup(s.Groups, s.Mode.Actual, o.SelectedGroup)
		if group != nil {
			content.append(overviewGroupSelection(o, *group, canChoose && !o.Busy, inner))
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
func overviewRouteGroup(groups []mihomo.Group, mode, selected string) (group *mihomo.Group, canChoose bool) {
	var requested, proxy, selector, first *mihomo.Group
	count := 0
	for i := range groups {
		g := &groups[i]
		if (g.Name == "GLOBAL") != (mode == "global") {
			continue
		}
		if mode == "global" {
			return g, false
		}
		count++
		if g.Name == selected {
			requested = g
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
	return cmp.Or(requested, proxy, selector, first), count > 1
}

// Wrap between hops so each arrow stays with its destination. The card centers
// the resulting rows and their hit regions together.
func overviewGroupSelection(o dashboardOptions, group mihomo.Group, canChoose bool, width int) dashboardBlock {
	name := clean(group.Name)
	row := themed(cellCrop(name, width), text, panel, o.Dim)
	var block dashboardBlock
	trailing := ""
	if canChoose {
		// Balance the popup's keyboard-focus gutter on the right of this row.
		gutter := min(dropdownGutter, max(0, width-1))
		marker := strings.Repeat(" ", gutter)
		trailing = strings.Repeat(" ", min(gutter, max(0, width-gutter-3)))
		expanded := o.OpenDropdown == overviewRouteGroupTarget
		focused := o.Focus == overviewRouteGroupTarget || expanded
		if focused && gutter > 0 {
			marker = "›" + marker[1:]
		}
		arrow := "▾"
		if expanded {
			arrow = "▴"
		}
		labelWidth := width - gutter - len(trailing) - 2
		row = themed(marker, focus, panel, o.Dim)
		if labelWidth > 0 {
			label := lipgloss.NewStyle().Underline(focused || o.Hover == overviewRouteGroupTarget).Render(cellCrop(name, labelWidth))
			row += themed(label+" ", text, panel, o.Dim)
		}
		row += themed(arrow, accent, panel, o.Dim)
		block.targets = []hitRegion{{ID: overviewRouteGroupTarget, W: ansi.StringWidth(row), H: 1}}
		if expanded {
			row += trailing
			block.lines = []string{row}
			block.targets[0].W = ansi.StringWidth(row)
			return block
		}
	}

	members, hasEndpoint := overviewRoutePath(o.Snapshot.Groups, group, o.Locale)
	for i, member := range members {
		value := member
		color := text
		if i == len(members)-1 {
			if hasEndpoint {
				value = lipgloss.NewStyle().Bold(true).Render(value)
			} else {
				color = muted
			}
		}
		hop := themed("→ ", muted, panel, o.Dim) + themed(value, color, panel, o.Dim)
		if ansi.StringWidth(row)+1+ansi.StringWidth(hop)+len(trailing) > width {
			block.lines = append(block.lines, row+trailing)
			row, trailing = cellCrop(hop, width), ""
		} else {
			row += " " + hop
		}
	}
	block.lines = append(block.lines, row+trailing)
	return block
}
