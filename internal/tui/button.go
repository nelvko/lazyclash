package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
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
	// Width is the minimum label width in terminal cells, excluding padding
	// and the focus gutter. Selectable peers pass their longest label width.
	Width int
}

// renderButton owns appearance only; callers own activation guards and hit
// regions. Bubbles has no button component, so actions share this small view.
// The focus gutter and face padding also bound the monochrome fallback: focus
// and terminal color capability never move adjacent controls.
func renderButton(label string, o buttonOptions) string {
	if o.Surface == "" {
		o.Surface = bg
	}
	neutral := raised
	if o.Surface == raised {
		neutral = panel
	}
	foreground, background := text, neutral
	bold, underline := false, false
	switch o.Kind {
	case buttonPrimary:
		foreground, background, bold = primaryButtonForeground, primaryButtonBackground, true
	case buttonText:
		foreground, background = accent, o.Surface
	case buttonDanger:
		foreground = buttonDangerForeground
	case buttonDangerConfirm:
		foreground, background, bold = primaryButtonForeground, errorColor, true
	}
	selected := o.Selectable && o.Selected
	if selected {
		foreground, background, bold = text, buttonSelectedBackground, true
	}
	if o.Hovered && !o.Disabled && !o.Busy {
		switch {
		case selected:
			background = buttonSelectedHoverBackground
		case o.Kind == buttonText:
			underline = true
		case o.Kind == buttonPrimary:
			background = buttonPrimaryHoverBackground
		case o.Kind == buttonDanger:
			background = buttonDangerHoverBackground
		case o.Kind == buttonDangerConfirm:
			background = buttonDangerForeground
		default:
			background = buttonHoverBackground
		}
	}
	if (o.Disabled || o.Busy) && !selected {
		foreground, background, bold = muted, neutral, false
		if o.Kind == buttonText {
			background = o.Surface
		}
		if o.Disabled {
			foreground = disabled
		}
	}
	width := max(ansi.StringWidth(label), o.Width)
	padding := width - ansi.StringWidth(label)
	label = strings.Repeat(" ", padding/2) + label + strings.Repeat(" ", padding-padding/2)
	face := " " + label + " "
	if o.Plain && (o.Kind != buttonText || o.Selectable) {
		face = "[" + label + "]"
		if selected {
			face = "<" + label + ">"
		}
	}
	face = lipgloss.NewStyle().Bold(bold).Underline(underline).Render(face)
	marker := "  "
	markerColor := focus
	if o.Focused && !o.Disabled {
		marker = " ›"
	}
	if o.Busy {
		markerColor = muted
	}
	return themed(marker, markerColor, o.Surface, o.Dim) + themed(face, foreground, background, o.Dim)
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
