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
	summary        string
	compactSummary string
	leading        []headerAction
	actions        []headerAction
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
		h.actions = []headerAction{
			{"proxies.search-open", tr("proxies.search"), "/", m.proxiesReady() && !m.proxiesDirect() && m.proxies.detail == ""},
			{"proxies.providers", tr("providers.title.proxies"), "≡", m.providersSupported(providerProxies)},
		}
	case "profiles":
		h.actions = []headerAction{{"profiles.update-all", tr("header.updateAll"), "↻", m.hasRefreshableProfiles()}}
	case "connections":
		h.summary, h.compactSummary = m.connectionTotals(false), m.connectionTotals(true)
		h.actions = []headerAction{{"conn.close-all", tr("connections.close_all"), "×", !m.connections.confirming && m.canCloseAllConnections()}}
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
		if state, status := m.corePageState(); state != "" {
			h.summary = status
			h.actions = []headerAction{{"logs.clear", tr("logs.clear"), "×", true}}
			if id, label := m.corePageAction(); id != "" {
				h.actions = append(h.actions, headerAction{id, label, "", true})
			}
		}
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
			if action.id == "conn.close-all" {
				options.Kind = buttonDanger
			}
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
	if h.compactSummary != "" && ansi.StringWidth(h.summary)+width+2 > available {
		h.summary = h.compactSummary
		if ansi.StringWidth(h.summary)+width+2 > available {
			values, width = buttons(true)
		}
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

// Align the mode choices and provider action at the right, with a fixed gap
// between the two groups. Smaller terminals use a dedicated header row.
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
	gap := 4
	if g.headerHeight == g.headerY+1 && modeWidth+gap+actionWidth > right-left {
		// A short header has no spare action row. Compact the labels before
		// wrapping, so the controls cannot land on its separator or body.
		actions = renderActions(true)
		actionWidth = ansi.StringWidth(strings.Join(actions, ""))
	}
	if modeWidth+gap+actionWidth > right-left {
		y, left = g.headerY+1, g.headerX
	}
	if modeWidth+gap+actionWidth > right-left {
		actions = renderActions(true)
		actionWidth = ansi.StringWidth(strings.Join(actions, ""))
	}
	left = max(left, right-modeWidth-gap-actionWidth)
	var hits []hitRegion
	x := left
	for i, action := range h.leading {
		w := ansi.StringWidth(leading[i])
		if action.enabled && !o.Busy {
			hits = append(hits, hitRegion{ID: action.id, X: x, Y: y, W: w, H: 1})
		}
		x += w
	}
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
