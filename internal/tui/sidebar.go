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
	width, menuY, end, height, itemHeight int
}

func sidebarGeometry(o dashboardOptions, popup bool) sidebarLayout {
	g := dashboardGeometry(o)
	s := sidebarLayout{width: min(sidebarWidth, o.Width), menuY: g.headerHeight, end: dashboardContentBottom(o), itemHeight: 3}
	if popup {
		s.menuY += 2
	}
	available := max(0, s.end-s.menuY)
	// Extremely small popups still need room for an item and an overflow hint.
	if available < 4 {
		s.itemHeight = 1
	}
	total := len(navPages()) * s.itemHeight
	if total > available {
		available = max(0, available-1)
	}
	s.height = min(total, available/s.itemHeight*s.itemHeight)
	return s
}

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
		start = header.headerHeight + 1
	}
	for y := start; y < g.end; y++ {
		putRows(rows, 0, y, themed(strings.Repeat(" ", g.width), text, sidebarBackground, o.Dim), o.Width)
		if popup {
			putRows(rows, g.width-1, y, themed("│", divider, sidebarBackground, o.Dim), o.Width)
		}
	}
	if !popup {
		logoLine := lipgloss.NewStyle().Width(g.width - 1).Align(lipgloss.Center).Render(logoTextFrame(o.LogoFrame, o.LogoIntroFrame, sidebarBackground, o.Dim))
		putRows(rows, 0, header.headerY, themed(logoLine, accent, sidebarBackground, o.Dim), o.Width)
		frame := logoFrameAt(o.LogoFrame)
		if header.headerY > 0 && frame.bubble != "" {
			x := (g.width-1-ansi.StringWidth(logo))/2 + ansi.StringWidth("la") + frame.offset
			color := logoGradientColor(ansi.StringWidth("la")+frame.offset, 0)
			if frame.dim {
				color = disabled
			}
			putRows(rows, x, header.headerY-1, themed(frame.bubble, color, sidebarBackground, o.Dim), o.Width)
		}
		hit := hitRegion{ID: "logo", X: 0, Y: header.headerY, W: g.width - 1, H: 1}
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
		footer := sidebarTraffic(o, g.width)
		footerY := g.end - len(footer) - 1
		if len(lines) <= g.height && footerY >= g.menuY+g.height+1 {
			for row, line := range footer {
				putRows(rows, 2, footerY+row, line, o.Width)
			}
		}
	}
	return visible
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
