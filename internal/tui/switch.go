package tui

import (
	"cmp"
	"strings"
	"unicode"

	bubbleskey "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type switchVariant uint8

const (
	Slider switchVariant = iota
	Status
)

// Switch is a controlled input. On is supplied by the owner from its latest
// observation. An input event only requests the opposite value.
type Switch struct {
	Variant               switchVariant
	On, Disabled, Pending bool

	// Status labels omit the indicator. Message replaces the binary face for
	// observations such as an unavailable service or a shell awaiting activation.
	OnLabel, OffLabel, Message, Color string
}

func (s Switch) Enabled() bool { return !s.Disabled && !s.Pending }

// Intent is shared by pointer activation, shortcuts and focused keyboard input.
func (s Switch) Intent() (on, ok bool) {
	if !s.Enabled() {
		return false, false
	}
	return !s.On, true
}

var switchActivateKey = bubbleskey.NewBinding(bubbleskey.WithKeys("enter", "space", " "))

func (s Switch) KeyIntent(k tea.KeyPressMsg, focused bool) (on, ok bool) {
	if !focused || k.IsRepeat || !bubbleskey.Matches(k, switchActivateKey) {
		return false, false
	}
	return s.Intent()
}

type switchOptions struct {
	Surface                      string
	Focused, Hovered, Plain, Dim bool
	Width                        int // Exact total cell width when positive.
}

func (s Switch) View(o switchOptions) string {
	o.Surface = cmp.Or(o.Surface, bg)
	enabled := s.Enabled()
	marker := "  "
	if o.Focused && enabled {
		marker = "› "
		if s.Variant == Status {
			marker = " ›"
		}
	}
	face := themed(marker, focus, o.Surface, o.Dim)
	if s.Variant == Status {
		label, ink := s.statusFace()
		if enabled && (o.Focused || o.Hovered) {
			label = ansi.NewStyle().Underline(true).UnderlineColor(lipgloss.Color(focus)).Styled(label)
		}
		left, right := " ", " "
		if enabled && o.Plain {
			left, right = "[", "]"
		}
		face += themed(left, focus, o.Surface, o.Dim) + themed(label, ink, o.Surface, o.Dim) + themed(right, muted, o.Surface, o.Dim)
	} else {
		face += s.sliderFace(o)
	}
	if o.Width > 0 {
		face = cellPad(face, o.Width)
	}
	return face
}

func (s Switch) statusFace() (string, string) {
	// Strip terminal controls and collapse whitespace before measuring or styling.
	label := func(value string) string {
		words := strings.FieldsFunc(ansi.Strip(value), func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r)
		})
		return strings.Join(words, " ")
	}
	message := label(s.Message)
	switch {
	case s.Pending:
		return "◌ " + cmp.Or(message, "切换中…"), cmp.Or(s.Color, accent)
	case message != "":
		return message, cmp.Or(s.Color, disabled)
	case s.On:
		return "● " + cmp.Or(label(s.OnLabel), "已开启"), cmp.Or(s.Color, success)
	default:
		return "○ " + cmp.Or(label(s.OffLabel), "已关闭"), cmp.Or(s.Color, muted)
	}
}

func (s Switch) sliderFace(o switchOptions) string {
	railColor, thumbColor := divider, muted
	if s.On {
		railColor, thumbColor = buttonSelectedBackground, switchOnThumb
	}
	if !s.Enabled() {
		railColor, thumbColor = divider, disabled
	}
	railBG, thumbBG := railColor, thumbColor
	if o.Plain {
		railBG, thumbBG = o.Surface, o.Surface
	}
	rail := themed("░░░░", railColor, railBG, o.Dim)
	thumb := themed("██", thumbColor, thumbBG, o.Dim)
	if s.Pending {
		// Mark the last observed thumb position in color and monochrome.
		thumb = themed("◌ ", accent, o.Surface, o.Dim)
	}
	if s.On {
		return rail + thumb
	}
	return thumb + rail
}
