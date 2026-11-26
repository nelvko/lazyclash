package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	modalBackground     = raised
	inspectorBackground = panel
	backdropText        = disabled
	backdropBackground  = sidebarBackground
	modalPaddingX       = 2
	modalBodyInset      = modalPaddingX + 1 // Leave room beside a scrolling document.
)

// Flatten the dashboard's nested styles to an opaque backdrop before compositing the dialog.
func modalBackdrop(screen string) string {
	return themed(ansi.Strip(screen), backdropText, backdropBackground, false)
}

func (m *Model) dashboardDimmed() bool {
	return m.feedbackDialogOpen() || m.providers.open || (m.page == "connections" && m.connections.confirming)
}

// Clipping half of a wide character leaves a plain space. Give it the same
// background as the layer beneath the dialog when the final view is painted.
func (m *Model) canvasBackground() string {
	if m.dashboardDimmed() {
		return backdropBackground
	}
	return bg
}

// modalSurface paints a dialog over the existing dashboard.
// Content and hit regions use the same local cell coordinates in every palette.
type modalSurface struct {
	bounds       hitRegion
	viewW, viewH int
	background   string
	rows         []string
}

// Large documents switch to a full-screen dialog on narrow or short terminals.
func (m *Model) canFloatDocument() bool {
	return m.width >= 110 && m.height >= 30
}

func (m *Model) newModalSurface(width, height int) modalSurface {
	width, height = max(0, min(width, m.width)), max(0, min(height, m.height))
	bounds := hitRegion{X: (m.width - width) / 2, Y: (m.height - height) / 2, W: width, H: height}
	outline := m.lowColor() && (width < m.width || height < m.height)
	return m.modalSurfaceAt(bounds, modalBackground, outline)
}

// The inspector floats against undimmed content and needs a visible edge in every palette.
func (m *Model) newInspectorSurface(bounds hitRegion) modalSurface {
	return m.modalSurfaceAt(bounds, inspectorBackground, true)
}

func (m *Model) renderModalNotice(message string) (string, []hitRegion) {
	s := m.newModalSurface(m.width, m.height)
	s.put(0, 0, cellCrop(message, m.width))
	return s.render(""), nil
}

func (m *Model) modalSurfaceAt(bounds hitRegion, background string, outline bool) modalSurface {
	width, height := bounds.W, bounds.H
	s := modalSurface{
		bounds: bounds,
		viewW:  m.width, viewH: m.height, background: background,
	}
	// Reuse the outer padding cells so toggling the outline never moves content.
	if outline && width >= 2 && height >= 2 {
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

func (s modalSurface) divider(y int) {
	line := strings.Repeat("─", max(0, s.bounds.W-2*modalPaddingX))
	s.put(modalPaddingX, y, themed(line, divider, s.background, false))
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
