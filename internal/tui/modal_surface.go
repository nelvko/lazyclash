package tui

import (
	"slices"
	"strings"
)

// modalSurface paints a centered window over the dashboard's existing backdrop.
// Content and hit regions use the same local cell coordinates in every palette.
type modalSurface struct {
	bounds       hitRegion
	viewW, viewH int
	background   string
	rows         []string
}

func (m *Model) newModalSurface(width, height int, background string) modalSurface {
	width, height = max(0, min(width, m.width)), max(0, min(height, m.height))
	s := modalSurface{
		bounds: hitRegion{X: (m.width - width) / 2, Y: (m.height - height) / 2, W: width, H: height},
		viewW:  m.width, viewH: m.height, background: background,
	}
	// When the palette collapses the surface and backdrop to the same color,
	// use the outer padding cells for a thin outline.
	if m.lowColor() && (width < m.width || height < m.height) && width >= 2 && height >= 2 {
		inside := strings.Repeat(" ", width-2)
		edge := strings.Repeat("─", width-2)
		s.rows = slices.Repeat([]string{themed("│"+inside+"│", border, background, false)}, height)
		s.rows[0] = themed("┌"+edge+"┐", border, background, false)
		s.rows[height-1] = themed("└"+edge+"┘", border, background, false)
	} else {
		blank := themed(strings.Repeat(" ", width), text, background, false)
		s.rows = slices.Repeat([]string{blank}, height)
	}
	return s
}

func (s modalSurface) put(x, y int, line string) {
	putRows(s.rows, x, y, themed(line, text, s.background, false), s.bounds.W)
}

func (s modalSurface) screen(hit hitRegion) hitRegion {
	hit.X += s.bounds.X
	hit.Y += s.bounds.Y
	return hit
}

func (s modalSurface) render(base string) string {
	if s.viewW <= 0 || s.viewH <= 0 {
		return ""
	}
	box := strings.Join(s.rows, "\n")
	if s.bounds.W == s.viewW && s.bounds.H == s.viewH {
		return box
	}
	return overlay(base, box, s.bounds.X, s.bounds.Y, s.viewW, s.viewH)
}
