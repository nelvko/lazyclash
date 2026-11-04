package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func (m Model) proxyButton(id, label string, surface string, enabled bool) string {
	o := dashboardButtonOptions(id, m.navigationOptions(), false, surface)
	o.Kind, o.Disabled = buttonText, !enabled || m.busy || m.quitting
	return renderButton(clean(label), o)
}
func (m Model) proxyModes(width int) dashboardBlock {
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
	return b
}

func (m Model) proxySearchWidth(width int) int {
	modes := m.proxyModes(width)
	if !m.proxiesNarrow() && len(modes.lines) == 1 && width-ansi.StringWidth(modes.lines[0])-2 >= 24 {
		width -= ansi.StringWidth(modes.lines[0]) + 2
	}
	return max(1, width-ansi.StringWidth(m.proxies.search.Prompt)-1)
}

func (m Model) proxiesToolbar(width int) dashboardBlock {
	b := m.proxyModes(width)
	input := m.proxies.search
	input.Placeholder = m.tr("proxies.search_placeholder")
	input.SetWidth(m.proxySearchWidth(width))
	styleInput(&input, inputBackground, false)
	x := 0
	if !m.proxiesNarrow() && len(b.lines) == 1 && width-ansi.StringWidth(b.lines[0])-2 >= 24 {
		x = ansi.StringWidth(b.lines[0]) + 2
		b.lines[0] += "  "
	} else {
		b.lines = append(b.lines, "", "")
	}
	b.lines[len(b.lines)-1] += themed(cellPad(input.View(), width-x), text, inputBackground, false)
	b.targets = append(b.targets, hitRegion{ID: "proxies.search", X: x, Y: len(b.lines) - 1, W: width - x, H: 1})
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
	for i, gi := range groups {
		if i > 0 {
			b.blank()
		}
		g := m.snapshot.Groups[gi]
		b.append(m.proxyGroupHeader(gi, width))
		if m.proxies.expanded == g.Name {
			b.blank()
			b.append(m.proxyGroupBody(gi, width))
		}
		if g.Name == m.proxies.testGroup && m.proxies.total > 0 && (m.proxies.testing || m.proxies.testStatusVisible) {
			status := m.tr("proxies.test."+m.proxies.testStatus, m.proxies.completed, m.proxies.total)
			if m.proxies.testing {
				status = m.spinner.View() + " " + status
			}
			add(status, muted)
		}
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
	summary := m.proxySummary(g.Now)
	if g.Type == "LoadBalance" || g.Type == "Relay" {
		summary = m.tr("proxies.distributed")
	}
	badgeWidth := 3
	for _, group := range m.snapshot.Groups {
		badgeWidth = max(badgeWidth, len(strconv.Itoa(len(group.All)))+2)
	}
	badgeWidth = min(width, badgeWidth)
	badge := themed(cellPad(" "+strconv.Itoa(len(g.All)), badgeWidth), muted, panel, false)
	expanded := m.proxies.expanded == g.Name
	type headerAction struct{ id, value string }
	var actions []headerAction
	_, supported := m.backend.(proxyDelayBackend)
	if expanded {
		if m.proxies.sort != "default" {
			actions = append(actions, headerAction{"proxies.sort", m.proxyButton("proxies.sort", m.tr("proxies.sort."+m.proxies.sort)+" ↑", surface, true)})
		}
		label := m.tr("proxies.test_group")
		if m.proxies.testing {
			label = m.tr("proxies.cancel_test")
		}
		actions = append(actions, headerAction{"proxies.test", m.proxyButton("proxies.test", label, surface, supported)})
	}
	var actionValues []string
	for _, action := range actions {
		actionValues = append(actionValues, action.value)
	}
	actionText := strings.Join(actionValues, " ")
	actionWidth := ansi.StringWidth(actionText)
	inlineActions := expanded && width >= max(52, actionWidth+badgeWidth+24)
	right := " " + badge
	if inlineActions {
		right = " " + actionText + right
	}
	if g.Type != "Selector" && g.Type != "LoadBalance" && g.Type != "Relay" {
		right = themed(" "+m.tr("proxies.auto"), muted, surface, false) + right
	}
	leftWidth := max(1, width-ansi.StringWidth(right))
	name := themed(lipgloss.NewStyle().Bold(true).Render(clean(g.Name)), color, surface, false)
	prefix := themed(marker, focus, surface, false) + themed(arrow, muted, surface, false)
	b := dashboardBlock{}
	if m.proxiesNarrow() || width < 72 {
		b.lines = []string{cellPad(prefix+name, leftWidth) + right, themed(cellPad("    "+clean(summary), width), muted, surface, false)}
	} else {
		nameWidth := min(24, max(12, width/4))
		left := prefix + cellPad(name, nameWidth) + themed("  ", text, surface, false)
		room := max(0, leftWidth-ansi.StringWidth(left))
		b.lines = []string{cellPad(left+themed(cellCrop(clean(summary), room), muted, surface, false), leftWidth) + right}
	}
	groupWidth := width
	if inlineActions {
		x := width - badgeWidth - 1 - actionWidth
		groupWidth = x
		for _, action := range actions {
			w := ansi.StringWidth(action.value)
			if !m.busy && !m.quitting && (action.id != "proxies.test" || supported) {
				b.targets = append(b.targets, hitRegion{ID: action.id, X: x, W: w, H: 1})
			}
			x += w + 1
		}
	}
	headerHeight := len(b.lines)
	if inlineActions {
		headerHeight = 1
	}
	b.targets = append(b.targets, hitRegion{ID: id, W: groupWidth, H: headerHeight})
	if inlineActions && len(b.lines) == 2 {
		b.targets = append(b.targets, hitRegion{ID: id, Y: 1, W: width, H: 1})
	}
	if expanded && !inlineActions {
		b.lines = append(b.lines, "")
		x := 0
		for _, action := range actions {
			w := min(width, ansi.StringWidth(action.value))
			if x > 0 && x+1+w > width {
				b.lines = append(b.lines, "")
				x = 0
			}
			if x > 0 {
				b.lines[len(b.lines)-1] += " "
				x++
			}
			b.lines[len(b.lines)-1] += cellCrop(action.value, w)
			if !m.busy && !m.quitting && (action.id != "proxies.test" || supported) {
				b.targets = append(b.targets, hitRegion{ID: action.id, X: x, Y: len(b.lines) - 1, W: w, H: 1})
			}
			x += w
		}
	}
	return b
}

func (m Model) proxyColumns(width int) int {
	if m.proxiesNarrow() {
		return 1
	}
	return min(3, max(1, (width-2+1)/37))
}
func (m Model) proxyGroupBody(gi, width int) dashboardBlock {
	g := m.snapshot.Groups[gi]
	b := dashboardBlock{}
	indent := 2
	if m.proxiesNarrow() {
		indent = 0
	}
	inner := max(1, width-indent)
	add := func(v string, color string) {
		for _, line := range strings.Split(wrapText(themed(clean(v), color, bg, false), inner), "\n") {
			b.lines = append(b.lines, themed(strings.Repeat(" ", indent), text, bg, false)+line)
		}
	}
	indices := m.proxyMembers(g)
	columns := m.proxyColumns(width)
	cellWidth := (inner - (columns - 1)) / columns
	for first := 0; first < len(indices); first += columns {
		if first > 0 {
			b.blank()
		}
		var cells []dashboardBlock
		for c := 0; c < columns && first+c < len(indices); c++ {
			cells = append(cells, m.proxyNode(gi, indices[first+c], cellWidth))
		}
		rowY := len(b.lines)
		for y := range cells[0].lines {
			line := themed(strings.Repeat(" ", indent), text, bg, false)
			for c, cell := range cells {
				if c > 0 {
					line += themed(" ", text, bg, false)
				}
				line += cell.lines[y]
			}
			b.lines = append(b.lines, cellCrop(line, width))
		}
		for c, cell := range cells {
			for _, hit := range cell.targets {
				hit.X += indent + c*(cellWidth+1)
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
	referenceID := fmt.Sprintf("proxies.reference.%d.%d", gi, ni)
	selected := g.Now == node && g.Type != "LoadBalance" && g.Type != "Relay"
	surface := panel
	if selected {
		surface = selectionBackground
	} else if m.hover == id || m.hover == referenceID {
		surface = hoverBackground
	}
	prefix := "  "
	if m.focus == id {
		prefix = "› "
	}
	rail := " "
	if selected {
		rail = "▌"
	}
	edge := themed(rail+" ", accent, surface, false)
	value, color := m.proxyDelayLabel(g.Name, node)
	if !m.proxyTestable(node) {
		value = "—"
	}
	if m.busy && m.proxies.pendingGroup == g.Name && m.proxies.pendingNode == node {
		value, color = m.tr("proxies.switching"), accent
	}
	rightPadding := min(2, max(0, width-4))
	contentWidth := max(0, width-4-rightPadding)
	valueWidth := min(ansi.StringWidth(value), contentWidth/2)
	nameWidth := max(0, contentWidth-valueWidth-1)
	trailing := themed(strings.Repeat(" ", rightPadding), text, surface, false)
	line := edge + themed(prefix, focus, surface, false) + themed(cellPad(clean(node), nameWidth), text, surface, false) + themed(" "+cellCrop(value, valueWidth), color, surface, false) + trailing
	metaWidth := contentWidth
	button, buttonWidth := "", 0
	_, _, reference := m.proxyGroup(node)
	if reference {
		button = m.proxyButton(referenceID, m.tr("proxies.enter"), surface, true)
		buttonWidth = min(metaWidth, ansi.StringWidth(button))
		metaWidth = max(0, metaWidth-buttonWidth-1)
	}
	meta := edge + themed("  "+cellPad(clean(m.proxyNodeMeta(node)), metaWidth), muted, surface, false)
	padding := edge + themed(strings.Repeat(" ", max(0, width-2)), text, surface, false)
	b := dashboardBlock{lines: []string{cellCrop(padding, width), themed(cellPad(line, width), text, surface, false)}, targets: []hitRegion{{ID: id, W: width, H: 4}}}
	if reference && buttonWidth > 0 {
		buttonX := width - rightPadding - buttonWidth
		meta = cellPad(meta, buttonX) + cellCrop(button, buttonWidth) + trailing
		b.targets[0].W = buttonX
		b.targets = append(b.targets,
			hitRegion{ID: id, X: buttonX, W: width - buttonX, H: 2},
			hitRegion{ID: id, X: buttonX, Y: 3, W: width - buttonX, H: 1},
		)
		if rightPadding > 0 {
			b.targets = append(b.targets, hitRegion{ID: id, X: width - rightPadding, Y: 2, W: rightPadding, H: 1})
		}
		if !m.busy && !m.quitting {
			b.targets = append(b.targets, hitRegion{ID: referenceID, X: buttonX, Y: 2, W: buttonWidth, H: 1})
		}
	} else {
		meta += trailing
	}
	b.lines = append(b.lines, themed(cellPad(meta, width), text, surface, false), cellCrop(padding, width))
	return b
}

// Count unique members in visual order; a partially clipped card is visible.
// Split hit regions keep each card's inline action independent from selection.
func proxyVisibleRange(locale i18n.Localizer, hits []hitRegion, scroll, height int) string {
	seen := make(map[string]int)
	first, last, total := 0, 0, 0
	for _, hit := range hits {
		if !strings.HasPrefix(hit.ID, "node.") {
			continue
		}
		ordinal, exists := seen[hit.ID]
		if !exists {
			total++
			ordinal = total
			seen[hit.ID] = ordinal
		}
		if hit.Y < scroll+height && hit.Y+hit.H > scroll {
			if first == 0 || ordinal < first {
				first = ordinal
			}
			last = max(last, ordinal)
		}
	}
	if first == 0 {
		return ""
	}
	return locale.T("proxies.visible_range", first, last, total)
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
	if g.Type == "LoadBalance" || g.Type == "Relay" {
		add(m.tr("proxies.distributed_hint"), muted)
	} else if g.Type != "Selector" {
		add(m.tr("proxies.automatic"), muted)
	}
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
