package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func (m Model) proxyButton(id, label string, surface string, enabled bool) string {
	o := dashboardButtonOptions(id, m.navigationOptions(), false, surface)
	o.Kind, o.Disabled = buttonText, !enabled || m.busy || m.quitting
	return renderButton(clean(label), o)
}
func (m Model) proxiesToolbar(width int) dashboardBlock {
	b := dashboardBlock{lines: []string{""}}
	x := 0
	for _, mode := range []string{"rule", "global", "direct"} {
		o := dashboardButtonOptions("mode."+mode, m.navigationOptions(), false, bg)
		o.Selectable, o.Selected = true, m.snapshot.Mode.Actual == mode
		o.Disabled = m.busy || !m.proxiesReady() || m.snapshot.Mode.Error != ""
		v := renderButton(m.tr("dashboard.mode."+mode), o)
		w := min(width, ansi.StringWidth(v))
		if x > 0 && x+w > width {
			b.lines = append(b.lines, "")
			x = 0
		}
		b.lines[len(b.lines)-1] += cellCrop(v, w)
		if !o.Disabled {
			b.targets = append(b.targets, hitRegion{ID: "mode." + mode, X: x, Y: len(b.lines) - 1, W: w, H: 1})
		}
		x += w
	}
	input := m.proxies.search
	input.Placeholder = m.tr("proxies.search_placeholder")
	input.SetWidth(max(1, width-ansi.StringWidth(input.Prompt)-1))
	styleInput(&input, inputBackground, false)
	b.lines = append(b.lines, "", themed(cellPad(input.View(), width), text, inputBackground, false))
	b.targets = append(b.targets, hitRegion{ID: "proxies.search", Y: len(b.lines) - 1, W: width, H: 1})
	if !m.proxies.editing && m.proxies.search.Value() != "" {
		value := m.proxyButton("proxies.clear", m.tr("proxies.clear"), bg, true)
		b.targets = append(b.targets, hitRegion{ID: "proxies.clear", Y: len(b.lines), W: min(width, ansi.StringWidth(value)), H: 1})
		b.lines = append(b.lines, cellCrop(value, width))
	}
	return b
}
func (m Model) proxiesBody(width int) (string, []hitRegion) {
	width = max(1, width)
	b := m.proxiesToolbar(width)
	b.blank()
	add := func(value, color string) {
		b.lines = append(b.lines, strings.Split(wrapText(themed(clean(value), color, bg, false), width), "\n")...)
	}
	button := func(id, label string) {
		v := m.proxyButton(id, label, bg, true)
		b.targets = append(b.targets, hitRegion{ID: id, Y: len(b.lines), W: min(width, ansi.StringWidth(v)), H: 1})
		b.lines = append(b.lines, cellCrop(v, width))
	}
	if !m.proxiesReady() {
		switch {
		case !m.snapshotReady:
			add(m.tr("proxies.loading"), muted)
		case !m.snapshot.Installed:
			add(m.tr("proxies.uninstalled"), muted)
			button("card.install", m.tr("dashboard.action.install"))
		case m.snapshot.ServiceError != "":
			add(m.locale.Diagnostic(m.snapshot.ServiceError, m.snapshot.ServiceDiagnostic), errorColor)
		case !m.snapshot.Service.Active:
			add(m.tr("proxies.stopped"), muted)
			button("core.start", m.tr("dashboard.action.start"))
		default:
			add(m.tr("proxies.unavailable"), warning)
			add(m.locale.Diagnostic(m.snapshot.APIError, m.snapshot.APIDiagnostic), errorColor)
		}
		return strings.Join(b.lines, "\n"), b.targets
	}
	if m.proxies.detail != "" {
		b.append(m.proxyDetail(width))
		return strings.Join(b.lines, "\n"), b.targets
	}
	if m.snapshot.Mode.Error != "" {
		add(m.locale.Diagnostic(m.snapshot.Mode.Error, m.snapshot.Mode.ErrorDiagnostic), warning)
	}
	if m.snapshot.Mode.Actual == "direct" {
		add(m.tr("proxies.direct_mode"), muted)
		b.blank()
	}
	if len(m.proxies.history) > 0 {
		button("proxies.back", m.tr("proxies.back"))
	} else if m.proxiesNarrow() && m.proxies.expanded != "" {
		button("proxies.directory", m.tr("proxies.directory"))
	}
	groups := m.visibleProxyGroups()
	if len(groups) == 0 {
		if m.proxies.search.Value() != "" {
			add(m.tr("proxies.no_matches"), muted)
		} else {
			add(m.tr("proxies.no_groups"), muted)
		}
	}
	for _, gi := range groups {
		g := m.snapshot.Groups[gi]
		b.append(m.proxyGroupHeader(gi, width))
		if m.proxies.expanded == g.Name {
			b.append(m.proxyGroupBody(gi, width))
		}
		b.lines = append(b.lines, themed(strings.Repeat("─", width), divider, bg, false))
	}
	if m.proxies.testGroup != "" {
		b.blank()
		status := m.tr("proxies.test."+m.proxies.testStatus, clean(m.proxies.testGroup), m.proxies.completed, m.proxies.total)
		if m.proxies.testing {
			status = m.spinner.View() + " " + status
		}
		add(status, muted)
	}
	return strings.Join(b.lines, "\n"), b.targets
}
func (m Model) proxyGroupHeader(gi, width int) dashboardBlock {
	g := m.snapshot.Groups[gi]
	id := fmt.Sprintf("group.%d", gi)
	marker, arrow := "  ", "▸ "
	if m.focus == id {
		marker = "› "
	}
	if m.proxies.expanded == g.Name {
		arrow = "▾ "
	}
	color := text
	if m.proxies.expanded == g.Name {
		color = accent
	}
	surface := bg
	if m.hover == id {
		surface = hoverBackground
	}
	summary := strings.Join(m.proxyPath(g.Now), " → ")
	if g.Type == "LoadBalance" || g.Type == "Relay" {
		summary = m.tr("proxies.distributed")
	}
	if summary == "" {
		summary = m.tr("proxies.no_selection")
	}
	meta := g.Type + " · " + m.tr("proxies.members", len(g.All))
	name := themed(lipgloss.NewStyle().Bold(true).Render(clean(g.Name)), color, surface, false)
	prefix := themed(marker, focus, surface, false) + themed(arrow, muted, surface, false)
	if m.proxiesNarrow() || width < 72 {
		nameroom := max(1, width-4)
		if width >= 36 {
			nameroom = max(1, nameroom-ansi.StringWidth(meta)-2)
		}
		first := prefix + cellCrop(name, nameroom)
		if width >= 36 {
			first = cellPad(first, max(1, width-ansi.StringWidth(meta))) + themed(meta, muted, surface, false)
		}
		return dashboardBlock{lines: []string{cellPad(first, width), themed(cellPad("    "+clean(summary), width), muted, surface, false)}, targets: []hitRegion{{ID: id, W: width, H: 2}}}
	}
	nameWidth := min(24, max(12, width/4))
	left := prefix + cellPad(name, nameWidth) + themed("  ", text, surface, false)
	room := max(1, width-ansi.StringWidth(left)-ansi.StringWidth(meta)-2)
	line := left + themed(cellPad(clean(summary), room), muted, surface, false) + themed("  "+meta, muted, surface, false)
	return dashboardBlock{lines: []string{cellPad(line, width)}, targets: []hitRegion{{ID: id, W: width, H: 1}}}
}
func (m Model) proxyGroupBody(gi, width int) dashboardBlock {
	g := m.snapshot.Groups[gi]
	b := dashboardBlock{}
	indent := 4
	if m.proxiesNarrow() {
		indent = 0
	}
	inner := max(1, width-indent)
	add := func(v string, color string) {
		for _, line := range strings.Split(wrapText(themed(clean(v), color, bg, false), inner), "\n") {
			b.lines = append(b.lines, themed(strings.Repeat(" ", indent), text, bg, false)+line)
		}
	}
	if g.Type == "LoadBalance" || g.Type == "Relay" {
		add(m.tr("proxies.distributed_hint"), muted)
	} else {
		add(m.tr("proxies.current", strings.Join(m.proxyPath(g.Now), " → ")), muted)
	}
	if g.Type != "Selector" && g.Type != "LoadBalance" && g.Type != "Relay" {
		add(m.tr("proxies.automatic"), muted)
	}
	buttons := []struct {
		id, label string
		enabled   bool
	}{{"proxies.sort", m.tr("proxies.sort_label", m.tr("proxies.sort."+m.proxies.sort)), true}, {"proxies.test", m.tr("proxies.test_group"), true}}
	if m.proxies.testing {
		buttons[1].label = m.tr("proxies.cancel_test")
	}
	_, supported := m.backend.(proxyDelayBackend)
	buttons[1].enabled = supported
	if _, _, ref := m.proxyGroup(g.Now); ref {
		buttons = append(buttons, struct {
			id, label string
			enabled   bool
		}{"proxies.reference", m.tr("proxies.enter_group"), true})
	}
	x := indent
	for _, button := range buttons {
		v := m.proxyButton(button.id, button.label, bg, button.enabled)
		w := min(inner, ansi.StringWidth(v))
		if x == indent || x+w > width {
			b.lines = append(b.lines, themed(strings.Repeat(" ", indent), text, bg, false))
			x = indent
		}
		b.lines[len(b.lines)-1] += cellCrop(v, w)
		if button.enabled && !m.busy {
			b.targets = append(b.targets, hitRegion{ID: button.id, X: x, Y: len(b.lines) - 1, W: w, H: 1})
		}
		x += w
	}
	b.blank()
	indices := m.proxyMembers(g)
	columns := 1
	if !m.proxiesNarrow() && inner >= 70 {
		columns = 2
	}
	cellWidth := (inner - 2*(columns-1)) / columns
	for first := 0; first < len(indices); first += columns {
		var cells []dashboardBlock
		for c := 0; c < columns && first+c < len(indices); c++ {
			cells = append(cells, m.proxyNode(gi, indices[first+c], cellWidth))
		}
		rowY := len(b.lines)
		for y := 0; y < 2; y++ {
			line := themed(strings.Repeat(" ", indent), text, bg, false)
			for c, cell := range cells {
				if c > 0 {
					line += themed("  ", text, bg, false)
				}
				line += cell.lines[y]
			}
			b.lines = append(b.lines, cellCrop(line, width))
		}
		for c, cell := range cells {
			for _, hit := range cell.targets {
				hit.X += indent + c*(cellWidth+2)
				hit.Y += rowY
				b.targets = append(b.targets, hit)
			}
		}
	}
	if len(indices) == 0 {
		label := m.tr("proxies.empty_group")
		if len(g.All) > 0 {
			label = m.tr("proxies.no_matches")
		}
		add(label, muted)
	}
	return b
}
func (m Model) proxyDelayLabel(group, node string) (string, string) {
	if !m.proxyTestable(node) {
		return m.tr("proxies.not_applicable"), muted
	}
	if m.proxies.testing {
		key := proxyResultKey(group, node)
		if m.proxies.inflight[key] {
			return m.tr("proxies.testing"), accent
		}
		for _, target := range m.proxies.queue {
			if target.group == group && target.node == node {
				return m.tr("proxies.queued"), muted
			}
		}
	}
	r := m.proxyDelay(group, node).result
	switch r.State {
	case "success":
		color := success
		if r.Delay >= 200 {
			color = warning
		}
		return fmt.Sprintf("%d ms", r.Delay), color
	case "timeout":
		return m.tr("proxies.timeout"), errorColor
	case "failed":
		return m.tr("proxies.test_failed"), errorColor
	default:
		return m.tr("proxies.not_tested"), muted
	}
}
func (m Model) proxyNode(gi, ni, width int) dashboardBlock {
	g := m.snapshot.Groups[gi]
	node := g.All[ni]
	id := fmt.Sprintf("node.%d.%d", gi, ni)
	selected := g.Now == node && g.Type != "LoadBalance" && g.Type != "Relay"
	surface := panel
	if selected {
		surface = selectionBackground
	} else if m.hover == id {
		surface = hoverBackground
	}
	prefix := "  "
	if m.focus == id {
		prefix = "› "
	}
	check := "  "
	if selected {
		check = "✓ "
	}
	value, color := m.proxyDelayLabel(g.Name, node)
	if m.busy && m.proxies.pendingGroup == g.Name && m.proxies.pendingNode == node {
		value, color = m.tr("proxies.switching"), accent
	}
	valueWidth := min(ansi.StringWidth(value), max(0, width/2-2))
	nameWidth := max(1, width-4-valueWidth-1)
	line := themed(prefix, focus, surface, false) + themed(check, text, surface, false) + themed(cellPad(clean(node), nameWidth), text, surface, false) + themed(" "+cellCrop(value, valueWidth), color, surface, false)
	meta := themed(cellPad("    "+clean(m.proxyMeta(node)), width), muted, surface, false)
	return dashboardBlock{lines: []string{cellPad(line, width), meta}, targets: []hitRegion{{ID: id, W: width, H: 2}}}
}
func (m Model) proxyDetail(width int) dashboardBlock {
	b := dashboardBlock{}
	add := func(v string, color string) {
		b.lines = append(b.lines, strings.Split(wrapText(themed(clean(v), color, bg, false), width), "\n")...)
	}
	button := func(id, label string) {
		v := m.proxyButton(id, label, bg, true)
		b.targets = append(b.targets, hitRegion{ID: id, Y: len(b.lines), W: min(width, ansi.StringWidth(v)), H: 1})
		b.lines = append(b.lines, cellCrop(v, width))
	}
	button("proxies.back", m.tr("proxies.back"))
	name := m.proxies.detail
	add(name, accent)
	_, g, exists := m.proxyGroup(m.proxies.detailGroup)
	if !exists || indexOf(g.All, name) < 0 {
		add(m.tr("proxies.removed"), warning)
		return b
	}
	add(m.tr("proxies.type", m.proxyMeta(name)), text)
	add(m.tr("proxies.current", strings.Join(m.proxyPath(name), " → ")), muted)
	if _, _, ok := m.proxyGroup(name); ok {
		button("proxies.enter", m.tr("proxies.enter_group"))
	}
	if m.proxyTestable(name) {
		r := m.proxyDelay(g.Name, name)
		label, color := m.proxyDelayLabel(g.Name, name)
		add(m.tr("proxies.delay", label), color)
		if !r.result.Time.IsZero() {
			add(m.tr("proxies.test_time", r.result.Time.Local().Format(time.RFC3339)), muted)
		}
		target := r.result.URL
		if target == "" && r.result.State != "" {
			target = m.tr("proxies.core_target")
		} else if target == "" {
			target = mihomo.DefaultProxyDelayURL
		}
		add(m.tr("proxies.test_url", target), muted)
		if r.err != nil {
			add(m.localizeError(r.err), errorColor)
		}
		add(m.tr("proxies.test_hint"), muted)
	}
	return b
}
