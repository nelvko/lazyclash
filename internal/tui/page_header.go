package tui

import (
	"fmt"
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
	h := pageHeader{color: muted}
	switch m.page {
	case "profiles":
		canUpdate := false
		for _, profile := range m.snapshot.ProfileDetails {
			canUpdate = canUpdate || profile.Refreshable
		}
		h.actions = []headerAction{{"profiles.update-all", "Update all", "↻", canUpdate}}
	case "proxies":
		if m.snapshot.Installed && m.snapshot.Service.Active && m.snapshot.ServiceError == "" && m.snapshot.APIError == "" {
			h.summary = fmt.Sprintf("%d groups", len(m.snapshot.Groups))
		}
		h.actions = []headerAction{{"page.refresh", "Refresh", "↻", true}}
	case "connections":
		if m.runtime.received && m.runtime.snapshot.State == "ready" && m.runtime.snapshot.Connections != nil {
			h.summary = fmt.Sprintf("%d active", len(m.runtime.snapshot.Connections.Connections))
		}
		h.actions = []headerAction{{"conn.retry", "Refresh", "↻", !m.connections.confirming}}
	case "rules":
		if m.rules.received && m.rules.snapshot.State == "ready" {
			h.summary = fmt.Sprintf("%d rules", len(m.rules.snapshot.Rules))
		}
		h.actions = []headerAction{{"rules.retry", "Refresh", "↻", true}}
	case "logs":
		h.summary = "Connecting"
		follow, compact := "Resume", "▶"
		if m.logs.active {
			h.summary, h.color = "Live", info
			follow, compact = "Pause", "Ⅱ"
		} else if m.logs.paused {
			h.summary = "Paused"
		} else if m.logs.ended {
			h.summary = "Ended"
		}
		h.actions = []headerAction{{"logs.follow", follow, compact, true}, {"logs.retry", "Reconnect", "↻", true}}
	case "settings":
		h.actions = []headerAction{{"network", "Network settings", "Network", true}}
	}
	return h
}

// On small terminals actions get their own header row. They remain outside the
// scrolling body, with compact labels when the full labels do not fit.
func renderPageHeader(o dashboardOptions, g dashboardLayout, titleX, titleWidth int) (string, int, int, []hitRegion) {
	h := o.Header
	if o.Page == "overview" {
		status, color := coreSummary(o.Snapshot)
		h = pageHeader{summary: "Mihomo · " + status, color: color}
	}
	y, left := g.headerY, titleX+titleWidth+2
	if o.Width < 70 && o.Page != "overview" {
		y, left = g.headerY+1, g.x
	}
	available := max(0, g.x+g.width-left)
	buttons := func(compact bool) ([]string, int) {
		var values []string
		total := 0
		for _, action := range h.actions {
			label := action.label
			if compact {
				label = action.compact
			}
			value := dashboardButton(action.id, label, o, false)
			if !action.enabled || o.Busy {
				value = themed("[ "+label+" ]", disabled, bg, o.Dim)
			}
			values = append(values, value)
			total += ansi.StringWidth(value)
		}
		return values, total + max(0, len(values)-1)*2
	}
	values, width := buttons(false)
	if width > available {
		values, width = buttons(true)
	}
	var hits []hitRegion
	x := max(left, g.x+g.width-width)
	for i, action := range h.actions {
		w := ansi.StringWidth(values[i])
		if action.enabled && !o.Busy {
			hits = append(hits, hitRegion{ID: action.id, X: x, Y: y, W: w, H: 1})
		}
		x += w + 2
	}
	value := strings.Join(values, themed("  ", text, bg, o.Dim))
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
