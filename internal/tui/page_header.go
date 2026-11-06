package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type headerAction struct {
	id, label, compact string
	enabled            bool
}

type pageHeader struct {
	summary string
	actions []headerAction
}

func (h pageHeader) owns(id string) bool {
	for _, action := range h.actions {
		if action.id == id {
			return true
		}
	}
	return false
}

func (h pageHeader) enabled(id string) bool {
	for _, action := range h.actions {
		if action.id == id {
			return action.enabled
		}
	}
	return false
}

func (m Model) hasRefreshableProfiles() bool {
	for _, profile := range m.snapshot.ProfileDetails {
		if profile.Refreshable {
			return true
		}
	}
	return false
}

func (m Model) pageHeader() pageHeader {
	tr := m.tr
	h := pageHeader{}
	switch m.page {
	case "profiles":
		h.actions = []headerAction{{"profiles.update-all", tr("header.updateAll"), "↻", m.hasRefreshableProfiles()}}
	case "proxies":
		if m.snapshot.Installed && m.snapshot.Service.Active && m.snapshot.ServiceError == "" && m.snapshot.APIError == "" {
			count := 0
			for _, g := range m.snapshot.Groups {
				if (m.snapshot.Mode.Actual == "global") == (g.Name == "GLOBAL") {
					count++
				}
			}
			h.summary = tr("header.groups", count)
		}
		h.actions = []headerAction{{"page.refresh", tr("header.refresh"), "↻", true}}
	case "connections":
		if m.runtime.received && m.runtime.snapshot.State == "ready" && m.runtime.snapshot.Connections != nil {
			h.summary = tr("header.active", len(m.runtime.snapshot.Connections.Connections))
		}
		h.actions = []headerAction{{"conn.retry", tr("header.refresh"), "↻", !m.connections.confirming}}
	case "rules":
		_, providersSupported := m.backend.(ruleProviderBackend)
		_, customSupported := m.backend.(customRulesBackend)
		switch m.rules.view {
		case "providers":
			h.summary = tr("rules.providers.title")
			h.actions = []headerAction{{"rules.providers-back", tr("rules.management.back"), "←", true}, {"rules.providers-all", tr("rules.providers.update_all"), "↻", m.ruleProvidersCanUpdateAll()}}
		case "custom":
			s := m.customRules
			available := s.loaded && !s.loading && !s.saving && !s.applying && !s.inputOpen && s.confirm == ""
			h.summary = tr("custom_rules.title")
			h.actions = []headerAction{
				{"rules.custom-back", tr("rules.management.back"), "←", !s.saving && !s.applying},
				{"rules.custom-add", tr("custom_rules.add"), "+", available},
				{"rules.custom-save", tr("custom_rules.save"), "✓", available && m.customRulesDirty()},
				{"rules.custom-apply", tr("custom_rules.apply"), "▶", available && !m.customRulesDirty() && s.draft.Pending},
			}
		default:
			h.actions = []headerAction{{"rules.providers", tr("rules.providers.title"), "≡", providersSupported}, {"rules.custom", tr("custom_rules.title"), "✎", customSupported && m.snapshot.Applied != "" && m.snapshot.Applied != "default"}}
		}
	case "logs":
		h.summary = tr("header.connecting")
		follow, compact := tr("header.resume"), "▶"
		if m.logs.active {
			h.summary = tr("header.live")
			follow, compact = tr("header.pause"), "Ⅱ"
		} else if m.logs.paused {
			h.summary = tr("header.paused")
		} else if m.logs.ended {
			h.summary = tr("header.ended")
		}
		h.actions = []headerAction{{"logs.clear", tr("logs.clear"), "×", true}, {"logs.follow", follow, compact, true}, {"logs.retry", tr("header.reconnect"), "↻", true}}
	case "settings":
		h.actions = []headerAction{{"network", tr("header.network"), tr("header.networkCompact"), true}}
	}
	return h
}

// On small terminals actions get their own header row. They remain outside the
// scrolling body, with compact labels when the full labels do not fit.
func renderPageHeader(o dashboardOptions, g dashboardLayout, titleX, titleWidth int) (string, int, int, []hitRegion) {
	h := o.Header
	if o.Page == "overview" {
		status, _ := coreSummary(o.Snapshot, o.Locale)
		h = pageHeader{summary: "Mihomo · " + status}
	}
	y, left := g.headerY, titleX+titleWidth+2
	if o.Width < 70 && o.Page != "overview" {
		y, left = g.headerY+1, g.headerX
	}
	right := g.headerX + g.headerWidth
	available := max(0, right-left)
	gap, squeeze := 2, false
	buttons := func(compact bool) ([]string, int) {
		var values []string
		total := 0
		for _, action := range h.actions {
			label := action.label
			if compact {
				label = action.compact
			}
			if squeeze && ansi.StringWidth(label) > 1 {
				label = string([]rune(label)[0])
			}
			options := dashboardButtonOptions(action.id, o, false, sidebarBackground)
			options.Disabled = !action.enabled || o.Busy
			value := renderButton(label, options)
			values = append(values, value)
			total += ansi.StringWidth(value)
		}
		return values, total + max(0, len(values)-1)*gap
	}
	values, width := buttons(false)
	if width > available {
		values, width = buttons(true)
	}
	if width > available {
		gap, squeeze = 0, true
		values, width = buttons(true)
	}
	for width > available && len(h.actions) > 1 {
		h.actions = h.actions[1:]
		values, width = buttons(true)
	}
	var hits []hitRegion
	x := max(left, right-width)
	for i, action := range h.actions {
		w := ansi.StringWidth(values[i])
		if action.enabled && !o.Busy {
			hits = append(hits, hitRegion{ID: action.id, X: x, Y: y, W: w, H: 1})
		}
		x += w + gap
	}
	value := strings.Join(values, themed(strings.Repeat(" ", gap), text, sidebarBackground, o.Dim))
	if h.summary != "" {
		summaryWidth := available - width
		if width > 0 {
			summaryWidth -= 2
		}
		if summaryWidth >= ansi.StringWidth(h.summary) {
			prefix := themed(h.summary, muted, sidebarBackground, o.Dim)
			if width > 0 {
				prefix += themed("  ", text, sidebarBackground, o.Dim)
			}
			value = prefix + value
		}
	}
	return value, max(left, right-ansi.StringWidth(value)), y, hits
}
