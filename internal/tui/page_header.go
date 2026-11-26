package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

type headerAction struct {
	id, label, compact string
	enabled            bool
}

type pageHeader struct {
	summary        string
	compactSummary string
	titleAction    headerAction
	leading        []headerAction
	actions        []headerAction
}

func (h pageHeader) owns(id string) bool {
	if id == "" {
		return false
	}
	if h.titleAction.id == id {
		return true
	}
	for _, action := range append(h.leading, h.actions...) {
		if action.id == id {
			return true
		}
	}
	return false
}

func (h pageHeader) enabled(id string) bool {
	if id == "" {
		return false
	}
	if h.titleAction.id == id {
		return h.titleAction.enabled
	}
	for _, action := range append(h.leading, h.actions...) {
		if action.id == id {
			return action.enabled
		}
	}
	return false
}

func (m *Model) hasRefreshableProfiles() bool {
	for _, profile := range m.snapshot.ProfileDetails {
		if profile.Refreshable {
			return true
		}
	}
	return false
}

func (m *Model) pageHeader() pageHeader {
	tr := m.tr
	h := pageHeader{}
	switch m.page {
	case "proxies":
		if m.width >= 50 {
			h.leading = m.proxyModeActions()
		}
		if m.proxiesReady() && !m.proxiesDirect() && !m.proxiesNarrow() && m.hasExpandedProxyGroups() {
			h.titleAction = headerAction{id: "proxies.collapse_all", label: tr("proxies.collapse_all"), enabled: !m.quitting}
		}
		h.actions = append(h.actions, headerAction{"proxies.providers", tr("providers.title.proxies"), "≡", m.providersSupported(providerProxies)})
	case "profiles":
		h.actions = []headerAction{{"profiles.update-all", tr("header.updateAll"), "↻", m.hasRefreshableProfiles()}}
	case "connections":
		h.summary, h.compactSummary = m.connectionTotals(false), m.connectionTotals(true)
		pause, compact := tr("connections.pause"), "Ⅱ"
		if m.connections.paused {
			pause, compact = tr("connections.resume"), "▶"
		}
		bulk := headerAction{"conn.close-all", tr("connections.close_all"), "×", !m.connections.confirming && m.canCloseAllConnections()}
		if m.connections.closedView {
			bulk = headerAction{"conn.clear-history", tr("connections.clear_history"), "⌫", m.canClearConnectionHistory()}
		}
		h.actions = []headerAction{{"conn.pause", pause, compact, m.canPauseConnections()}, bulk}
	case "rules":
		h.actions = []headerAction{{"rules.providers", tr("providers.title.rules"), "≡", m.providersSupported(providerRules)}}
	case "logs":
		follow, compact := tr("logs.pause_display"), "Ⅱ"
		if !m.logs.active && !m.logs.reconnecting {
			h.summary = tr("header.connecting")
		}
		if m.logs.paused {
			h.summary = tr("header.paused")
			follow, compact = tr("logs.resume_display"), "▶"
		}
		h.actions = []headerAction{{"logs.clear", tr("logs.clear"), "×", true}, {"logs.follow", follow, compact, true}}
		if !m.snapshotReady {
			h.summary, _ = m.logsStatus()
			h.actions = nil
		} else if state, status := m.corePageState(); state != "" {
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

func renderPageTitleAction(o dashboardOptions) string {
	action := o.Header.titleAction
	if action.id == "" {
		return ""
	}
	options := dashboardButtonOptions(action.id, o, false, bg)
	options.Kind, options.Compact, options.Disabled = buttonText, true, !action.enabled || o.Busy
	return renderButton(action.label, options)
}

func renderHeaderButton(o dashboardOptions, action headerAction, label string) string {
	options := dashboardButtonOptions(action.id, o, false, bg)
	if o.Page == "settings" {
		options.Hovered = false
	}
	options.Disabled = !action.enabled || o.Busy
	if action.id == "conn.close-all" {
		options.Kind = buttonDanger
	}
	return renderButton(label, options)
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
			value := renderHeaderButton(o, action, label)
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

func (m *Model) proxyModeActions() []headerAction {
	return proxyModeChoices(m.locale, m.proxiesReady() && m.snapshot.Mode.Error == "")
}

func proxyModeChoices(locale i18n.Localizer, enabled bool) []headerAction {
	var actions []headerAction
	for _, mode := range []string{"rule", "global", "direct"} {
		actions = append(actions, headerAction{id: "mode." + mode, label: locale.T("dashboard.mode." + mode), enabled: enabled})
	}
	return actions
}

func proxyModeSegmentWidth(actions []headerAction) int {
	labelWidth := 0
	for _, action := range actions {
		labelWidth = max(labelWidth, ansi.StringWidth(action.label))
	}
	return labelWidth + 2 // One cell for focus and one for the selection mark.
}

// The same three selectable faces wrap on the smallest screens.
func renderProxyModeSegments(o dashboardOptions, actions []headerAction, width int, kind buttonKind, selectedMode string) dashboardBlock {
	block := dashboardBlock{lines: []string{""}}
	segmentWidth := proxyModeSegmentWidth(actions)
	surface := bg
	if kind == buttonRoutingSegment {
		surface = panel
		// Give each card segment eight cells when possible. Reclaim extra
		// padding before wrapping a row that still fits all three labels.
		segmentWidth = max(segmentWidth, min(8, width/max(1, len(actions))))
	}
	w := min(width, segmentWidth)
	x := 0
	for _, action := range actions {
		options := dashboardButtonOptions(action.id, o, false, surface)
		options.Kind = kind
		options.Selectable, options.Selected = true, action.id == "mode."+selectedMode
		options.Width, options.Disabled = segmentWidth-2, !action.enabled || o.Busy
		if x > 0 && x+w > width {
			block.blank()
			x = 0
		}
		y := len(block.lines) - 1
		block.lines[y] += cellCrop(renderButton(action.label, options), w)
		if !options.Disabled {
			block.targets = append(block.targets, hitRegion{ID: action.id, X: x, Y: y, W: w, H: 1})
		}
		x += w
	}
	return block
}

// Align the mode choices and provider action at the right, with a fixed gap
// between the two groups. Smaller terminals use a dedicated header row.
func renderLeadingPageHeader(o dashboardOptions, g dashboardLayout, left int) (string, int, int, []hitRegion) {
	h := o.Header
	modes := renderProxyModeSegments(o, h.leading, g.headerWidth, buttonSegment, o.Snapshot.Mode.Desired)
	renderActions := func(compact bool) []string {
		var values []string
		for _, action := range h.actions {
			label := action.label
			if compact {
				label = action.compact
			}
			values = append(values, renderHeaderButton(o, action, label))
		}
		return values
	}
	actions := renderActions(false)
	modeWidth := ansi.StringWidth(modes.lines[0])
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
	hits := modes.targets
	for i := range hits {
		hits[i].X += left
		hits[i].Y += y
	}
	x := left + modeWidth + gap
	for i, action := range h.actions {
		w := ansi.StringWidth(actions[i])
		if action.enabled && !o.Busy {
			hits = append(hits, hitRegion{ID: action.id, X: x, Y: y, W: w, H: 1})
		}
		x += w
	}
	return modes.lines[0] + themed(strings.Repeat(" ", gap), text, bg, o.Dim) + strings.Join(actions, ""), left, y, hits
}
