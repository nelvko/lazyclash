package tui

import (
	"cmp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type buttonKind uint8

const (
	buttonSecondary buttonKind = iota
	buttonPrimary
	buttonText
	buttonIcon
	buttonDanger
	buttonDangerConfirm
	buttonSegment
	buttonRoutingSegment
)

type buttonOptions struct {
	Kind                 buttonKind
	Surface              string
	TextColor            string // Optional foreground for text actions on colored surfaces.
	TextHoverBackground  string // Text buttons can highlight their face on hover in place of an underline.
	Focused, Hovered     bool
	Disabled, Busy       bool
	Selected, Selectable bool
	Plain, Dim           bool
	Compact, AlignLeft   bool
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
	case buttonSecondary:
		background = buttonSecondaryBackground
	case buttonPrimary:
		foreground, background, bold = primaryButtonForeground, primaryButtonBackground, true
	case buttonText, buttonIcon:
		foreground, background = cmp.Or(o.TextColor, accent), o.Surface
	case buttonDanger:
		foreground = buttonDangerForeground
	case buttonDangerConfirm:
		foreground, background, bold = primaryButtonForeground, errorColor, true
	}
	selected := o.Selectable && o.Selected
	selectedText, selectedBG, selectedHover := text, buttonSelectedBackground, buttonSelectedHoverBackground
	markerColor := focus
	if o.Kind == buttonRoutingSegment {
		foreground, background = routingIdleText, routingIdleBackground
		selectedText, selectedBG, selectedHover = routingSelectedText, routingSelectedBackground, routingSelectedHoverBackground
		markerColor = routingFocus
	}
	if selected {
		foreground, background, bold = selectedText, selectedBG, true
	}
	hovered := o.Hovered && !o.Disabled && !o.Busy
	if hovered {
		switch {
		case selected:
			background = selectedHover
		case o.Kind == buttonSecondary:
			background = buttonSecondaryHoverBackground
		case o.Kind == buttonText:
			background = cmp.Or(o.TextHoverBackground, o.Surface)
			underline = o.TextHoverBackground == ""
		case o.Kind == buttonIcon:
			foreground, background = text, buttonHoverBackground
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
	flat := o.Kind == buttonText || o.Kind == buttonIcon
	if (o.Disabled || o.Busy) && !selected {
		foreground, background, bold = muted, neutral, false
		if flat {
			background = o.Surface
		}
		if o.Disabled {
			foreground = disabled
		}
	}
	width := max(ansi.StringWidth(label), o.Width)
	padding := width - ansi.StringWidth(label)
	leftPadding := padding / 2
	if o.AlignLeft {
		leftPadding = 0
	}
	label = strings.Repeat(" ", leftPadding) + label + strings.Repeat(" ", padding-leftPadding)
	if o.Kind == buttonSegment || o.Kind == buttonRoutingSegment {
		// Keep both cues inside the face so adjacent modes share a continuous
		// surface. The right cell distinguishes selection from keyboard focus.
		left, right := " ", " "
		if o.Focused && !o.Disabled {
			left = themed("›", markerColor, background, o.Dim)
		}
		if o.Plain && selected {
			right = "●"
		}
		return themed(left+lipgloss.NewStyle().Bold(bold).Render(label)+right, foreground, background, o.Dim)
	}
	face := " " + label + " "
	if o.Plain && (!flat || o.Selectable || (o.Kind == buttonIcon && hovered)) {
		face = "[" + label + "]"
		if selected {
			face = "<" + label + ">"
		}
	}
	face = lipgloss.NewStyle().Bold(bold).Underline(underline).Render(face)
	marker := "  "
	if o.Focused && !o.Disabled {
		marker = " ›"
	}
	if o.Compact {
		marker = marker[1:]
	}
	if o.Busy {
		markerColor = muted
	}
	return themed(marker, markerColor, o.Surface, o.Dim) + themed(face, foreground, background, o.Dim)
}

func (m *Model) buttonPlain() bool {
	return m.lowColor()
}

func dashboardButtonOptions(id string, o dashboardOptions, primary bool, surface string) buttonOptions {
	kind := buttonSecondary
	if primary {
		kind = buttonPrimary
	}
	return buttonOptions{Kind: kind, Surface: surface, Focused: o.Focus == id, Hovered: o.Hover == id, Plain: o.PlainButtons, Dim: o.Dim}
}
