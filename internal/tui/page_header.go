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
	summary, color string
	actions        []headerAction
}

func (h pageHeader) owns(id string) bool {
	for _, action := range h.actions {
		if action.id == id {
			return true
		}
	}
	return false
}

func (m Model) pageHeader() pageHeader {
	tr := m.tr
	h := pageHeader{color: muted}
	switch m.page {
	case "profiles":
		canUpdate := false
		for _, profile := range m.snapshot.ProfileDetails {
			canUpdate = canUpdate || profile.Refreshable
		}
		h.actions = []headerAction{{"profiles.update-all", tr("header.updateAll"), "↻", canUpdate}}
	case "proxies":
		if m.snapshot.Installed && m.snapshot.Service.Active && m.snapshot.ServiceError == "" && m.snapshot.APIError == "" {
			h.summary = tr("header.groups", len(m.snapshot.Groups))
		}
		h.actions = []headerAction{{"page.refresh", tr("header.refresh"), "↻", true}}
	case "connections":
		if m.runtime.received && m.runtime.snapshot.State == "ready" && m.runtime.snapshot.Connections != nil {
			h.summary = tr("header.active", len(m.runtime.snapshot.Connections.Connections))
		}
		h.actions = []headerAction{{"conn.retry", tr("header.refresh"), "↻", !m.connections.confirming}}
	case "rules":
		if m.rules.received && m.rules.snapshot.State == "ready" {
			h.summary = tr("header.rules", len(m.rules.snapshot.Rules))
		}
		h.actions = []headerAction{{"rules.retry", tr("header.refresh"), "↻", true}}
	case "logs":
		h.summary = tr("header.connecting")
		follow, compact := tr("header.resume"), "▶"
		if m.logs.active {
			h.summary, h.color = tr("header.live"), info
			follow, compact = tr("header.pause"), "Ⅱ"
		} else if m.logs.paused {
			h.summary = tr("header.paused")
		} else if m.logs.ended {
			h.summary = tr("header.ended")
		}
		h.actions = []headerAction{{"logs.follow", follow, compact, true}, {"logs.retry", tr("header.reconnect"), "↻", true}}
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
		status, color := coreSummary(o.Snapshot, o.Locale)
		h = pageHeader{summary: "Mihomo · " + status, color: color}
	}
	y, left := g.headerY, titleX+titleWidth+2
	if o.Width < 70 && o.Page != "overview" {
		y, left = g.headerY+1, g.x
	}
	available := max(0, g.x+g.width-left)
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
			value := dashboardButton(action.id, label, o, false)
			if !action.enabled || o.Busy {
				value = themed("[ "+label+" ]", disabled, bg, o.Dim)
			}
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
	x := max(left, g.x+g.width-width)
	for i, action := range h.actions {
		w := ansi.StringWidth(values[i])
		if action.enabled && !o.Busy {
			hits = append(hits, hitRegion{ID: action.id, X: x, Y: y, W: w, H: 1})
		}
		x += w + gap
	}
	value := strings.Join(values, themed(strings.Repeat(" ", gap), text, bg, o.Dim))
	if h.summary != "" {
		summaryWidth := available - width
		if width > 0 {
			summaryWidth -= 2
		}
		if summaryWidth >= ansi.StringWidth(h.summary) {
			prefix := themed(h.summary, h.color, bg, o.Dim)
			if width > 0 {
				prefix += themed("  ", text, bg, o.Dim)
			}
			value = prefix + value
		}
	}
	return value, max(left, g.x+g.width-ansi.StringWidth(value)), y, hits
}
