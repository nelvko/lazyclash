package tui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type sidebarState struct {
	viewport    viewport.Model
	initialized bool
	reveal      bool
	bounds      hitRegion
	focus, page string
	popup       bool
}

type sidebarLayout struct {
	width, menuY, end, height, itemHeight, footerHeight int
}

func sidebarGeometry(o dashboardOptions, popup bool) sidebarLayout {
	g := dashboardGeometry(o)
	s := sidebarLayout{width: min(sidebarWidth, o.Width), menuY: g.headerHeight, end: dashboardContentBottom(o), itemHeight: 3}
	if popup {
		s.menuY = g.contentY + 1
	}
	available := max(0, s.end-s.menuY)
	if !popup {
		// Preserve a complete compact menu before reserving the clock row.
		footerRows := 1
		if available >= len(navPages())+2 {
			footerRows++
		}
		available = max(0, available-footerRows)
	}
	// Keep all seven destinations visible before spending rows on padding.
	if available < len(navPages())*s.itemHeight {
		s.itemHeight = 1
	}
	total := len(navPages()) * s.itemHeight
	if total > available {
		available = max(0, available-1)
	}
	s.height = min(total, available/s.itemHeight*s.itemHeight)
	if g.sidebar && !popup {
		s.footerHeight = s.end - s.menuY - s.height
		if s.height < total {
			s.footerHeight-- // The scroll indicator owns the row below the menu.
		}
	}
	return s
}

func (g sidebarLayout) hasClockRow() bool { return g.footerHeight >= 2 }

func (s *sidebarState) prepare(o dashboardOptions, g sidebarLayout, popup bool, lines []string) {
	bounds := hitRegion{Y: g.menuY, W: g.width, H: g.height}
	reveal := s.reveal || !s.initialized || s.bounds != bounds || s.popup != popup || s.page != o.Page || (strings.HasPrefix(o.Focus, "nav.") && s.focus != o.Focus)
	if !s.initialized {
		s.viewport = viewport.New()
		s.viewport.SoftWrap = false
		s.initialized = true
	}
	s.viewport.SetWidth(g.width)
	s.viewport.SetHeight(g.height)
	s.viewport.MouseWheelDelta = g.itemHeight
	s.viewport.SetContentLines(lines)
	// Keep whole items visible, including when resizing out of tiny mode.
	s.viewport.SetYOffset(s.viewport.YOffset() / g.itemHeight * g.itemHeight)
	if reveal {
		selected := o.Page
		if strings.HasPrefix(o.Focus, "nav.") {
			selected = strings.TrimPrefix(o.Focus, "nav.")
		}
		for index, page := range navPages() {
			if page != selected {
				continue
			}
			top, bottom := index*g.itemHeight, (index+1)*g.itemHeight
			if top < s.viewport.YOffset() {
				s.viewport.SetYOffset(top)
			} else if bottom > s.viewport.YOffset()+g.height {
				s.viewport.SetYOffset(bottom - g.height)
			}
			break
		}
	}
	s.bounds, s.focus, s.page, s.popup = bounds, o.Focus, o.Page, popup
	s.reveal = false
}

func drawSidebar(rows []string, o dashboardOptions, popup bool) []hitRegion {
	g := sidebarGeometry(o, popup)
	if g.width < 4 || g.end < 4 || g.height == 0 {
		return nil
	}
	header := dashboardGeometry(o)
	var targets []hitRegion
	var logoTarget *hitRegion
	start := 0
	if popup {
		start = header.contentY
	}
	for y := start; y < g.end; y++ {
		putRows(rows, 0, y, themed(strings.Repeat(" ", g.width), text, sidebarBackground, o.Dim), o.Width)
		if popup {
			putRows(rows, g.width-1, y, themed("│", divider, sidebarBackground, o.Dim), o.Width)
		}
	}
	if !popup {
		wordmark := logoTextFrame(o.LogoFrame, o.LogoIntroFrame, sidebarBackground, o.Dim)
		// Round right for the nine-cell wordmark so yClash stays put in wider frames.
		logoX := (g.width - ansi.StringWidth(wordmark) + 1) / 2
		putRows(rows, logoX, header.headerY, wordmark, o.Width)
		frame := logoFrameAt(o.LogoFrame)
		if header.headerY > 0 && frame.bubble != "" {
			column := len(logoPrefix) + frame.offset
			x := logoX + column
			color := logoGradientColor(column, 0)
			if frame.dim {
				color = disabled
			}
			putRows(rows, x, header.headerY-1, themed(frame.bubble, color, sidebarBackground, o.Dim), o.Width)
		}
		hit := hitRegion{ID: "logo", X: 0, Y: header.headerY, W: g.width, H: 1}
		logoTarget = &hit
	}
	pages := navPages()
	lines := make([]string, len(pages)*g.itemHeight)
	for index, page := range pages {
		id := "nav." + page
		label := pageTitle(page, o.Locale)
		labelStyle := lipgloss.NewStyle()
		current := ""
		background, foreground := sidebarBackground, muted
		if page == o.Page {
			background, foreground = bg, selectionForeground
			if popup {
				background = selectionBackground
			}
			labelStyle = labelStyle.Bold(true)
			if o.PlainButtons {
				current = " •"
			}
		}
		if o.Hover == id && page != o.Page {
			foreground = text
			if popup {
				background = hoverBackground
			}
		}
		marker := "  "
		if o.Focus == id {
			marker = "› "
		}
		// Center only the label; tiny popups clip decorations inside the menu.
		label = cellCrop(labelStyle.Render(label), max(1, g.width-6))
		padding := (g.width - 2 - ansi.StringWidth(label)) / 2
		value := strings.Repeat(" ", max(0, padding-2)) + themed(ansi.Cut(marker, max(0, 2-padding), 2), focus, background, o.Dim) + label + current
		value = cellPad(value, g.width-2)
		for row := 0; row < g.itemHeight; row++ {
			y := index*g.itemHeight + row
			lines[y] = themed(strings.Repeat(" ", g.width), foreground, background, o.Dim)
			if popup {
				lines[y] = themed(" "+strings.Repeat(" ", g.width-2)+"│", divider, sidebarBackground, o.Dim)
			}
			line := strings.Repeat(" ", g.width-2)
			if row == g.itemHeight/2 {
				line = value
			}
			putRows(lines, 1, y, themed(line, foreground, background, o.Dim), g.width)
		}
		x, width := 0, g.width
		if popup {
			x, width = 1, g.width-2
		}
		targets = append(targets, hitRegion{ID: id, X: x, Y: index * g.itemHeight, W: width, H: g.itemHeight})
	}
	s := o.Sidebar
	if s == nil {
		s = &sidebarState{}
	}
	s.prepare(o, g, popup, lines)
	for row, line := range strings.Split(s.viewport.View(), "\n") {
		if row >= g.height {
			break
		}
		putRows(rows, 0, g.menuY+row, line, o.Width)
	}
	visible := targets[:0]
	for _, target := range targets {
		target.Y += g.menuY - s.viewport.YOffset()
		if target, ok := clipRegion(target, 0, g.menuY, g.width, g.height); ok {
			visible = append(visible, target)
		}
	}
	if logoTarget != nil {
		visible = append([]hitRegion{*logoTarget}, visible...)
	}
	if !s.viewport.AtTop() {
		putRows(rows, g.width-2, g.menuY-1, themed("↑", muted, sidebarBackground, o.Dim), o.Width)
	}
	if !s.viewport.AtBottom() {
		putRows(rows, g.width-2, g.menuY+g.height, themed("↓", muted, sidebarBackground, o.Dim), o.Width)
	}
	if !popup {
		footer := g.footer(o)
		footerY := g.end - len(footer)
		for row, line := range footer {
			putRows(rows, (g.width-ansi.StringWidth(line))/2, footerY+row, line, o.Width)
		}
	}
	return visible
}

func (g sidebarLayout) footer(o dashboardOptions) []string {
	full, _, color := headerCoreState(o)
	symbol, label, _ := strings.Cut(full, " ")
	status := themed(symbol, color, sidebarBackground, o.Dim) + themed(" "+label, text, sidebarBackground, o.Dim)
	var footer []string
	if g.footerHeight >= 3 {
		footer = append(footer, themed(strings.Repeat("─", g.width-4), divider, sidebarBackground, o.Dim))
	}
	if g.footerHeight >= 5 {
		footer = append(footer, "")
	}
	if g.hasClockRow() {
		if elapsed, ok := o.coreUptime(); ok {
			clock := lipgloss.NewStyle().Bold(true).Render(formatCoreUptime(elapsed))
			footer = append(footer, themed(clock, text, sidebarBackground, o.Dim))
		}
	}
	footer = append(footer, status)
	if g.footerHeight >= 4 {
		footer = append(footer, "")
	}
	return footer
}

func (m *Model) sidebarWheel(msg tea.MouseWheelMsg) bool {
	if m.wizard.open || m.wizard.networkOpen || m.form != "" {
		return false
	}
	o := m.dashboardOptions()
	if !dashboardGeometry(o).sidebar && !m.sidebarOpen {
		return false
	}
	point := msg.Mouse()
	inside := point.X >= 0 && point.X < min(sidebarWidth, m.width)
	if inside && m.sidebar.initialized && point.Y >= m.sidebar.bounds.Y && point.Y < m.sidebar.bounds.Y+m.sidebar.bounds.H {
		m.sidebar.viewport, _ = m.sidebar.viewport.Update(msg)
		m.hover = ""
	}
	// Neither the sidebar's fixed chrome nor a popup's backdrop scrolls the body.
	return inside || m.sidebarOpen
}
