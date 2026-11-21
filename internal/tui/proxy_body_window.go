package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
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
	l.total += (rows-1)*l.stride() + l.cardHeight()
}

func (l proxyBodyLayout) cardHeight() int {
	if l.compact {
		return 2
	}
	return 3
}

func (l proxyBodyLayout) stride() int {
	if l.compact {
		return 2
	}
	return 4
}

func (l proxyBodyLayout) visibleRows(part proxyBodyPart, start, end int) (int, int) {
	if start >= end || end <= part.y {
		return 0, 0
	}
	rows := (len(part.members) + l.grid.columns - 1) / l.grid.columns
	first := max(0, (start-part.y)/l.stride())
	if part.y+first*l.stride()+l.cardHeight() <= start {
		first++
	}
	last := min(rows, (end-1-part.y)/l.stride()+1)
	return first, last
}

func (m *Model) paintProxiesBody(layout proxyBodyLayout, start, end int) dashboardBlock {
	out := dashboardBlock{lines: make([]string, end-start)}
	add := func(block dashboardBlock, y int) {
		first, last := max(start, y), min(end, y+len(block.lines))
		if first >= last {
			return
		}
		copy(out.lines[first-start:last-start], block.lines[first-y:last-y])
		for _, target := range block.targets {
			target.Y += y
			if target.Y < end && target.Y+target.H > start {
				out.targets = append(out.targets, target)
			}
		}
	}
	for _, part := range layout.parts {
		if len(part.members) == 0 {
			add(part.block, part.y)
			continue
		}
		first, last := layout.visibleRows(part, start, end)
		if first < last {
			block := m.proxyGroupRows(part.group, layout.width, layout.grid, part.members, first, last, layout.compact)
			add(block, part.y+first*layout.stride())
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
					W: grid.cardWidth, H: layout.cardHeight(),
				})
				continue
			}
			for _, target := range m.proxyNodeKeyboardTargets(part.group, member, grid.cardWidth, layout.cardHeight()) {
				target.X += x
				target.Y += y
				targets = append(targets, target)
			}
		}
	}
	return targets
}

func (m *Model) proxyBodyRange(layout proxyBodyLayout, start, end int) (stickyGroup, label string) {
	first, last, total := 0, 0, 0
	for _, part := range layout.parts {
		if len(part.members) == 0 {
			continue
		}
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
	if first > 0 {
		label = m.tr("proxies.visible_range", first, last, total)
	}
	return stickyGroup, label
}

func proxyNodeWidths(width int) (rightPadding, contentWidth, valueWidth int) {
	rightPadding = min(2, max(0, width-4))
	contentWidth = max(0, width-4-rightPadding)
	valueWidth = min(12, contentWidth/2)
	return rightPadding, contentWidth, valueWidth
}

// The primary card region and inline actions are the only distinct keyboard
// targets. Mouse rendering adds the other rectangles around those actions.
func (m *Model) proxyNodeKeyboardTargets(gi, ni, width, height int) []hitRegion {
	rightPadding, contentWidth, valueWidth := proxyNodeWidths(width)
	leftWidth := width
	var actions []hitRegion
	node := m.snapshot.Groups[gi].All[ni]
	if valueWidth > 0 {
		leftWidth = width - rightPadding - valueWidth
		if _, delaySupported := m.backend.(proxyDelayBackend); delaySupported && !m.busy && !m.quitting && m.proxiesReady() && m.proxyTestable(node) {
			actions = append(actions, hitRegion{
				ID: fmt.Sprintf("proxies.delay.%d.%d", gi, ni), X: leftWidth,
				Y: height - 2, W: valueWidth, H: 1,
			})
		}
	}
	if _, _, reference := m.proxyGroup(node); reference {
		id := fmt.Sprintf("proxies.reference.%d.%d", gi, ni)
		buttonWidth := min(contentWidth, ansi.StringWidth(m.proxyButton(id, m.tr("proxies.enter"), panel, true)))
		if buttonWidth > 0 {
			x := width - rightPadding - buttonWidth
			leftWidth = min(leftWidth, x)
			if !m.busy && !m.quitting {
				actions = append(actions, hitRegion{ID: id, X: x, Y: height - 1, W: buttonWidth, H: 1})
			}
		}
	}
	return append([]hitRegion{{ID: fmt.Sprintf("node.%d.%d", gi, ni), W: leftWidth, H: height}}, actions...)
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
