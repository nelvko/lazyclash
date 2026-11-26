package tui

import (
	"fmt"
	"iter"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func (m *Model) proxyButton(id, label string, surface string, enabled bool) string {
	o := dashboardButtonOptions(id, m.navigationOptions(), false, surface)
	o.Kind, o.Disabled = buttonText, !enabled || m.busy || m.quitting
	return renderButton(clean(label), o)
}
func (m *Model) proxyModes(width int) dashboardBlock {
	return renderProxyModeSegments(m.navigationOptions(), m.proxyModeActions(), width, buttonSegment, m.snapshot.Mode.Desired)
}

func (m *Model) proxiesToolbar(width int) dashboardBlock {
	if m.width < 50 {
		return m.proxyModes(width)
	}
	return dashboardBlock{}
}
func (m *Model) proxiesBody(width int) (string, []hitRegion) {
	layout := m.layoutProxiesBody(width)
	body := m.paintProxiesBody(layout, 0, layout.total)
	return strings.Join(body.lines, "\n"), body.targets
}

func (m *Model) layoutProxiesBody(width int) proxyBodyLayout {
	width = max(1, width)
	layout := proxyBodyLayout{width: width, grid: m.proxyGrid(width)}
	var b dashboardBlock
	finish := func() proxyBodyLayout {
		layout.appendBlock(b)
		return layout
	}
	if m.proxySearchVisible() && !m.proxySearchFixed(m.dashboardOptions()) {
		b = m.proxySearchBlock(width)
	}
	add := func(value, color string) {
		b.lines = append(b.lines, strings.Split(wrapText(themed(clean(value), color, bg, false), width), "\n")...)
	}
	button := func(id, label string) {
		v := m.proxyButton(id, label, bg, true)
		b.targets = append(b.targets, hitRegion{ID: id, Y: len(b.lines), W: min(width, ansi.StringWidth(v)), H: 1})
		b.lines = append(b.lines, cellCrop(v, width))
	}
	finishEmpty := func(state emptyStateOptions) proxyBodyLayout {
		height := max(1, dashboardGeometry(m.dashboardOptions()).height-len(b.lines))
		b.append(m.emptyState(width, height, state))
		return finish()
	}
	if !m.proxiesReady() {
		if state, ok := m.coreEmptyState(); ok {
			return finishEmpty(state)
		}
		detail := m.locale.Diagnostic(m.snapshot.APIError, m.snapshot.APIDiagnostic)
		if !m.snapshot.Service.Active {
			detail = m.tr("dashboard.status.unknown")
		}
		return finishEmpty(emptyStateOptions{Title: m.tr("empty.proxies.failed"), Detail: detail, Error: true})
	}
	if m.snapshot.Mode.Error != "" {
		add(m.locale.Diagnostic(m.snapshot.Mode.Error, m.snapshot.Mode.ErrorDiagnostic), warning)
	}
	if m.snapshot.Mode.Actual == "" || m.snapshot.Mode.Error != "" {
		add(m.tr("dashboard.settings.actualMode", m.tr("dashboard.status.unknown")), muted)
	} else if m.snapshot.Mode.Actual != m.snapshot.Mode.Desired {
		add(m.tr("dashboard.settings.actualMode", dashboardModeLabel(m.snapshot.Mode.Actual, m.locale)), muted)
	}
	if m.proxiesDirect() {
		return finishEmpty(emptyStateOptions{Title: m.tr("proxies.direct.description")})
	}
	if len(m.proxies.history) > 0 {
		button("proxies.back", m.tr("proxies.back"))
	} else if m.proxiesNarrow() && m.proxies.activeGroup != "" {
		button("proxies.directory", m.tr("proxies.directory"))
	}
	groups := m.visibleProxyGroups()
	if len(groups) == 0 {
		state := emptyStateOptions{Title: m.tr("proxies.no_groups")}
		if m.proxySearchQuery() != "" && m.proxyGroupsForMode() > 0 {
			state.Title = m.tr("proxies.search_no_match")
			state.ActionID, state.ActionLabel = "proxies.empty.clear", m.tr("empty.proxies.clear_search")
		}
		return finishEmpty(state)
	}
	compactGroups := m.compactProxyCards()
	spacedDirectory := m.proxiesNarrow() && m.proxies.activeGroup == "" && m.height > proxyCompactMaxHeight
	columns := m.proxyHeaderColumns()
	layout.memberGutter = layout.grid.rowGap
	for i, gi := range groups {
		if i > 0 && (spacedDirectory || compactGroups && m.proxyGroupExpanded(m.snapshot.Groups[groups[i-1]].Name)) {
			b.blank()
		}
		g := m.snapshot.Groups[gi]
		b.append(m.proxyGroupHeader(gi, width, columns))
		if m.proxyGroupExpanded(g.Name) {
			if layout.memberGutter > 0 {
				b.blank()
			}
			members := m.proxyMembers(g)
			if len(members) == 0 {
				if len(groups) == 1 {
					return finishEmpty(m.proxyEmptyMembers(gi))
				}
				b.append(m.emptyState(width, 3, m.proxyEmptyMembers(gi)))
			} else {
				layout.appendBlock(b)
				layout.appendGrid(gi, members)
				b = dashboardBlock{}
			}
		}
	}
	return finish()
}

type proxyHeaderColumns struct{ name, kind, count int }

func (m *Model) proxyHeaderColumns() proxyHeaderColumns {
	var cols proxyHeaderColumns
	for _, g := range m.snapshot.Groups {
		cols.name = max(cols.name, ansi.StringWidth(clean(g.Name)))
		cols.count = max(cols.count, len(strconv.Itoa(len(g.All))))
		cols.kind = max(cols.kind, ansi.StringWidth(m.proxyGroupTypeLabel(g.Type)))
	}
	return cols
}

func (m *Model) proxyGroupHeader(gi, width int, cols proxyHeaderColumns) dashboardBlock {
	g := m.snapshot.Groups[gi]
	id := fmt.Sprintf("group.%d", gi)
	testID, sortID := fmt.Sprintf("proxies.test.%d", gi), fmt.Sprintf("proxies.sort.%d", gi)
	typeID := proxyTypeTarget + strconv.Itoa(gi)
	expanded := m.proxyGroupExpanded(g.Name)
	marker, arrow := "  ", "▸"
	if m.focus == id && !m.menu.dropdownOpen(id) {
		marker = "› "
	}
	color := text
	if expanded {
		arrow, color = "▾", accent
	}
	surface := bg
	if m.hover == id || m.hover == testID || m.hover == sortID || m.hover == typeID {
		surface = hoverBackground
		if m.effectiveTheme() == app.ThemeDark {
			surface = raised
		}
	}
	summary := clean(m.proxyGroupSummary(g))
	count := fmt.Sprintf("%*d", cols.count, len(g.All))
	trailing := "  " + count + "  " + arrow + " "
	if width < 32 {
		trailing = " " + count + " " + arrow
	}
	// The wide columns depend only on the directory and available width, so
	// opening a group or updating its test progress never shifts its identity.
	const columnGap = "    "
	nameWidth := min(max(cols.name, 18), 24, width/5)
	typeWidth := min(max(cols.kind, 12), 16)
	typeX, typeY := 2+nameWidth+len(columnGap), 0
	minTitle := typeX + typeWidth + len(columnGap) + 6 // keep some of the current node visible
	if m.proxiesNarrow() {
		typeX, typeY = 2, 1
		minTitle = min(12, 2+ansi.StringWidth(clean(g.Name)), max(4, width/5))
	}
	kind := cellCrop(m.proxyGroupTypeLabel(g.Type), min(typeWidth, width-typeX))
	rightWidth := ansi.StringWidth(trailing)
	actionBudget := max(0, width-rightWidth-minTitle)
	sortable := expanded && m.proxySortable(g)
	sortValue := ""
	testValue := ""
	testing := m.proxies.testGroup == g.Name && m.proxies.testing
	showResult := m.proxies.testGroup == g.Name && m.proxies.testStatusVisible
	if testing || showResult || expanded && m.proxyGroupHasTestAction(g.Name) {
		label := m.tr("proxies.test_group")
		switch {
		case testing:
			label = fmt.Sprintf("%s %d/%d", m.tr("proxies.cancel_test"), m.proxies.completed, m.proxies.total)
		case showResult:
			label = m.tr("proxies.test."+m.proxies.testStatus, m.proxies.completed, m.proxies.total)
		}
		o := dashboardButtonOptions(testID, m.navigationOptions(), false, surface)
		o.Kind, o.Compact, o.Disabled = buttonText, width < 52, m.busy || m.quitting
		testValue = renderButton(label, o)
		testBudget := actionBudget
		if sortable {
			testBudget-- // even the smallest sort trigger can show its disclosure arrow
		}
		if (testing || showResult) && ansi.StringWidth(testValue) > testBudget {
			testValue = renderButton(fmt.Sprintf("%d/%d", m.proxies.completed, m.proxies.total), o)
		}
		testValue = cellCrop(testValue, testBudget)
	}
	if available := actionBudget - ansi.StringWidth(testValue); sortable && available > 0 {
		o := dashboardButtonOptions(sortID, m.navigationOptions(), false, surface)
		o.Disabled = m.busy || m.quitting
		sortValue, _ = m.proxySortDropdown(g.Name).render(sortID, o, m.menu.dropdownOpen(sortID), available)
	}
	sortWidth, testWidth := ansi.StringWidth(sortValue), ansi.StringWidth(testValue)
	leftWidth := max(0, width-rightWidth-sortWidth-testWidth)
	name := themed(lipgloss.NewStyle().Bold(true).Render(clean(g.Name)), color, surface, false)
	prefix := themed(marker, focus, surface, false)
	typeLabel := themed(kind, muted, surface, false)
	right := sortValue + testValue + themed(trailing, muted, surface, false)
	var b dashboardBlock
	if m.proxiesNarrow() {
		meta := "  " + typeLabel
		room := width - ansi.StringWidth(meta)
		separator := " · "
		if width < 24 && room < ansi.StringWidth(summary)+3 {
			separator = " "
		}
		if gap := ansi.StringWidth(separator); kind != "" && room > gap {
			meta += themed(separator, muted, surface, false)
			room -= gap
		} else if kind != "" {
			room = 0
		}
		b.lines = []string{cellPad(prefix+name, leftWidth) + right, meta + cellCrop(summary, room)}
	} else {
		left := prefix + cellPad(name, nameWidth) + columnGap + cellPad(typeLabel, typeWidth) + columnGap
		b.lines = []string{cellPad(left+summary, leftWidth) + right}
	}
	// Reserve action rectangles even when disabled so clicking them never folds
	// the group. The remaining header surface belongs to the folding target.
	var actionRegions []hitRegion
	reserve := func(action string, x, y, w int, enabled bool) {
		if w <= 0 {
			return
		}
		hit := hitRegion{ID: action, X: x, Y: y, W: w, H: 1, DropdownRight: action == sortID}
		actionRegions = append(actionRegions, hit)
		if enabled {
			b.targets = append(b.targets, hit)
		}
	}
	if proxyGroupConfigType(g.Type) != "" {
		// The type is a hover target, including when actions are disabled.
		reserve(typeID, typeX, typeY, ansi.StringWidth(kind), true)
	}
	enabled := !m.busy && !m.quitting
	reserve(sortID, leftWidth, 0, sortWidth, enabled)
	reserve(testID, leftWidth+sortWidth, 0, testWidth, enabled)
	if !m.compactProxyCards() {
		b.lines = append([]string{""}, b.lines...)
		b.blank()
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

func (m *Model) proxyGroupBody(gi, width int) dashboardBlock {
	indices := m.proxyMembers(m.snapshot.Groups[gi])
	if len(indices) == 0 {
		return m.emptyState(width, 3, m.proxyEmptyMembers(gi))
	}
	grid := m.proxyGrid(width)
	return m.proxyGroupRows(gi, width, grid, indices, 0, (len(indices)+grid.columns-1)/grid.columns)
}

func (m *Model) proxyGroupRows(gi, width int, grid proxyGridLayout, indices []int, firstRow, endRow int) dashboardBlock {
	b := dashboardBlock{}
	for row := firstRow; row < endRow; row++ {
		first := row * grid.columns
		if row > firstRow && grid.rowGap > 0 {
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
		b.append(m.emptyState(width, 3, m.proxyEmptyMembers(gi)))
	}
	return b
}
func (m *Model) proxyEmptyMembers(gi int) emptyStateOptions {
	state := emptyStateOptions{Title: m.tr("proxies.empty_group")}
	if len(m.snapshot.Groups[gi].All) > 0 && m.proxySearchQuery() != "" {
		state.Title = m.tr("proxies.search_no_match")
		state.ActionID, state.ActionLabel = "proxies.empty.clear", m.tr("empty.proxies.clear_search")
	}
	return state
}

// Count the unfiltered directory for the current routing mode. A search cannot
// cause a no-match state when the directory was already empty.
func (m *Model) proxyGroupsForMode() int {
	return len(m.proxyGroups(""))
}

func (m *Model) proxyDelayLabel(group, node string) (string, string) {
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
		if r.Delay >= 150 {
			color = warning
		}
		return fmt.Sprintf("%d ms", r.Delay), color
	case "timeout":
		return m.tr("proxies.timeout"), muted
	case "failed":
		return m.tr("proxies.test_failed"), muted
	default:
		return m.tr("proxies.not_tested"), muted
	}
}

func proxyNodeForeground(foreground string, emphasized bool) string {
	if emphasized {
		switch foreground {
		case muted:
			return proxyNodeEmphasizedMeta
		case success:
			return proxyNodeEmphasizedFast
		case warning:
			return proxyNodeEmphasizedSlow
		}
	}
	return foreground
}

func (m *Model) proxyNode(gi, ni, width int) dashboardBlock {
	g := m.snapshot.Groups[gi]
	node := g.All[ni]
	id := fmt.Sprintf("node.%d.%d", gi, ni)
	referenceID := fmt.Sprintf("proxies.reference.%d.%d", gi, ni)
	delayID := fmt.Sprintf("proxies.delay.%d.%d", gi, ni)
	selected := g.Now == node && g.Type != "LoadBalance" && g.Type != "Relay"
	hovered := m.hover == id || m.hover == referenceID || m.hover == delayID
	dark := m.effectiveTheme() == app.ThemeDark
	surface := gridCardBackground
	if selected {
		surface = proxyNodeSelectedBackground
	} else if hovered {
		surface = raised
		if !dark {
			surface = buttonHoverBackground
		}
	}
	emphasized := dark && (selected || hovered)
	prefix := " "
	if m.focus == id || m.focus == delayID {
		prefix = "›"
	}
	padding := m.proxyCardPadding()
	rail, railColor, railSurface := " ", accent, surface
	if selected {
		rail = "▌"
		if padding > 0 {
			// Paint the inner half against the card while retaining the plain-text marker.
			railColor, railSurface = bg, accent
		}
	}
	edge := themed(rail, railColor, railSurface, false)
	value, color := m.proxyDelayLabel(g.Name, node)
	if !m.proxyTestable(node) {
		value = "—"
	}
	if m.busy && m.proxies.pendingGroup == g.Name && m.proxies.pendingNode == node {
		value, color = m.tr("proxies.switching"), accent
	}
	rightPadding, contentWidth, valueWidth := proxyNodeWidths(width)
	nameWidth := max(0, contentWidth-valueWidth-1)
	_, delaySupported := m.backend.(proxyDelayBackend)
	delayEnabled := delaySupported && m.proxyTestable(node) && m.proxiesReady() && !m.busy && !m.quitting
	delaySurface := surface
	delayPending := value == m.tr("proxies.testing") || value == m.tr("proxies.queued")
	if delayEnabled && !delayPending && (m.hover == delayID || m.focus == delayID) {
		delaySurface = inputBackground
	}
	color = proxyNodeForeground(color, emphasized && delaySurface == surface)
	value = cellCrop(value, valueWidth)
	value = strings.Repeat(" ", valueWidth-ansi.StringWidth(value)) + value
	trailing := themed(strings.Repeat(" ", rightPadding), text, surface, false)
	name := lipgloss.NewStyle().Bold(true).Render(clean(node))
	line := edge + themed(prefix, focus, surface, false) + themed(cellPad(name, nameWidth), text, surface, false) + themed(" ", color, surface, false) + themed(value, color, delaySurface, false) + trailing
	metaWidth := contentWidth
	_, _, reference := m.proxyGroup(node)
	if reference {
		metaWidth = nameWidth
	}
	meta := edge + themed(" "+cellPad(clean(m.proxyNodeMeta(node)), metaWidth), proxyNodeForeground(muted, emphasized), surface, false)
	var actions []hitRegion
	actionX := width - rightPadding - valueWidth
	if valueWidth > 0 {
		actions = append(actions, hitRegion{ID: delayID, X: actionX, Y: padding, W: valueWidth, H: 1})
	}
	if reference && valueWidth > 0 {
		options := dashboardButtonOptions(referenceID, m.navigationOptions(), false, surface)
		options.Kind, options.Compact, options.Disabled = buttonText, true, m.busy || m.quitting
		options.Width = max(0, valueWidth-3) // one focus cell and two face padding cells
		button := renderButton(m.tr("proxies.enter"), options)
		meta = cellPad(meta, actionX) + cellCrop(button, valueWidth)
		actions = append(actions, hitRegion{ID: referenceID, X: actionX, Y: padding + 1, W: valueWidth, H: 1})
	}
	meta += trailing
	b := dashboardBlock{lines: make([]string, proxyCardHeight(padding))}
	b.lines[padding] = themed(cellPad(line, width), text, surface, false)
	b.lines[padding+1] = themed(cellPad(meta, width), text, surface, false)
	if padding > 0 {
		b.lines[0], b.lines[len(b.lines)-1] = proxyCardEdges(width, surface, selected, m.dashboardDimmed())
	}
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

func proxyCardEdges(width int, surface string, selected, dimmed bool) (top, bottom string) {
	if dimmed {
		// The monochrome backdrop would turn the shaped edges into solid text.
		blank := strings.Repeat(" ", width)
		return blank, blank
	}
	// Mirror the face and its half-width rail around the two text rows.
	if selected && width > 0 {
		top = themed("▗", accent, bg, false)
		bottom = themed("▝", accent, bg, false)
		width--
	}
	top += themed(strings.Repeat("▄", width), surface, bg, false)
	bottom += themed(strings.Repeat("▀", width), surface, bg, false)
	return top, bottom
}

// For a padded card, the full-height primary region includes both text rows.
// The other regions for that card only matter for mouse input.
func proxyBodyTextTargets(hits []hitRegion) iter.Seq[hitRegion] {
	return func(yield func(hitRegion) bool) {
		var paddedID string
		for _, hit := range hits {
			if paddedID != "" && hit.ID == paddedID {
				continue
			}
			if strings.HasPrefix(hit.ID, "node.") && hit.H > proxyNodeContentRows {
				paddedID = hit.ID
				hit.Y += (hit.H - proxyNodeContentRows) / 2
				hit.H = proxyNodeContentRows
			}
			if !yield(hit) {
				return
			}
		}
	}
}

// Body targets are in section order, with each full-height heading before its
// member cards. Once a heading is visible, there is no need for a second label.
func proxyStickyGroup(groups []mihomo.Group, hits []hitRegion, scroll, height int) string {
	if scroll <= 0 || height <= 0 {
		return ""
	}
	group, memberPrefix := "", ""
	for hit := range proxyBodyTextTargets(hits) {
		if indexText, ok := strings.CutPrefix(hit.ID, "group."); ok {
			if hit.Y+hit.H > scroll {
				break
			}
			index, err := strconv.Atoi(indexText)
			if err == nil && index >= 0 && index < len(groups) {
				group, memberPrefix = groups[index].Name, "node."+indexText+"."
			}
		} else if memberPrefix != "" && hit.Y < scroll+height && hit.Y+hit.H > scroll && strings.HasPrefix(hit.ID, memberPrefix) {
			return group
		}
	}
	return ""
}

// Count unique members in visual order if at least one text row is visible.
// Split hit regions keep each card's inline action independent from selection.
func proxyVisibleRange(locale i18n.Localizer, hits []hitRegion, scroll, height, width int) string {
	if height <= 0 {
		return ""
	}
	seen := make(map[string]int)
	first, last, total, groups := 0, 0, 0, 0
	lastGroup := ""
	for hit := range proxyBodyTextTargets(hits) {
		if !strings.HasPrefix(hit.ID, "node.") {
			continue
		}
		ordinal, exists := seen[hit.ID]
		if !exists {
			group, _, _ := strings.Cut(hit.ID[len("node."):], ".")
			if group != lastGroup {
				groups++
				lastGroup = group
			}
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
	return proxyMemberRange(locale, first, last, total, groups, width)
}

func proxyMemberRange(locale i18n.Localizer, first, last, total, groups, width int) string {
	if first == 0 || first == 1 && last == total {
		return ""
	}
	value := locale.T("proxies.visible_range", first, last, total)
	var label string
	if groups > 1 {
		label = locale.T("proxies.range.expanded", groups, value)
	} else {
		label = locale.T("proxies.range.group", value)
	}
	if ansi.StringWidth(label) <= width {
		return label
	}
	return value
}
