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
	selectionWidth := 6
	for _, mode := range []string{"rule", "global", "direct"} {
		selectionWidth = max(selectionWidth, ansi.StringWidth(m.tr("dashboard.mode."+mode)))
	}
	for _, mode := range []string{"rule", "global", "direct"} {
		o := dashboardButtonOptions("mode."+mode, m.navigationOptions(), false, bg)
		o.Selectable, o.Selected = true, m.snapshot.Mode.Desired == mode
		o.Width = selectionWidth
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
	actionWidth := ansi.StringWidth(m.proxyCollapseButton())
	if actionWidth > 0 {
		actionWidth += 2
	}
	if !m.proxiesNarrow() && len(modes.lines) == 1 && width-ansi.StringWidth(modes.lines[0])-2 >= 24+actionWidth {
		width -= ansi.StringWidth(modes.lines[0]) + 2
	}
	return searchInputWidth(width - actionWidth)
}

func (m Model) proxyCollapseButton() string {
	if m.proxiesNarrow() || !m.hasExpandedProxyGroups() {
		return ""
	}
	return m.proxyButton("proxies.collapse_all", m.tr("proxies.collapse_all"), bg, true)
}

func (m Model) proxiesToolbar(width int) dashboardBlock {
	b := m.proxyModes(width)
	action := m.proxyCollapseButton()
	actionWidth := ansi.StringWidth(action)
	actionSpace := 0
	if actionWidth > 0 {
		actionSpace = actionWidth + 2
	}
	x := 0
	inline := !m.proxiesNarrow() && len(b.lines) == 1 && width-ansi.StringWidth(b.lines[0])-2 >= 24+actionSpace
	if inline {
		x = ansi.StringWidth(b.lines[0]) + 2
		b.lines = []string{"", b.lines[0], ""}
		for i := range b.targets {
			b.targets[i].Y++
		}
	} else {
		b.blank()
	}
	field := m.searchField("proxies.search", m.tr("proxies.search_placeholder"), m.proxies.search,
		max(1, width-x-actionSpace), false)
	y := len(b.lines)
	if inline {
		y = 0
		for row, line := range field.lines {
			b.lines[row] = themed(cellPad(b.lines[row], x), text, bg, false) + line
		}
		for _, hit := range field.targets {
			hit.X += x
			b.targets = append(b.targets, hit)
		}
	} else {
		b.append(field)
	}
	if actionWidth > 0 {
		b.lines[y+1] += themed("  ", text, bg, false) + action
		if !m.busy && !m.quitting {
			b.targets = append(b.targets, hitRegion{ID: "proxies.collapse_all", X: width - actionWidth, Y: y + 1, W: actionWidth, H: 1})
		}
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
	if m.snapshot.Mode.Actual == "" || m.snapshot.Mode.Error != "" {
		add(m.tr("dashboard.settings.actualMode", m.tr("dashboard.status.unknown")), muted)
	} else if m.snapshot.Mode.Actual != m.snapshot.Mode.Desired {
		add(m.tr("dashboard.settings.actualMode", dashboardModeLabel(m.snapshot.Mode.Actual, m.locale)), muted)
	}
	if m.snapshot.Mode.Actual == "direct" {
		add(m.tr("proxies.direct_mode"), muted)
		b.blank()
	}
	if len(m.proxies.history) > 0 {
		button("proxies.back", m.tr("proxies.back"))
	} else if m.proxiesNarrow() && m.proxies.activeGroup != "" {
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
	compactGroups := m.proxiesNarrow() || m.height < 24
	for i, gi := range groups {
		if i > 0 && compactGroups {
			b.blank()
		}
		g := m.snapshot.Groups[gi]
		b.append(m.proxyGroupHeader(gi, width))
		if m.proxyGroupExpanded(g.Name) {
			if compactGroups {
				b.blank()
			}
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
	testID, sortID := fmt.Sprintf("proxies.test.%d", gi), fmt.Sprintf("proxies.sort.%d", gi)
	expanded := m.proxyGroupExpanded(g.Name)
	marker, arrow := "  ", "▸"
	if m.focus == id {
		marker = "› "
	}
	color := text
	if expanded {
		arrow, color = "▾", accent
	}
	surface := bg
	if m.hover == id || m.hover == testID || m.hover == sortID {
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
	count := strconv.Itoa(len(g.All))
	badge := themed(strings.Repeat(" ", max(0, badgeWidth-len(count)-1))+count+" ", muted, panel, false)
	type headerAction struct {
		id, value string
		enabled   bool
	}
	var actions []headerAction
	_, supported := m.backend.(proxyDelayBackend)
	if expanded {
		if order := m.proxySort(g.Name); order != "default" {
			actions = append(actions, headerAction{sortID, m.proxyButton(sortID, m.tr("proxies.sort."+order)+" ↑", surface, true), true})
		}
		label := m.tr("proxies.test_group")
		if m.proxies.testing && m.proxies.testGroup == g.Name {
			label = m.tr("proxies.cancel_test")
		}
		actions = append(actions, headerAction{testID, m.proxyButton(testID, label, surface, supported), supported})
	}
	var actionValues []string
	for _, action := range actions {
		actionValues = append(actionValues, action.value)
	}
	actionText := strings.Join(actionValues, "")
	actionWidth := ansi.StringWidth(actionText)
	right := " " + badge + themed(" "+arrow+" ", muted, surface, false)
	if g.Type != "Selector" && g.Type != "LoadBalance" && g.Type != "Relay" {
		right = themed(" "+m.tr("proxies.auto"), muted, surface, false) + right
	}
	suffixWidth := ansi.StringWidth(right)
	inlineActions := expanded && width >= max(52, actionWidth+suffixWidth+24)
	if inlineActions {
		right = " " + actionText + right
	}
	leftWidth := max(1, width-ansi.StringWidth(right))
	name := themed(lipgloss.NewStyle().Bold(true).Render(clean(g.Name)), color, surface, false)
	prefix := themed(marker, focus, surface, false)
	b := dashboardBlock{}
	if m.proxiesNarrow() || width < 72 {
		b.lines = []string{cellPad(prefix+name, leftWidth) + right, themed(cellPad("  "+clean(summary), width), muted, surface, false)}
	} else {
		nameWidth := min(24, max(12, width/4))
		left := prefix + cellPad(name, nameWidth) + "  "
		room := max(0, leftWidth-ansi.StringWidth(left))
		b.lines = []string{cellPad(left+themed(cellCrop(clean(summary), room), muted, surface, false), leftWidth) + right}
	}
	// Reserve action rectangles even when disabled so clicking them never folds
	// the group. The remaining header surface belongs to the folding target.
	var actionRegions []hitRegion
	appendAction := func(action headerAction, x, y, w int) {
		hit := hitRegion{ID: action.id, X: x, Y: y, W: w, H: 1}
		actionRegions = append(actionRegions, hit)
		if action.enabled && !m.busy && !m.quitting {
			b.targets = append(b.targets, hit)
		}
	}
	if inlineActions {
		x := width - suffixWidth - actionWidth
		for _, action := range actions {
			w := ansi.StringWidth(action.value)
			appendAction(action, x, 0, w)
			x += w
		}
	} else if len(actions) > 0 {
		b.lines = append(b.lines, "  ")
		x := 2
		for _, action := range actions {
			w := min(max(1, width-2), ansi.StringWidth(action.value))
			if x > 2 && x+w > width {
				b.lines = append(b.lines, "  ")
				x = 2
			}
			b.lines[len(b.lines)-1] += cellCrop(action.value, w)
			appendAction(action, x, len(b.lines)-1, w)
			x += w
		}
	}
	if !m.proxiesNarrow() && m.height >= 24 {
		b.lines = append(append([]string{""}, b.lines...), "")
		for i := range actionRegions {
			actionRegions[i].Y++
		}
		for i := range b.targets {
			b.targets[i].Y++
		}
	}
	for i, line := range b.lines {
		b.lines[i] = themed(cellPad(line, width), text, surface, false)
	}
	// The first, full-height region lets keyboard focus reveal the whole header.
	groupWidth := width
	for _, hit := range actionRegions {
		groupWidth = min(groupWidth, hit.X)
	}
	b.targets = append(b.targets, hitRegion{ID: id, W: groupWidth, H: len(b.lines)})
	if groupWidth < width {
		for y := range b.lines {
			x := groupWidth
			for _, hit := range actionRegions {
				if hit.Y != y {
					continue
				}
				if hit.X > x {
					b.targets = append(b.targets, hitRegion{ID: id, X: x, Y: y, W: hit.X - x, H: 1})
				}
				x = hit.X + hit.W
			}
			if x < width {
				b.targets = append(b.targets, hitRegion{ID: id, X: x, Y: y, W: width - x, H: 1})
			}
		}
	}
	return b
}

func (m Model) proxyGroupBody(gi, width int) dashboardBlock {
	g := m.snapshot.Groups[gi]
	b := dashboardBlock{}
	grid := m.proxyGrid(width)
	inner := max(1, width-grid.indent)
	add := func(v string, color string) {
		for _, line := range strings.Split(wrapText(themed(clean(v), color, bg, false), inner), "\n") {
			b.lines = append(b.lines, themed(strings.Repeat(" ", grid.indent), text, bg, false)+line)
		}
	}
	indices := m.proxyMembers(g)
	for first := 0; first < len(indices); first += grid.columns {
		if first > 0 {
			b.blank()
		}
		var cells []dashboardBlock
		for c := 0; c < grid.columns && first+c < len(indices); c++ {
			cells = append(cells, m.proxyNode(gi, indices[first+c], grid.cardWidth))
		}
		rowY := len(b.lines)
		for y := range cells[0].lines {
			line := themed(strings.Repeat(" ", grid.indent), text, bg, false)
			for c, cell := range cells {
				if c > 0 {
					line += themed(strings.Repeat(" ", proxyColumnGap), text, bg, false)
				}
				line += cell.lines[y]
			}
			b.lines = append(b.lines, cellCrop(line, width))
		}
		for c, cell := range cells {
			for _, hit := range cell.targets {
				hit.X += grid.indent + c*(grid.cardWidth+proxyColumnGap)
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
	if state := m.proxyNodeTestState(group, node); state != "" {
		return m.tr("proxies." + state), accent
	}
	if m.proxies.testing {
		key, path := proxyResultKey(group, node), m.proxyPathKey(node)
		if m.proxies.inflight[key] && m.proxies.inflightPaths[key] == path {
			return m.tr("proxies.testing"), accent
		}
		for _, target := range m.proxies.queue {
			if target.group == group && target.node == node && target.path == path {
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
	delayID := fmt.Sprintf("proxies.delay.%d.%d", gi, ni)
	selected := g.Now == node && g.Type != "LoadBalance" && g.Type != "Relay"
	surface := panel
	if selected {
		surface = selectionBackground
	} else if m.hover == id || m.hover == referenceID || m.hover == delayID {
		surface = hoverBackground
	}
	prefix := "  "
	if m.focus == id || m.focus == delayID {
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
	valueWidth := min(12, contentWidth/2)
	nameWidth := max(0, contentWidth-valueWidth-1)
	_, delaySupported := m.backend.(proxyDelayBackend)
	delayEnabled := delaySupported && m.proxyTestable(node) && m.proxiesReady() && !m.busy && !m.quitting
	delaySurface := surface
	delayPending := value == m.tr("proxies.testing") || value == m.tr("proxies.queued")
	if delayEnabled && !delayPending && (m.hover == delayID || m.focus == delayID) {
		delaySurface = inputBackground
	}
	value = cellCrop(value, valueWidth)
	leftPadding := max(0, (valueWidth-ansi.StringWidth(value))/2)
	value = cellPad(strings.Repeat(" ", leftPadding)+value, valueWidth)
	trailing := themed(strings.Repeat(" ", rightPadding), text, surface, false)
	line := edge + themed(prefix, focus, surface, false) + themed(cellPad(clean(node), nameWidth), text, surface, false) + themed(" ", color, surface, false) + themed(value, color, delaySurface, false) + trailing
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
	b := dashboardBlock{lines: []string{cellCrop(padding, width), themed(cellPad(line, width), text, surface, false)}}
	var actions []hitRegion
	if valueWidth > 0 {
		actions = append(actions, hitRegion{ID: delayID, X: width - rightPadding - valueWidth, Y: 1, W: valueWidth, H: 1})
	}
	if reference && buttonWidth > 0 {
		buttonX := width - rightPadding - buttonWidth
		meta = cellPad(meta, buttonX) + cellCrop(button, buttonWidth) + trailing
		actions = append(actions, hitRegion{ID: referenceID, X: buttonX, Y: 2, W: buttonWidth, H: 1})
	} else {
		meta += trailing
	}
	b.lines = append(b.lines, themed(cellPad(meta, width), text, surface, false))
	// Keep one full-height left region for navigation, then fill each row around
	// its independent actions. Disabled delay slots remain inert.
	leftWidth := width
	for _, action := range actions {
		leftWidth = min(leftWidth, action.X)
	}
	b.targets = append(b.targets, hitRegion{ID: id, W: leftWidth, H: len(b.lines)})
	for y := range b.lines {
		x := leftWidth
		for _, action := range actions {
			if action.Y != y {
				continue
			}
			if action.X > x {
				b.targets = append(b.targets, hitRegion{ID: id, X: x, Y: y, W: action.X - x, H: 1})
			}
			if action.ID == delayID && delayEnabled || action.ID == referenceID && !m.busy && !m.quitting {
				b.targets = append(b.targets, action)
			}
			x = action.X + action.W
		}
		if x < width {
			b.targets = append(b.targets, hitRegion{ID: id, X: x, Y: y, W: width - x, H: 1})
		}
	}
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
