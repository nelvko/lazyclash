package tui

import (
	"cmp"
	"strings"
	"time"

	bubbleskey "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
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

// Observations and animation frames never represent a requested business state.
// The zero value is unknown and cannot be activated.
type toggleModel struct {
	state    toggleState
	reason   string
	sliding  bool
	position int
}

func (t *toggleModel) setState(state toggleState, reason string) {
	t.state, t.reason = state, reason
}

func (t toggleModel) canToggle() bool {
	return t.state == toggleOn || t.state == toggleOff
}

type toggleIntent struct{ On bool }

var toggleActivateKey = bubbleskey.NewBinding(bubbleskey.WithKeys("enter", "space", " "))

func (t toggleModel) intent(msg tea.KeyPressMsg, focused bool) (toggleIntent, bool) {
	if !focused || !t.canToggle() || msg.IsRepeat || !bubbleskey.Matches(msg, toggleActivateKey) {
		return toggleIntent{}, false
	}
	return toggleIntent{On: t.state == toggleOff}, true
}

type toggleLabels struct{ Unavailable, Unsupported, Unknown string }

type toggleOptions struct {
	Style, Surface                    string
	Labels                            toggleLabels
	Focused, Hovered, Monochrome, Dim bool
}

const (
	switchWidth      = 3 // Compact faces and logical animation positions.
	sliderThumbWidth = 2
	switchSlotWidth  = switchWidth * sliderThumbWidth
)

// U+3007 is a single two-cell ring, so the thumb stays intact in fonts
// that leave side bearings around half circles.
var slantedFrames = [3]string{"〇╱╱╱╱", "◢◤〇╱╱", "◢◤◢◤〇"}
var lineFrames = [3]string{"●──", "━●─", "━━●"}

// Check every frame using the same width policy as the enclosing layout.
// x/ansi also honors RUNEWIDTH_EASTASIAN at process startup.
func effectiveSwitchStyle(style string, monochrome bool) string {
	style = app.NormalizeSwitchStyle(style)
	var frames [3]string
	frameWidth := switchWidth
	switch style {
	case app.SwitchStyleSlanted:
		frames = slantedFrames
		frameWidth = switchSlotWidth
	case app.SwitchStyleLine:
		frames = lineFrames
	case app.SwitchStyleSlider:
		if monochrome {
			return app.SwitchStyleText
		}
		return style
	default:
		return style
	}
	for _, frame := range frames {
		if ansi.StringWidth(frame) != frameWidth {
			return app.SwitchStyleText
		}
	}
	return style
}

func switchEndpoint(state toggleState) int {
	if state == toggleOn {
		return switchWidth - 1
	}
	return 0
}

func renderToggle(t toggleModel, o toggleOptions) string {
	surface := cmp.Or(o.Surface, bg)
	if !t.canToggle() {
		var label string
		switch t.state {
		case togglePending:
			return themed("...", accent, surface, o.Dim)
		case toggleUnavailable:
			label = cmp.Or(o.Labels.Unavailable, "暂不可用")
		case toggleUnsupported:
			label = cmp.Or(o.Labels.Unsupported, "暂不支持")
		default:
			label = cmp.Or(o.Labels.Unknown, "状态未知")
		}
		return themed(strings.Join(strings.Fields(ansi.Strip(label)), " "), disabled, surface, o.Dim)
	}
	// A dimmed modal backdrop strips nested styles, including slider backgrounds.
	style := effectiveSwitchStyle(o.Style, o.Monochrome || o.Dim)
	position := switchEndpoint(t.state)
	if t.sliding && style != app.SwitchStyleText {
		position = t.position
	}
	thumb, track := muted, divider
	if position > 0 {
		thumb = success
		if style == app.SwitchStyleSlider {
			thumb, track = switchOnThumb, switchOnTrack
		}
	}
	span := func(value, background string) string {
		if o.Dim {
			return themed(value, thumb, surface, true)
		}
		s := ansi.NewStyle().ForegroundColor(lipgloss.Color(thumb))
		if background != "" {
			s = s.BackgroundColor(lipgloss.Color(background))
		}
		if o.Focused {
			s = s.Underline(true).UnderlineColor(lipgloss.Color(focus))
		} else if o.Hovered {
			s = s.Underline(true)
		}
		return s.Styled(value)
	}
	switch style {
	case app.SwitchStyleSlider:
		var face strings.Builder
		segment := strings.Repeat(" ", sliderThumbWidth)
		for cell := range switchWidth {
			background := track
			if cell == position {
				background = thumb
			}
			face.WriteString(span(segment, background))
		}
		return face.String()
	case app.SwitchStyleText:
		label := "OFF"
		if t.state == toggleOn {
			label = " ON"
		}
		background := raised
		if surface == raised {
			background = panel
		}
		return span(label, background)
	case app.SwitchStyleLine:
		return span(lineFrames[position], "")
	default:
		// Restore only the weight so the remaining track keeps its color and focus underline.
		ring := ansi.NewStyle().Bold().String() + "〇" + ansi.NewStyle().Normal().String()
		return span(strings.Replace(slantedFrames[position], "〇", ring, 1), "")
	}
}

// A page supplies stable control IDs and confirmed observations. A frame never
// changes activation, even while the rendered thumb is between endpoints.
type switchAnimations struct {
	items      map[string]switchMotion
	generation uint64
}

type switchMotion struct {
	confirmed toggleState
	position  int
	token     uint64 // zero when stationary
}

type switchFrameMsg struct {
	id       string
	token    uint64
	position int
}

func (a *switchAnimations) reset() {
	clear(a.items) // tokens remain unique across page changes
}

func (a *switchAnimations) forget(id string) {
	delete(a.items, id)
}

func (a *switchAnimations) observe(id string, state toggleState, animate bool) tea.Cmd {
	if a.items == nil {
		a.items = make(map[string]switchMotion)
	}
	motion := a.items[id]
	// A refresh or a request awaiting shell acknowledgement can temporarily
	// disable a still-mounted switch. Keep its last confirmed endpoint until
	// the parent supplies an actual value. The disabled face remains inert.
	if state == togglePending || state == toggleUnavailable {
		motion.token = 0
		motion.position = switchEndpoint(motion.confirmed)
		a.items[id] = motion
		return nil
	}
	if state != toggleOn && state != toggleOff {
		delete(a.items, id)
		return nil
	}
	if !animate || motion.confirmed == toggleUnknown {
		a.items[id] = switchMotion{confirmed: state, position: switchEndpoint(state)}
		return nil
	}
	if motion.confirmed == state {
		return nil
	}
	motion.confirmed = state
	motion.token = 0
	if motion.position != switchEndpoint(state) {
		a.generation++
		motion.token = a.generation
	}
	a.items[id] = motion
	return motion.tick(id)
}

func (s switchMotion) tick(id string) tea.Cmd {
	if s.token == 0 {
		return nil
	}
	step := 1
	if s.confirmed == toggleOff {
		step = -1
	}
	next := switchFrameMsg{id, s.token, s.position + step}
	return tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg { return next })
}

func (a *switchAnimations) frame(msg switchFrameMsg) tea.Cmd {
	motion := a.items[msg.id]
	if motion.token == 0 || motion.token != msg.token || motion.position == msg.position {
		return nil
	}
	motion.position = msg.position
	if motion.position == switchEndpoint(motion.confirmed) {
		motion.token = 0
	}
	a.items[msg.id] = motion
	return motion.tick(msg.id)
}

func (a switchAnimations) control(id string, observed toggleModel) toggleModel {
	if motion := a.items[id]; motion.token != 0 && observed.state == motion.confirmed {
		observed.sliding, observed.position = true, motion.position
	}
	return observed
}
