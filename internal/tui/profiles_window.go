package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type profileGridLayout struct {
	names                             []string
	intro, detail, empty              dashboardBlock
	width, columns, cardWidth, cardH  int
	detailRow, detailY, emptyY, total int
	compact                           bool
}

func (m *Model) layoutProfiles(width int) profileGridLayout {
	width = max(1, width)
	l := profileGridLayout{width: width, columns: 1, cardH: 6, detailRow: -1, compact: m.compactProfileCards()}
	if !m.snapshotReady {
		body, targets := m.unavailableProfiles(width)
		l.intro = dashboardBlock{lines: strings.Split(body, "\n"), targets: targets}
		l.total = len(l.intro.lines)
		return l
	}
	if l.compact {
		l.cardH = 4
	}
	if width >= 86 {
		l.columns = 2
	}
	l.cardWidth = (width - 2*(l.columns-1)) / l.columns
	l.names = m.profiles()
	l.intro = m.profilesIntro(width, len(l.names)-1)
	l.total = len(l.intro.lines) + l.rowCount()*l.stride()
	for i, name := range l.names {
		if name == m.profilesUI.detail {
			l.detailRow = i / l.columns
			l.detailY = l.rowY(l.detailRow) + l.stride()
			l.detail = m.profileDetailsPaneAt(name, i, width)
			l.total += len(l.detail.lines) + 1
			break
		}
	}
	if len(l.names) == 1 {
		l.emptyY = l.total
		height := max(1, dashboardGeometry(m.dashboardOptions()).height-l.total)
		l.empty = m.emptyState(width, height, emptyStateOptions{
			Title: m.tr("empty.profiles.none"), Detail: m.tr("profiles.empty_hint"),
		})
		l.total += len(l.empty.lines)
	}
	return l
}

func (l profileGridLayout) stride() int { return l.cardH + 1 }

func (l profileGridLayout) rowCount() int {
	return (len(l.names) + l.columns - 1) / l.columns
}

func (l profileGridLayout) rowY(row int) int {
	y := len(l.intro.lines) + row*l.stride()
	if l.detailRow >= 0 && row > l.detailRow {
		y += len(l.detail.lines) + 1
	}
	return y
}

func (l profileGridLayout) card(index int) hitRegion {
	return hitRegion{ID: "profile." + strconv.Itoa(index), X: (index % l.columns) * (l.cardWidth + 2),
		Y: l.rowY(index / l.columns), W: l.cardWidth, H: l.cardH}
}

func (m *Model) paintProfiles(l profileGridLayout, start, end int) dashboardBlock {
	out := dashboardBlock{lines: make([]string, end-start)}
	out.copyVisibleBlock(l.intro, 0, start)
	for row := 0; row < l.rowCount(); row++ {
		y := l.rowY(row)
		if y >= end {
			break
		}
		if y+l.cardH > start {
			out.copyVisibleBlock(m.paintProfileRow(l, row*l.columns), y, start)
		}
		if row == l.detailRow {
			out.copyVisibleBlock(l.detail, l.detailY, start)
		}
	}
	out.copyVisibleBlock(l.empty, l.emptyY, start)
	return out
}

// Only visible cards are painted. Tab and focus restoration keep the geometry
// of every card and inline action, including links below the viewport.
func (m *Model) profilesTargets(width int) []hitRegion {
	l := m.layoutProfiles(width)
	targets := append([]hitRegion(nil), l.intro.targets...)
	refreshable := make(map[string]bool)
	for _, info := range m.snapshot.ProfileDetails {
		if info.Refreshable {
			refreshable[info.ID] = true
		}
	}
	widths := make(map[string]int, 2)
	for _, button := range profileCardControls("profiles.refresh.", "profiles.menu.", l.cardWidth, true) {
		widths[button.label] = ansi.StringWidth(m.profileButtonView(button, panel))
	}
	for i, name := range l.names {
		card := l.card(i)
		index := strconv.Itoa(i)
		controls := profileCardControls("profiles.refresh."+index, "profiles.menu."+index, card.W, refreshable[name])
		targets = append(targets, m.profileCardTargets(card, controls, widths, l.compact)...)
		if l.detailRow >= 0 && i == min(len(l.names), (l.detailRow+1)*l.columns)-1 {
			for _, target := range l.detail.targets {
				target.Y += l.detailY
				targets = append(targets, target)
			}
		}
	}
	for _, target := range l.empty.targets {
		target.Y += l.emptyY
		targets = append(targets, target)
	}
	return targets
}
