package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func overviewModeCard(o dashboardOptions, width int) dashboardBlock {
	tr, s := o.Locale.T, o.Snapshot
	inner := max(1, width-4)
	action := overviewControlButton{id: "card.proxies", label: tr("dashboard.overview.proxiesAction"), enabled: s.Installed && !o.Busy}
	if !s.Installed {
		return overviewActionPanel(o, width, tr("dashboard.overview.route"), overviewTextBlock([]string{
			themed(tr("dashboard.control.modeAfterInstall"), muted, panel, o.Dim),
		}, inner), action)
	}
	names := map[string]string{"rule": tr("dashboard.mode.rule"), "global": tr("dashboard.mode.global"), "direct": tr("dashboard.mode.direct")}
	issue := runtimeControlIssue(s, o.Locale)
	if issue == "" && s.Service.Active {
		if s.Mode.Error != "" {
			issue = clean(o.Locale.Diagnostic(s.Mode.Error, s.Mode.ErrorDiagnostic))
		} else if s.APIError != "" {
			issue = clean(o.Locale.Diagnostic(s.APIError, s.APIDiagnostic))
		}
	}
	var buttons []overviewControlButton
	for _, mode := range []string{"rule", "global", "direct"} {
		buttons = append(buttons, overviewControlButton{id: "mode." + mode, label: names[mode], enabled: !o.Busy && issue == "", selected: s.Mode.Desired == mode})
	}
	content := overviewButtons(o, inner, buttons)
	known := s.Service.Active && s.ServiceError == "" && s.APIError == "" && s.Mode.Error == "" && names[s.Mode.Actual] != ""
	if known {
		key := map[string]string{"rule": "dashboard.overview.modeRule", "global": "dashboard.overview.modeGlobal", "direct": "dashboard.overview.modeDirect"}[s.Mode.Actual]
		content.append(overviewTextBlock([]string{themed(tr(key), muted, panel, o.Dim)}, inner))
	}
	if !known || s.Mode.Desired != s.Mode.Actual {
		saved, actual := names[s.Mode.Desired], tr("dashboard.status.unknown")
		if saved == "" {
			saved = tr("dashboard.status.unknown")
		}
		if known {
			actual = names[s.Mode.Actual]
		}
		content.append(overviewTextBlock([]string{themed(tr("dashboard.control.savedMode", saved), muted, panel, o.Dim)}, inner))
		if s.Service.Active {
			content.append(overviewTextBlock([]string{themed(tr("dashboard.control.runningMode", actual), warning, panel, o.Dim)}, inner))
		}
	}
	if issue != "" {
		content.append(overviewTextBlock([]string{themed(issue, warning, panel, o.Dim)}, inner))
	}
	if !known || s.Mode.Actual != "direct" {
		content.blank()
		content.append(overviewTextBlock([]string{themed(strings.Repeat("─", inner), divider, panel, o.Dim)}, inner))
		content.append(overviewTextBlock(overviewNodeLines(o), inner))
	}
	return overviewActionPanel(o, width, tr("dashboard.overview.route"), content, action)
}

func overviewNodeLines(o dashboardOptions) []string {
	tr, s := o.Locale.T, o.Snapshot
	style := func(value, color string) string { return themed(clean(value), color, panel, o.Dim) }
	status := tr("dashboard.groups.inactive")
	if !s.Installed {
		status = tr("dashboard.groups.afterStart")
	} else if s.ServiceError != "" {
		status = tr("dashboard.groups.serviceUnavailable")
	} else if s.Service.Active && s.APIError != "" {
		status = tr("dashboard.groups.apiUnavailable")
	} else if s.Service.Active {
		status = tr("dashboard.groups.empty")
		if s.Mode.Error != "" || s.Mode.Actual != "rule" && s.Mode.Actual != "global" {
			status = tr("dashboard.overview.nodeUnavailable")
		} else if len(s.Groups) > 0 {
			group := s.Groups[0]
			name := o.SelectedGroup
			if s.Mode.Actual == "global" {
				name = "GLOBAL"
			}
			found := false
			for _, candidate := range s.Groups {
				if candidate.Name == name {
					group, found = candidate, true
					break
				}
			}
			if !found && s.Mode.Actual == "global" {
				return []string{style(tr("dashboard.overview.currentNode"), muted), style(tr("dashboard.overview.nodeUnavailable"), muted)}
			}
			if !found {
				for _, candidate := range s.Groups {
					if candidate.Name == "PROXY" {
						group, found = candidate, true
						break
					}
				}
			}
			if !found {
				for _, candidate := range s.Groups {
					if candidate.Type == "Selector" {
						group = candidate
						break
					}
				}
			}
			node := clean(group.Now)
			if node == "" {
				node = "--"
			}
			return []string{style(tr("dashboard.overview.currentNode"), muted), lipgloss.NewStyle().Bold(true).Render(style(node, text))}
		}
	}
	lines := []string{style(tr("dashboard.overview.currentNode"), muted), style(status, muted)}
	if s.Installed && !s.Service.Active && s.ServiceError == "" {
		lines = append(lines, style(tr("dashboard.groups.startHint"), muted))
	}
	return lines
}
