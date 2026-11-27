package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestSwitchEmitsIntentWithoutChangingObservation(t *testing.T) {
	for _, variant := range []switchVariant{Slider, Status} {
		for _, on := range []bool{false, true} {
			for _, control := range []Switch{{On: on}, {On: on, Disabled: true}, {On: on, Pending: true}} {
				control.Variant = variant
				before := control
				desired, ok := control.Intent()
				if ok != control.Enabled() || ok && desired == on {
					t.Fatalf("unexpected pointer intent %+v: (%t, %t)", control, desired, ok)
				}
				for _, focused := range []bool{false, true} {
					for _, k := range []tea.KeyPressMsg{
						{Code: tea.KeyEnter}, {Code: tea.KeySpace, Text: " "},
						{Code: tea.KeyEnter, IsRepeat: true}, {Code: tea.KeyEnter, Mod: tea.ModCtrl},
						{Code: tea.KeySpace, Text: " ", Mod: tea.ModAlt}, {Code: tea.KeyTab},
					} {
						desired, ok := control.KeyIntent(k, focused)
						want := !control.Pending && !control.Disabled && focused && !k.IsRepeat && k.Mod == 0 && (k.Code == tea.KeyEnter || k.Code == tea.KeySpace)
						if ok != want || ok && desired == on || control != before {
							t.Fatalf("%+v key=%v focused=%t: (%t, %t)", control, k, focused, desired, ok)
						}
					}
				}
			}
		}
	}
}

func TestSwitchSliderUsesObservedThumbAndTheme(t *testing.T) {
	for _, tc := range []struct {
		control                          Switch
		marker, thumb, railInk, thumbInk string
	}{
		{Switch{}, "› ", "██", divider, muted},
		{Switch{On: true}, "› ", "██", buttonSelectedBackground, switchOnThumb},
		{Switch{Disabled: true, On: true}, "  ", "██", divider, disabled},
		{Switch{Pending: true}, "  ", "◌ ", divider, accent},
		{Switch{Pending: true, On: true}, "  ", "◌ ", divider, accent},
	} {
		for _, plain := range []bool{false, true} {
			railBG, thumbBG := tc.railInk, tc.thumbInk
			if plain {
				railBG, thumbBG = panel, panel
			}
			if tc.control.Pending {
				thumbBG = panel
			}
			rail := themed("░░░░", tc.railInk, railBG, false)
			thumb := themed(tc.thumb, tc.thumbInk, thumbBG, false)
			track := thumb + rail
			if tc.control.On {
				track = rail + thumb
			}
			got := tc.control.View(switchOptions{Surface: panel, Focused: true, Plain: plain})
			if want := themed(tc.marker, focus, panel, false) + track; got != want || ansi.StringWidth(got) != 8 {
				t.Fatalf("slider %+v plain=%t: %q, want %q", tc.control, plain, got, want)
			}
		}
	}
}

func TestSwitchStatusUsesLocalizedFacesAndTerminalCells(t *testing.T) {
	for _, tc := range []struct {
		control      Switch
		label, color string
	}{
		{Switch{}, "○ 已关闭", muted},
		{Switch{On: true}, "● 已开启", success},
		{Switch{On: true, OnLabel: "\x1b[31mCore\a核心\x1b[0m\n e\u0301 🇨🇳 👩‍💻"}, "● Core 核心 e\u0301 🇨🇳 👩‍💻", success},
		{Switch{Disabled: true, OffLabel: "Stopped"}, "○ Stopped", muted},
		{Switch{Pending: true, On: true, Message: "Stopping…"}, "◌ Stopping…", accent},
		{Switch{Disabled: true, Message: "Unknown", Color: warning}, "Unknown", warning},
	} {
		tc.control.Variant = Status
		label, ink := tc.control.statusFace()
		if label != tc.label || ink != tc.color {
			t.Fatalf("status face: %q %s, want %q %s", label, ink, tc.label, tc.color)
		}
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
			o := switchOptions{Surface: panel, Plain: profile == colorprofile.ASCII}
			for _, focused := range []bool{false, true} {
				o.Focused = focused
				view := terminalTheme(tc.control.View(o), profile)
				if !strings.Contains(ansi.Strip(view), tc.label) || ansi.StringWidth(view) != 4+ansi.StringWidth(tc.label) || strings.Contains(view, "\x1b[31mCore") {
					t.Fatalf("profile=%v focus=%t: %q", profile, focused, view)
				}
				hasMarker := strings.Contains(ansi.Strip(view), "›")
				if hasMarker != (focused && tc.control.Enabled()) {
					t.Fatalf("unexpected focus: %q", view)
				}
				for _, width := range []int{1, 2, 4, 6, 9, 18, 32} {
					sized := o
					sized.Width = width
					if painted := tc.control.View(sized); ansi.StringWidth(painted) != width || strings.ContainsAny(ansi.Strip(painted), "\r\t\n") {
						t.Fatalf("%q exceeds %d terminal cells", painted, width)
					}
				}
			}
		}
	}
}
