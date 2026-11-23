package tui

import (
	"strings"

	bubbleskey "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type toggleState uint8

const (
	toggleUnknown toggleState = iota
	toggleOn
	toggleOff
	togglePending
	toggleUnavailable
	toggleUnsupported
)

// toggleModel holds observations supplied by its parent, never a desired value.
// The zero value is unknown and cannot be activated. Bubbles has no toggle
// component; use its key bindings and the existing shared theme/layout helpers.
type toggleModel struct {
	state, lastConfirmed toggleState
	reason               string
}

// setState synchronizes an external observation. lastConfirmed is historical:
// it survives pending/unavailable/unknown observations without making them
// actionable. The parent can present reason beside the control or in details.
func (t *toggleModel) setState(state toggleState, reason string) {
	t.state, t.reason = state, reason
	if state == toggleOn || state == toggleOff {
		t.lastConfirmed = state
	}
}

func (t toggleModel) canToggle() bool {
	return t.state == toggleOn || t.state == toggleOff
}

// toggleIntent is a request, not confirmation that the feature changed.
type toggleIntent struct{ On bool }

var toggleActivateKey = bubbleskey.NewBinding(bubbleskey.WithKeys("enter", "space", " "))

// intent is called only by the parent that currently owns keyboard input,
// after editor/modal/navigation routing. It neither changes state nor starts a
// Cmd. The parent must set pending synchronously before returning its operation
// Cmd, and reject stale results using its existing job identity/generation.
func (t toggleModel) intent(msg tea.KeyPressMsg, focused bool) (toggleIntent, bool) {
	if !focused || !t.canToggle() || msg.IsRepeat || !bubbleskey.Matches(msg, toggleActivateKey) {
		return toggleIntent{}, false
	}
	return toggleIntent{On: t.state == toggleOff}, true
}

type toggleLabels struct {
	On, Off, Pending, Unavailable, Unsupported, Unknown string
}

type toggleOptions struct {
	Labels  toggleLabels
	Surface string
	// The filled switch has a fixed binary legend and reserves a focus gutter.
	// The text presentation uses Labels, Width and RightAligned.
	Filled, Hovered, Busy bool
	// A positive Width is exact. Otherwise all six labels set the natural width.
	Width        int
	RightAligned bool
	Focused, Dim bool
}

func (labels toggleLabels) resolved() toggleLabels {
	label := func(value, fallback string) string {
		// A toggle is one themed terminal row. Caller-provided ANSI must not
		// override semantic colors, and newlines/tabs must not alter geometry.
		value = strings.Join(strings.Fields(ansi.Strip(value)), " ")
		if value == "" {
			return fallback
		}
		return value
	}
	return toggleLabels{
		On: label(labels.On, "已开启"), Off: label(labels.Off, "已关闭"),
		Pending: label(labels.Pending, "切换中…"), Unavailable: label(labels.Unavailable, "不可用"),
		Unsupported: label(labels.Unsupported, "暂不支持"), Unknown: label(labels.Unknown, "状态未知"),
	}
}

func (labels toggleLabels) face(state toggleState) (string, string) {
	switch state {
	case toggleOn:
		return "● " + labels.On, success
	case toggleOff:
		return "○ " + labels.Off, muted
	case togglePending:
		return "◌ " + labels.Pending, accent
	case toggleUnavailable:
		return labels.Unavailable, disabled
	case toggleUnsupported:
		return labels.Unsupported, disabled
	default:
		return labels.Unknown, disabled
	}
}

// renderToggle draws the parent's observation. Rendering never changes the state.
func renderToggle(t toggleModel, o toggleOptions) string {
	if o.Surface == "" {
		o.Surface = bg
	}
	if o.Filled {
		return renderFilledToggle(t, o)
	}
	labels := o.Labels.resolved()
	value, foreground := labels.face(t.state)
	width := o.Width
	if width <= 0 {
		for _, state := range []toggleState{toggleOn, toggleOff, togglePending, toggleUnavailable, toggleUnsupported, toggleUnknown} {
			face, _ := labels.face(state)
			width = max(width, ansi.StringWidth(face))
		}
	}
	value = cellCrop(value, width)
	padding := strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
	if o.Focused {
		// Style the complete span without inserting escapes inside combining
		// characters or ZWJ/flag graphemes.
		value = ansi.NewStyle().Underline(true).UnderlineColor(lipgloss.Color(focus)).Styled(value)
	}
	if o.RightAligned {
		value = padding + value
	} else {
		value += padding
	}
	return themed(value, foreground, o.Surface, o.Dim)
}

func renderFilledToggle(t toggleModel, o toggleOptions) string {
	// The card describes transitional and unavailable states beside the control.
	// Keep their slot and focus marker in place while activation is blocked.
	blocked := o.Busy || !t.canToggle()
	value, foreground, surface := "   —   ", muted, o.Surface
	if !blocked {
		foreground = switchForeground
		if t.state == toggleOn {
			value, surface = " ● ON  ", switchOnBackground
		} else {
			value, surface = " OFF ○ ", switchOffBackground
		}
		if o.Hovered {
			value = lipgloss.NewStyle().Underline(true).Render(value)
		}
	}
	gutter := buttonFocusGutter(buttonOptions{Surface: o.Surface, Focused: o.Focused, Busy: blocked, Dim: o.Dim})
	return gutter + themed(value, foreground, surface, o.Dim)
}
