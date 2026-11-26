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
	memberGutter int
	grid         proxyGridLayout
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
	l.total += rows*l.grid.stride() - l.grid.rowGap
}

func (l proxyBodyLayout) visibleRows(part proxyBodyPart, start, end, inset int) (int, int) {
	y := part.y + inset
	if start >= end || end <= y {
		return 0, 0
	}
	stride := l.grid.stride()
	height := proxyCardHeight(l.grid.cardPadding) - 2*inset
	rows := (len(part.members) + l.grid.columns - 1) / l.grid.columns
	first := max(0, (start-y)/stride)
	if y+first*stride+height <= start {
		first++
	}
	last := min(rows, (end-1-y)/stride+1)
	return first, last
}

func (m *Model) paintProxiesBody(layout proxyBodyLayout, start, end int) dashboardBlock {
	out := dashboardBlock{lines: make([]string, end-start)}
	for _, part := range layout.parts {
		if len(part.members) == 0 {
			out.copyVisibleBlock(part.block, part.y, start)
			continue
		}
		first, last := layout.visibleRows(part, start, end, 0)
		if first < last {
			block := m.proxyGroupRows(part.group, layout.width, layout.grid, part.members, first, last)
			out.copyVisibleBlock(block, part.y+first*layout.grid.stride(), start)
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
			y := part.y + (i/grid.columns)*grid.stride()
			if listOnly {
				targets = append(targets, hitRegion{
					ID: fmt.Sprintf("node.%d.%d", part.group, member), X: x, Y: y,
					W: grid.cardWidth, H: proxyCardHeight(grid.cardPadding),
				})
				continue
			}
			for _, target := range m.proxyNodeKeyboardTargets(part.group, member, grid) {
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
		from, to := layout.visibleRows(part, start, end, layout.grid.cardPadding)
		if from < to {
			if first == 0 {
				first = total + from*layout.grid.columns + 1
				if part.y-layout.memberGutter <= start {
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
	const prefixWidth = 2 // selection rail and keyboard focus marker
	rightPadding = min(1, max(0, width-prefixWidth))
	contentWidth = max(0, width-prefixWidth-rightPadding)
	valueWidth = min(10, contentWidth/2)
	return rightPadding, contentWidth, valueWidth
}

// The primary card region and inline actions are the only distinct keyboard
// targets. Mouse rendering adds the other rectangles around those actions.
func (m *Model) proxyNodeKeyboardTargets(gi, ni int, grid proxyGridLayout) []hitRegion {
	rightPadding, _, valueWidth := proxyNodeWidths(grid.cardWidth)
	leftWidth := grid.cardWidth
	var actions []hitRegion
	node := m.snapshot.Groups[gi].All[ni]
	if valueWidth > 0 {
		leftWidth = grid.cardWidth - rightPadding - valueWidth
		if _, delaySupported := m.backend.(proxyDelayBackend); delaySupported && !m.busy && !m.quitting && m.proxiesReady() && m.proxyTestable(node) {
			actions = append(actions, hitRegion{
				ID: fmt.Sprintf("proxies.delay.%d.%d", gi, ni), X: leftWidth,
				Y: grid.cardPadding, W: valueWidth, H: 1,
			})
		}
		if _, _, reference := m.proxyGroup(node); reference && !m.busy && !m.quitting {
			actions = append(actions, hitRegion{
				ID: fmt.Sprintf("proxies.reference.%d.%d", gi, ni), X: leftWidth,
				Y: grid.cardPadding + 1, W: valueWidth, H: 1,
			})
		}
	}
	return append([]hitRegion{{ID: fmt.Sprintf("node.%d.%d", gi, ni), W: leftWidth, H: proxyCardHeight(grid.cardPadding)}}, actions...)
}

func (m *Model) proxyListFocusValid() bool {
	if !m.proxiesReady() || m.proxiesDirect() {
		return false
	}
	gi, group, node, ok := m.proxyAtFocus()
	if !ok || !slices.Contains(m.visibleProxyGroups(), gi) {
		return false
	}
	if strings.HasPrefix(m.focus, "group.") {
		return m.focus == fmt.Sprintf("group.%d", gi)
	}
	query := m.proxySearchQuery()
	return node != "" && m.proxyGroupExpanded(group.Name) && (m.proxySearchMatchesGroup(query, group) || m.proxySearchMatchesNode(query, node))
}
