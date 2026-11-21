package tui

import (
	"fmt"
	"slices"
	"strings"
)

type proxyBodyPart struct {
	y       int
	block   dashboardBlock
	group   int
	members []int
}

type proxyBodyLayout struct {
	width, total int
	grid         proxyGridLayout
	compact      bool
	parts        []proxyBodyPart
}

func (l *proxyBodyLayout) appendBlock(block dashboardBlock) {
	if len(block.lines) == 0 {
		return
	}
	l.parts = append(l.parts, proxyBodyPart{y: l.total, block: block})
	l.total += len(block.lines)
}

func (l *proxyBodyLayout) appendGrid(group int, members []int) {
	l.parts = append(l.parts, proxyBodyPart{y: l.total, group: group, members: members})
	rows := (len(members) + l.grid.columns - 1) / l.grid.columns
	l.total += (rows-1)*l.stride() + proxyNodeHeight
}

func (l proxyBodyLayout) stride() int {
	if l.compact {
		return proxyNodeHeight
	}
	return proxyNodeHeight + 1
}

func (l proxyBodyLayout) visibleRows(part proxyBodyPart, start, end int) (int, int) {
	if start >= end || end <= part.y {
		return 0, 0
	}
	rows := (len(part.members) + l.grid.columns - 1) / l.grid.columns
	first := max(0, (start-part.y)/l.stride())
	if part.y+first*l.stride()+proxyNodeHeight <= start {
		first++
	}
	last := min(rows, (end-1-part.y)/l.stride()+1)
	return first, last
}

func (m *Model) paintProxiesBody(layout proxyBodyLayout, start, end int) dashboardBlock {
	out := dashboardBlock{lines: make([]string, end-start)}
	for _, part := range layout.parts {
		if len(part.members) == 0 {
			out.copyVisibleBlock(part.block, part.y, start)
			continue
		}
		first, last := layout.visibleRows(part, start, end)
		if first < last {
			block := m.proxyGroupRows(part.group, layout.width, layout.grid, part.members, first, last, layout.compact)
			out.copyVisibleBlock(block, part.y+first*layout.stride(), start)
		}
	}
	return out
}

// A node grid row has fixed geometry even when only some cards in it are
// painted. Logical keyboard targets retain offsets into the whole body.
func (m *Model) proxyTargets(width int, listOnly bool) []hitRegion {
	layout := m.layoutProxiesBody(width)
	var targets []hitRegion
	for _, part := range layout.parts {
		if len(part.members) == 0 {
			for _, target := range part.block.targets {
				if !listOnly || strings.HasPrefix(target.ID, "group.") {
					target.Y += part.y
					targets = append(targets, target)
				}
			}
			continue
		}
		grid := layout.grid
		for i, member := range part.members {
			x := grid.indent + (i%grid.columns)*(grid.cardWidth+proxyColumnGap)
			y := part.y + (i/grid.columns)*layout.stride()
			if listOnly {
				targets = append(targets, hitRegion{
					ID: fmt.Sprintf("node.%d.%d", part.group, member), X: x, Y: y,
					W: grid.cardWidth, H: proxyNodeHeight,
				})
				continue
			}
			for _, target := range m.proxyNodeKeyboardTargets(part.group, member, grid.cardWidth) {
				target.X += x
				target.Y += y
				targets = append(targets, target)
			}
		}
	}
	return targets
}

func (m *Model) proxyBodyRange(layout proxyBodyLayout, start, end int) (stickyGroup, label string) {
	first, last, total, groups := 0, 0, 0, 0
	for _, part := range layout.parts {
		if len(part.members) == 0 {
			continue
		}
		groups++
		from, to := layout.visibleRows(part, start, end)
		if from < to {
			if first == 0 {
				first = total + from*layout.grid.columns + 1
				if part.y <= start {
					stickyGroup = m.snapshot.Groups[part.group].Name
				}
			}
			last = total + min(len(part.members), to*layout.grid.columns)
		}
		total += len(part.members)
	}
	return stickyGroup, proxyMemberRange(m.locale, first, last, total, groups, layout.width)
}

func proxyNodeWidths(width int) (rightPadding, contentWidth, valueWidth int) {
	rightPadding = min(2, max(0, width-4))
	contentWidth = max(0, width-4-rightPadding)
	valueWidth = min(10, contentWidth/2)
	return rightPadding, contentWidth, valueWidth
}

// The primary card region and inline actions are the only distinct keyboard
// targets. Mouse rendering adds the other rectangles around those actions.
func (m *Model) proxyNodeKeyboardTargets(gi, ni, width int) []hitRegion {
	rightPadding, _, valueWidth := proxyNodeWidths(width)
	leftWidth := width
	var actions []hitRegion
	node := m.snapshot.Groups[gi].All[ni]
	if valueWidth > 0 {
		leftWidth = width - rightPadding - valueWidth
		if _, delaySupported := m.backend.(proxyDelayBackend); delaySupported && !m.busy && !m.quitting && m.proxiesReady() && m.proxyTestable(node) {
			actions = append(actions, hitRegion{
				ID: fmt.Sprintf("proxies.delay.%d.%d", gi, ni), X: leftWidth,
				Y: 0, W: valueWidth, H: 1,
			})
		}
		if _, _, reference := m.proxyGroup(node); reference && !m.busy && !m.quitting {
			actions = append(actions, hitRegion{
				ID: fmt.Sprintf("proxies.reference.%d.%d", gi, ni), X: leftWidth,
				Y: 1, W: valueWidth, H: 1,
			})
		}
	}
	return append([]hitRegion{{ID: fmt.Sprintf("node.%d.%d", gi, ni), W: leftWidth, H: proxyNodeHeight}}, actions...)
}

func (m *Model) proxyListFocusValid() bool {
	if !m.proxiesReady() || m.proxiesDirect() || m.proxies.detail != "" {
		return false
	}
	gi, group, node, ok := m.proxyAtFocus()
	if !ok || !slices.Contains(m.visibleProxyGroups(), gi) {
		return false
	}
	if strings.HasPrefix(m.focus, "group.") {
		return m.focus == fmt.Sprintf("group.%d", gi)
	}
	return node != "" && m.proxyGroupExpanded(group.Name) && m.proxyMemberMatches(group, node)
}
