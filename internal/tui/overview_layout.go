package tui

import (
	"strings"

	"github.com/nelvko/lazyclash/internal/i18n"
)

// Keep card identities independent of their position and translated contents.
// Paired cards share both edges and dimensions; each row grows independently.
func overviewLayout(o dashboardOptions, width int) dashboardBlock {
	if overviewSetupVisible(o) {
		return overviewNamedCard("dashboard.guide.title", getStarted(o, width), width)
	}
	cards := []struct {
		id     string
		render func(dashboardOptions, int) dashboardBlock
	}{
		{"dashboard.control.core", overviewCoreCard},
		{"dashboard.profile.title", overviewProfileCard},
		{"dashboard.overview.network", overviewNetworkCard},
		{"dashboard.control.routingMode", overviewModeCard},
		{"dashboard.system.title", overviewSystemCard},
		{"dashboard.overview.ipInfo", overviewIPCard},
		{"dashboard.traffic.title", overviewTrafficCard},
	}
	paired := width >= 90
	columnWidth := (width - 2) / 2
	var body dashboardBlock
	for index := 0; index < len(cards); {
		if index > 0 {
			gap := ""
			if paired && cards[index].id == "dashboard.system.title" {
				// Join the route and IP surfaces across the gap without extending either focus region.
				right := themed(strings.Repeat(" ", columnWidth), text, panel, o.Dim)
				gap = themed(cellPad(strings.Repeat(" ", columnWidth+2)+right, width), text, bg, o.Dim)
			}
			body.lines = append(body.lines, gap)
		}
		render := func(index, w int) dashboardBlock {
			card := cards[index]
			return overviewNamedCard(card.id, card.render(o, w), w)
		}
		if paired && cards[index].id != "dashboard.traffic.title" {
			row := overviewColumns(o, render(index, columnWidth), render(index+1, columnWidth), columnWidth)
			// An odd spare cell stays outside the cards, preserving equal widths.
			if 2*columnWidth+2 < width {
				for y := range row.lines {
					row.lines[y] += themed(" ", text, bg, o.Dim)
				}
			}
			body.append(row)
			index += 2
		} else {
			body.append(render(index, width))
			index++
		}
	}
	return body
}

func overviewNamedCard(id string, block dashboardBlock, width int) dashboardBlock {
	if width >= 4 {
		block.cards = []hitRegion{{ID: "overview.card." + id, W: width, H: len(block.lines)}}
	}
	return block
}

// Reserve the same perimeter as a framed panel so focus can draw an outline
// without moving the content, actions or adjacent cards.
func overviewSurface(lines []string, width, height int, o dashboardOptions) []string {
	if width < 4 {
		return panelFrame(lines, width, height, o)
	}
	height = max(height, len(lines)+2)
	result := make([]string, height)
	result[0] = themed(strings.Repeat(" ", width), text, panel, o.Dim)
	for y := 1; y < height-1; y++ {
		content := ""
		if y-1 < len(lines) {
			content = lines[y-1]
		}
		result[y] = themed("  ", text, panel, o.Dim) + themed(cellPad(content, width-4), text, panel, o.Dim) + themed("  ", text, panel, o.Dim)
	}
	result[height-1] = result[0]
	return result
}

func overviewColumns(o dashboardOptions, left, right dashboardBlock, width int) dashboardBlock {
	height := max(len(left.lines), len(right.lines))
	left = overviewCardHeight(o, left, width, height)
	right = overviewCardHeight(o, right, width, height)
	var b dashboardBlock
	for y := 0; y < height; y++ {
		b.lines = append(b.lines, cellPad(left.lines[y], width)+themed("  ", text, bg, o.Dim)+cellPad(right.lines[y], width))
	}
	b.targets = append(b.targets, left.targets...)
	b.cards = append(b.cards, left.cards...)
	for _, target := range right.targets {
		target.X += width + 2
		b.targets = append(b.targets, target)
	}
	for _, card := range right.cards {
		card.X += width + 2
		b.cards = append(b.cards, card)
	}
	return b
}

func overviewCardHeight(o dashboardOptions, block dashboardBlock, width, height int) dashboardBlock {
	padding := height - len(block.lines)
	if padding <= 0 {
		return block
	}
	insert := len(block.lines) - 1
	if block.footerY > 0 {
		insert = block.footerY
		block.footerY += padding
	}
	lines := append([]string(nil), block.lines[:insert]...)
	blank := overviewSurface(nil, width, 3, o)[1]
	for range padding {
		lines = append(lines, blank)
	}
	block.lines = append(lines, block.lines[insert:]...)
	for i := range block.targets {
		if block.targets[i].Y >= insert {
			block.targets[i].Y += padding
		}
	}
	for i := range block.cards {
		block.cards[i].H = height
	}
	return block
}

func overviewVersion(s, fallback string, locale i18n.Localizer) string {
	if s = clean(s); s != "" {
		return s
	}
	return locale.T(fallback)
}
