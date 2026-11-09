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
	leading []headerAction
	actions []headerAction
}

func (h pageHeader) owns(id string) bool {
	for _, action := range append(h.leading, h.actions...) {
		if action.id == id {
			return true
		}
	}
	return false
}

func (h pageHeader) enabled(id string) bool {
	for _, action := range append(h.leading, h.actions...) {
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
	case "proxies":
		if m.width >= 50 {
			for _, mode := range []string{"rule", "global", "direct"} {
				h.leading = append(h.leading, headerAction{"mode." + mode, tr("dashboard.mode." + mode), "", m.proxiesReady() && m.snapshot.Mode.Error == ""})
			}
		}
		h.actions = []headerAction{{"proxies.providers", tr("providers.title.proxies"), "≡", m.providersSupported(providerProxies)}}
	case "profiles":
		h.actions = []headerAction{{"profiles.update-all", tr("header.updateAll"), "↻", m.hasRefreshableProfiles()}}
	case "connections":
		if m.runtime.received && m.runtime.snapshot.State == "ready" && m.runtime.snapshot.Connections != nil {
			h.summary = tr("header.active", len(m.runtime.snapshot.Connections.Connections))
		}
		h.actions = []headerAction{{"conn.retry", tr("header.refresh"), "↻", !m.connections.confirming}}
	case "rules":
		h.actions = []headerAction{{"rules.providers", tr("providers.title.rules"), "≡", m.providersSupported(providerRules)}}
	case "logs":
		h.summary = tr("header.connecting")
		follow, compact := tr("logs.pause_display"), "Ⅱ"
		if m.logs.reconnecting {
			h.summary = tr("header.connecting")
		} else if m.logs.active {
			h.summary = tr("header.live")
		}
		if m.logs.paused {
			h.summary = tr("header.paused")
			follow, compact = tr("logs.resume_display"), "▶"
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
	if len(h.leading) > 0 {
		return renderLeadingPageHeader(o, g, titleX+titleWidth+2)
	}
	if o.Page == "overview" && (o.InlineInstall || overviewSetupVisible(o)) {
		status, _ := coreSummary(o.Snapshot, o.Locale)
		h = pageHeader{summary: "Mihomo · " + status}
	}
	y, left := g.headerY, titleX+titleWidth+2
	if o.Width < 70 && o.Page != "overview" {
		y, left = g.headerY+1, g.headerX
	}
	right := g.headerX + g.headerWidth
	available := max(0, right-left)
	squeeze := false
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
			options := dashboardButtonOptions(action.id, o, false, bg)
			options.Disabled = !action.enabled || o.Busy
			value := renderButton(label, options)
			values = append(values, value)
			total += ansi.StringWidth(value)
		}
		return values, total
	}
	values, width := buttons(false)
	if width > available {
		values, width = buttons(true)
	}
	if width > available {
		squeeze = true
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
		x += w
	}
	value := strings.Join(values, "")
	if h.summary != "" {
		summaryWidth := available - width
		if width > 0 {
			summaryWidth -= 2
		}
		if summaryWidth >= ansi.StringWidth(h.summary) {
			prefix := themed(h.summary, muted, bg, o.Dim)
			if width > 0 {
				prefix += themed("  ", text, bg, o.Dim)
			}
			value = prefix + value
		}
	}
	return value, max(left, right-ansi.StringWidth(value)), y, hits
}

// Keep the mode choices beside the title when they fit. On smaller terminals
// the controls share a dedicated header row, with the provider action at right.
func renderLeadingPageHeader(o dashboardOptions, g dashboardLayout, left int) (string, int, int, []hitRegion) {
	h := o.Header
	var leading []string
	labelWidth := 6
	for _, action := range h.leading {
		labelWidth = max(labelWidth, ansi.StringWidth(action.label))
	}
	for _, action := range h.leading {
		options := dashboardButtonOptions(action.id, o, false, bg)
		options.Selectable, options.Selected = true, o.Snapshot.Mode.Desired == strings.TrimPrefix(action.id, "mode.")
		options.Width, options.Disabled = labelWidth, !action.enabled || o.Busy
		leading = append(leading, renderButton(action.label, options))
	}
	renderActions := func(compact bool) []string {
		var values []string
		for _, action := range h.actions {
			label := action.label
			if compact {
				label = action.compact
			}
			options := dashboardButtonOptions(action.id, o, false, bg)
			options.Disabled = !action.enabled || o.Busy
			values = append(values, renderButton(label, options))
		}
		return values
	}
	actions := renderActions(false)
	modeWidth := ansi.StringWidth(strings.Join(leading, ""))
	actionWidth := ansi.StringWidth(strings.Join(actions, ""))
	y, right := g.headerY, g.headerX+g.headerWidth
	if modeWidth+2+actionWidth > right-left {
		y, left = g.headerY+1, g.headerX
	}
	if modeWidth+2+actionWidth > right-left {
		actions = renderActions(true)
		actionWidth = ansi.StringWidth(strings.Join(actions, ""))
	}
	var hits []hitRegion
	x := left
	for i, action := range h.leading {
		w := ansi.StringWidth(leading[i])
		if action.enabled && !o.Busy {
			hits = append(hits, hitRegion{ID: action.id, X: x, Y: y, W: w, H: 1})
		}
		x += w
	}
	gap := max(2, right-x-actionWidth)
	x += gap
	for i, action := range h.actions {
		w := ansi.StringWidth(actions[i])
		if action.enabled && !o.Busy {
			hits = append(hits, hitRegion{ID: action.id, X: x, Y: y, W: w, H: 1})
		}
		x += w
	}
	return strings.Join(leading, "") + themed(strings.Repeat(" ", gap), text, bg, o.Dim) + strings.Join(actions, ""), left, y, hits
}
