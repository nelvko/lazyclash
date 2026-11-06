package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

type buttonKind uint8

const (
	buttonSecondary buttonKind = iota
	buttonPrimary
	buttonText
	buttonDanger
	buttonDangerConfirm
)

type buttonOptions struct {
	Kind                 buttonKind
	Surface              string
	Focused, Hovered     bool
	Disabled, Busy       bool
	Selected, Selectable bool
	Plain, Dim           bool
}

// renderButton owns appearance only; callers own activation guards and hit
// regions. Bubbles has no button component, so actions share this small view.
// The focus gutter and face padding also bound the monochrome fallback: focus
// and terminal color capability never move adjacent controls.
func renderButton(label string, o buttonOptions) string {
	if o.Surface == "" {
		o.Surface = bg
	}
	foreground, background := text, panel
	if o.Surface != bg && o.Surface != sidebarBackground {
		background = raised
	}
	bold, underline := false, false
	switch o.Kind {
	case buttonPrimary:
		foreground, background, bold = primaryButtonForeground, primaryButtonBackground, true
	case buttonText:
		foreground, background = accent, o.Surface
	case buttonDanger:
		foreground = errorColor
	case buttonDangerConfirm:
		foreground, background, bold = primaryButtonForeground, errorColor, true
	}
	if o.Selectable {
		check := "  "
		if o.Selected {
			check = "✓ "
			foreground, background = selectionForeground, selectionBackground
		}
		label = check + label
	}
	if o.Hovered && !o.Disabled && !o.Busy {
		switch {
		case o.Kind == buttonText:
			underline = true
		case o.Kind == buttonPrimary:
			background = accent
		case o.Kind == buttonDangerConfirm || o.Selected:
			// Preserve the destructive/selected surface's meaning.
		default:
			background = hoverBackground
		}
	}
	if o.Disabled {
		foreground, background, bold = disabled, o.Surface, false
	} else if o.Busy {
		foreground, background, bold = muted, o.Surface, false
	}
	face := " " + label + " "
	if o.Plain && o.Kind != buttonText {
		face = "[" + label + "]"
	}
	face = lipgloss.NewStyle().Bold(bold).Underline(underline).Render(face)
	marker := "  "
	if o.Focused && !o.Disabled {
		marker = "› "
	}
	return themed(marker, focus, o.Surface, o.Dim) + themed(face, foreground, background, o.Dim)
}

func (m Model) buttonPlain() bool {
	return m.noColor || m.colorProfile != colorprofile.Unknown && m.colorProfile <= colorprofile.ANSI
}

func dashboardButtonOptions(id string, o dashboardOptions, primary bool, surface string) buttonOptions {
	kind := buttonSecondary
	if primary {
		kind = buttonPrimary
	}
	return buttonOptions{Kind: kind, Surface: surface, Focused: o.Focus == id, Hovered: o.Hover == id, Plain: o.PlainButtons, Dim: o.Dim}
}
