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
	return min(48, d.naturalWidth(d.longestLabelWidth()))
}

func (d dropdown) naturalWidth(w int) int {
	if d.ShowCursor {
		w++ // A space separates the visible cursor from the option label.
	}
	return w + 6
}

func (d dropdown) longestLabelWidth() int {
	w := ansi.StringWidth(clean(d.Placeholder))
	found := false
	for _, item := range d.Items {
		w = max(w, ansi.StringWidth(item.label()))
		found = found || item.ID == d.Value
	}
	if !found && d.Placeholder == "" {
		w = max(w, ansi.StringWidth(clean(d.Value)))
	}
	return w
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

func (d dropdown) popupGeometry(anchor hitRegion, screenWidth int) (x, width, longest int) {
	longest = d.longestLabelWidth()
	width = min(screenWidth, d.naturalWidth(longest))
	// Regular trigger hits include a two-cell gutter. For a cropped trigger
	// leave at least one visible cell as the point of attachment.
	x = anchor.X + min(dropdownGutter, max(0, anchor.W-1))
	if d.CenterPopup {
		x = (2*anchor.X + anchor.W - width) / 2
	} else if anchor.DropdownRight {
		x = anchor.X + anchor.W - width
	}
	return max(0, min(x, screenWidth-width)), width, longest
}

// Long values have a separate reading area. The option list keeps one physical
// row per item, so hovering a long value never inserts rows under the pointer.
type dropdownDetail struct {
	id                string
	offset, maxOffset int
	y                 int
	lines             []string
	inline, divider   bool
}

func (p *dropdownDetail) scroll(delta int) {
	p.offset = max(0, min(p.maxOffset, p.offset+delta))
}

func (p *dropdownDetail) contains(y int) bool {
	return y >= p.y && y < p.y+len(p.lines)
}

func (d dropdown) labelColumns(inner int) int {
	padding := 2
	if inner >= 8 {
		padding = 4
	}
	if d.ShowCursor && inner >= 4 {
		padding++
	}
	return max(0, inner-padding)
}

// Cap the reserved reading area to about half the menu when there are many
// choices. A short option needs no reading area, but keeps its list position.
func (m *actionMenu) layoutDetail(inner, height, longest int) int {
	listRows := min(len(m.items), height)
	if m.dropdown == nil || longest <= m.dropdown.labelColumns(inner) || m.selected < 0 {
		m.detail = dropdownDetail{}
		return listRows
	}
	width := max(1, inner)
	longestLines := (longest + width - 1) / width
	listRows = min(listRows, max(1, height/2, height-longestLines-1))
	item := m.items[m.selected]
	label := item.label()
	if ansi.StringWidth(label) <= m.dropdown.labelColumns(inner) {
		m.detail = dropdownDetail{}
		return listRows
	}
	if m.detail.id != item.ID {
		m.detail = dropdownDetail{id: item.ID}
	}
	m.detail.inline = listRows == height
	if m.detail.inline && m.dropdown.labelColumns(inner) >= 2 {
		width = m.dropdown.labelColumns(inner)
	}
	lines := strings.Split(ansi.Hardwrap(label, width, true), "\n")
	// With one usable row, page through the full name in the selected row.
	room := max(1, height-listRows)
	m.detail.divider = room > 1
	if m.detail.divider {
		room--
	}
	visible := min(len(lines), room)
	m.detail.maxOffset = len(lines) - visible
	m.detail.scroll(0)
	m.detail.lines = lines[m.detail.offset : m.detail.offset+visible]
	return listRows
}

// The list stays on the anchor side of the separator when the popup flips above.
func (p dropdownDetail) block(inner int, above bool) []string {
	if p.inline || len(p.lines) == 0 {
		return nil
	}
	var rows []string
	divider := ""
	if p.divider {
		legend := ""
		if p.maxOffset > 0 && inner >= 11 {
			legend = " PgUp/PgDn "
		}
		divider = themed(legend+strings.Repeat("─", inner-len(legend)), border, raised, false)
		if !above {
			rows = append(rows, divider)
		}
	}
	for _, line := range p.lines {
		rows = append(rows, themed(cellPad(line, inner), text, raised, false))
	}
	if above && p.divider {
		rows = append(rows, divider)
	}
	return rows
}

// Wrapped options share one highlight, with cursor and checkmark on their first line.
func (d dropdown) row(item menuItem, width int, active, showCursor bool, line int) string {
	foreground, background := text, raised
	cursor := " "
	if item.DisabledReason != "" {
		foreground = disabled
	} else if active {
		background = selectionBackground
		if line == 0 && d.ShowCursor {
			cursor = themed("›", focus, background, false)
		} else if line == 0 && showCursor {
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
		row = cursor + gap + cellPad(item.label(), d.labelColumns(width)) + " " + check + " "
	case width >= 2:
		row = cursor + gap + cellPad(item.label(), d.labelColumns(width)) + check
	case active && (d.ShowCursor || showCursor):
		row = cursor
	case item.Checked:
		row = check
	default:
		row = cellPad(item.label(), width)
	}
	return themed(row, foreground, background, false)
}
