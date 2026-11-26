package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	tooltipIcon   = "ⓘ"
	tooltipPrefix = "tooltip:"
)

// The owner is the control's focus ID. The anchor can be its icon or the action itself.
type tooltipBinding struct {
	owner, text string
}

func (b tooltipBinding) id() string { return tooltipPrefix + b.owner }

func (b tooltipBinding) label(label string, o dashboardOptions, surface string, width int) dashboardBlock {
	lines := strings.Split(lipgloss.Wrap(label+" "+tooltipIcon, max(1, width), "/"), "\n")
	last := len(lines) - 1
	iconWidth := ansi.StringWidth(tooltipIcon)
	x := ansi.StringWidth(lines[last]) - iconWidth
	iconColor := muted
	if o.Hover == b.id() {
		iconColor = accent
	}
	for y, line := range lines {
		if y == last {
			line = ansi.Cut(line, 0, x) + themed(tooltipIcon, iconColor, surface, o.Dim)
		}
		lines[y] = themed(line, text, surface, o.Dim)
	}
	block := dashboardBlock{lines: lines}
	// Only the screen layout collects pointer targets. Keyboard navigation
	// measures the same labels without adding a stop for the icon.
	if o.tooltips != nil {
		block.targets = []hitRegion{{ID: b.id(), tip: b, X: x, Y: last, W: iconWidth, H: 1}}
	}
	return block
}

type tooltipDelayMsg uint64

type tooltipState struct {
	anchors    []hitRegion // Visible icons in the current interactive layer.
	offscreen  hitRegion   // Focused icon outside the viewport, in body coordinates.
	pointer    hitRegion   // Its position must still match when the timer fires.
	generation tooltipDelayMsg
	open       string // Icon ID, for both keyboard and mouse.
	keyboard   bool
	bounds     hitRegion
}

func tooltipAnchor(targets []hitRegion, id string) hitRegion {
	for _, hit := range targets {
		if hit.ID == id && hit.tip.text != "" {
			return hit
		}
	}
	return hitRegion{}
}

func (t *tooltipState) focused(focus string) hitRegion {
	if t.offscreen.ID != "" && t.offscreen.tip.owner == focus {
		return t.offscreen
	}
	for _, hit := range t.anchors {
		if hit.tip.owner == focus {
			return hit
		}
	}
	return hitRegion{}
}

func (t *tooltipState) dismiss() {
	t.open, t.keyboard, t.pointer, t.bounds = "", false, hitRegion{}, hitRegion{}
}

// A deliberate hover over a different icon can replace a keyboard tooltip.
// Passing over it briefly leaves the keyboard tooltip open.
func (t *tooltipState) motion(hover string) tea.Cmd {
	hit := tooltipAnchor(t.anchors, hover)
	if t.pointer == hit {
		return nil
	}
	t.pointer = hit
	t.generation++
	if !t.keyboard {
		t.open = ""
	}
	if hit.ID == "" || hit.ID == t.open {
		return nil
	}
	id := t.generation
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return id })
}

func (t *tooltipState) showAfterDelay(id tooltipDelayMsg) bool {
	if id != t.generation || t.pointer.ID == "" || t.pointer.ID == t.open {
		return false
	}
	t.open, t.keyboard = t.pointer.ID, false
	return true
}

func (t *tooltipState) key(key, focus string) bool {
	switch key {
	case "esc":
		if t.open == "" {
			return false
		}
	case "?":
		hit := t.focused(focus)
		if hit.ID == "" {
			return false
		}
		if t.open != hit.ID {
			t.dismiss()
			t.open, t.keyboard = hit.ID, true
			return true
		}
	default:
		return false
	}
	t.dismiss()
	return true
}

func (t *tooltipState) sync(targets []hitRegion, focus, hover string) {
	for _, hit := range targets {
		if hit.tip.text != "" {
			t.anchors = append(t.anchors, hit)
		}
	}
	if t.pointer.ID != "" && (hover != t.pointer.ID || tooltipAnchor(t.anchors, t.pointer.ID) != t.pointer) {
		t.pointer = hitRegion{}
	}
	open := tooltipAnchor(t.anchors, t.open)
	if open.ID == "" || t.keyboard && open.tip.owner != focus || !t.keyboard && open.ID != t.pointer.ID {
		t.open, t.keyboard, t.bounds = "", false, hitRegion{}
	}
}
