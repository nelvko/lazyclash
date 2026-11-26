package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// A dropdown describes a single-choice field. Its trigger and popup use the
// same options, value and cell widths. actionMenu owns transient navigation.
type dropdown struct {
	Items       []menuItem
	Value       string
	Placeholder string
	CenterPopup bool
	ShowCursor  bool
}

const dropdownGutter = 2

func (d dropdown) label() string {
	for _, item := range d.Items {
		if item.ID == d.Value {
			return clean(item.Label)
		}
	}
	if d.Placeholder != "" {
		return clean(d.Placeholder)
	}
	return clean(d.Value)
}

// Both faces reserve two columns on each side, a gap, and the chevron/check.
// The trigger's external keyboard-focus gutter is separate.
func (d dropdown) width() int {
	w := max(ansi.StringWidth(clean(d.Placeholder)), ansi.StringWidth(d.label()))
	for _, item := range d.Items {
		w = max(w, ansi.StringWidth(item.label()))
	}
	if d.ShowCursor {
		w++ // A space separates the visible cursor from the option label.
	}
	return min(48, w+6)
}

func (d dropdown) render(id string, o buttonOptions, expanded bool, width int) (string, hitRegion) {
	ideal := d.width()
	total := ideal + dropdownGutter
	if width > 0 {
		total = min(width, total)
	}
	arrow := "▾"
	if expanded {
		arrow = "▴"
		o.Focused, o.Hovered = false, false
	}
	o.Kind, o.Compact, o.Width = buttonSecondary, false, 0
	faceWidth := total - dropdownGutter
	var value string
	if faceWidth < 3 {
		// Even a single visible cell should still advertise the selector.
		value = themed(cellPad(arrow, total), text, o.Surface, o.Dim)
	} else if faceWidth == 3 {
		value = renderButton(arrow, o)
	} else {
		// Reclaim decorative padding before truncating a value on a narrow row.
		pad := max(0, 2-(ideal-faceWidth))
		label := strings.Repeat(" ", pad/2) + cellPad(d.label(), faceWidth-4-pad) + " " + arrow + strings.Repeat(" ", pad-pad/2)
		value = renderButton(label, o)
	}
	hit := hitRegion{}
	if !o.Disabled && !o.Busy {
		hit = hitRegion{ID: id, W: ansi.StringWidth(value), H: 1}
	}
	return value, hit
}

func (d dropdown) show(menu *actionMenu, anchor hitRegion) {
	*menu = actionMenu{open: len(d.Items) > 0, selected: -1}
	d.sync(menu)
	menu.setAnchor(anchor)
	for i, item := range menu.items {
		if item.Checked && item.DisabledReason == "" {
			menu.selected = i
			break
		}
	}
}

// Refresh the checked value without moving a user browsing another option.
func (d dropdown) sync(menu *actionMenu) {
	menu.setItems(d.Items)
	menu.dropdown = &d
	for i := range menu.items {
		menu.items[i].Checked = menu.items[i].ID == d.Value
	}
}

func (m actionMenu) dropdownOpen(id string) bool {
	return m.open && m.dropdown != nil && m.anchor.ID == id
}

func (d dropdown) popupGeometry(anchor hitRegion, screenWidth int) (x, width int) {
	width = min(screenWidth, d.width())
	// Regular trigger hits include a two-cell gutter. For a cropped trigger
	// leave at least one visible cell as the point of attachment.
	x = anchor.X + min(dropdownGutter, max(0, anchor.W-1))
	if d.CenterPopup {
		x = (2*anchor.X + anchor.W - width) / 2
	} else if anchor.DropdownRight {
		x = anchor.X + anchor.W - width
	}
	return max(0, min(x, screenWidth-width)), width
}

// Wrapped options share one highlight, with cursor and checkmark on their first line.
func (d dropdown) row(item menuItem, width int, active, monochrome bool, line int) string {
	foreground, background := text, raised
	cursor := " "
	if item.DisabledReason != "" {
		foreground = disabled
	} else if active {
		background = selectionBackground
		if line == 0 && d.ShowCursor {
			cursor = themed("›", focus, background, false)
		} else if line == 0 && monochrome {
			cursor = "›"
		}
	}
	check := " "
	if item.Checked && line == 0 {
		check = "✓"
	}
	// Drop optional padding first when the terminal cannot fit a framed row.
	gap := ""
	if d.ShowCursor && width >= 4 {
		gap = " "
	}
	var row string
	switch {
	case width >= 8:
		row = cursor + gap + cellPad(item.label(), width-4-len(gap)) + " " + check + " "
	case width >= 2:
		row = cursor + gap + cellPad(item.label(), width-2-len(gap)) + check
	case active && (d.ShowCursor || monochrome):
		row = cursor
	case item.Checked:
		row = check
	default:
		row = cellPad(item.label(), width)
	}
	return themed(row, foreground, background, false)
}
