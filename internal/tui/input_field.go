package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	inputFieldMaxWidth  = 60
	inputImportMaxWidth = 72
)

// inputFieldOptions describes the presentation around a Bubbles editor. The
// caller retains editing, validation and submission; the field owns its cell
// geometry, frame, optional clear action and trailing search controls.
type inputFieldOptions struct {
	ID, ClearID, Surface       string
	Width                      int
	Focused, Hovered, Invalid  bool
	Disabled                   bool
	ClearFocused, ClearHovered bool
	Trailing                   dashboardBlock
}

// Bubbles reserves another cell outside Width for its cursor. Keep this cell
// even with a real terminal cursor, so resizing and wide text share one viewport.
func resizeInputField(input *textinput.Model, width int) {
	width = max(1, width)
	position := input.Position()
	if input.Width() != width {
		input.SetWidth(width)
		input.CursorEnd()
	}
	input.SetCursor(position)
}

func prepareInputField(input textinput.Model, textWidth int, surface string, invalid bool) textinput.Model {
	input.Prompt = ""
	resizeInputField(&input, textWidth)
	styleInput(&input, surface, invalid)
	input.SetVirtualCursor(false)
	styles := input.Styles()
	styles.Cursor.Shape = tea.CursorBar
	input.SetStyles(styles)
	return input
}

func inputFieldClearWidth(width int, clear bool) int {
	if clear && width >= 12 {
		return 2
	}
	return 0
}

func inputFieldInlineTrailing(width, trailingWidth int, clear bool) bool {
	// Retain at least eight text cells before placing controls on the text row.
	return trailingWidth > 0 && width >= 4+1+8+inputFieldClearWidth(width, clear)+inputFieldSeparatorWidth(width, clear)+trailingWidth
}

func inputFieldSeparatorWidth(width int, clear bool) int {
	if inputFieldClearWidth(width, clear) > 0 {
		return 2
	}
	return 0
}

func inputFieldTextWidth(width, trailingWidth int, clear bool) int {
	available := width - 5 - inputFieldClearWidth(width, clear)
	if inputFieldInlineTrailing(width, trailingWidth, clear) {
		available -= trailingWidth + inputFieldSeparatorWidth(width, clear)
	}
	return max(1, available)
}

func inputFieldRows(width, trailingWidth int, clear bool) int {
	if trailingWidth > 0 && !inputFieldInlineTrailing(width, trailingWidth, clear) {
		return 4
	}
	return 3
}

func renderInputField(input textinput.Model, o inputFieldOptions) dashboardBlock {
	width, surface := max(1, o.Width), o.Surface
	if surface == "" {
		surface = bg
	}
	trailing := ""
	if len(o.Trailing.lines) > 0 {
		trailing = o.Trailing.lines[0]
	}
	trailingWidth := ansi.StringWidth(trailing)
	inline := inputFieldInlineTrailing(width, trailingWidth, o.ClearID != "")
	textWidth := inputFieldTextWidth(width, trailingWidth, o.ClearID != "")
	input = prepareInputField(input, textWidth, surface, o.Invalid)
	value := themed(cellPad(strings.ReplaceAll(input.View(), "\x00", ""), textWidth+1), text, surface, false)
	content := value
	var targets []hitRegion
	clearWidth := inputFieldClearWidth(width, o.ClearID != "")

	if clearWidth > 0 {
		clear := strings.Repeat(" ", clearWidth)
		if input.Value() != "" && !o.Disabled {
			label, color := " x", muted
			if o.ClearFocused {
				label, color = "›x", focus
			} else if o.ClearHovered {
				color = text
			}
			clear = themed(label, color, surface, false)
			targets = append(targets, hitRegion{ID: o.ClearID, X: 2 + ansi.StringWidth(content), Y: 1, W: clearWidth, H: 1})
		}
		content += clear
	}
	if inline {
		if clearWidth > 0 {
			separator := "  "
			if input.Value() != "" && !o.Disabled {
				separator = " │"
			}
			content += themed(separator, divider, surface, false)
		}
		x := 2 + ansi.StringWidth(content)
		content += trailing
		if !o.Disabled {
			for _, target := range o.Trailing.targets {
				target.X += x
				target.Y++
				targets = append(targets, target)
			}
		}
	}
	edge := border
	if o.Hovered {
		edge = text
	}
	if o.Focused || input.Focused() || o.ClearFocused {
		edge = accent
	}
	if o.Invalid {
		edge = errorColor
	}
	b := dashboardBlock{lines: inputFrame([]string{content}, width, edge, surface), targets: targets}
	if width < 5 {
		b.lines = []string{"", cellCrop(value, width), ""}
	}
	if o.ID != "" {
		hit := hitRegion{ID: o.ID, W: width, H: 3}
		if width >= 5 {
			hit.InputWidth = textWidth
		}
		// Inner controls precede the full frame, so clicking them cannot begin
		// editing or clear text through the outer field's hit region.
		b.targets = append(b.targets, hit)
	}
	if trailingWidth > 0 && !inline {
		x := max(0, width-trailingWidth)
		b.lines = append(b.lines, themed(cellPad(strings.Repeat(" ", x)+trailing, width), text, surface, false))
		if !o.Disabled {
			for _, target := range o.Trailing.targets {
				target.X += x
				target.Y += 3
				if clipped, ok := clipRegion(target, 0, 3, width, 1); ok {
					b.targets = append(b.targets, clipped)
				}
			}
		}
	}
	return b
}
