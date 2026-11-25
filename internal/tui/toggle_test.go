package tui

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestToggleIntentFocusAndBlockedStates(t *testing.T) {
	keys := []tea.KeyPressMsg{
		{Code: tea.KeyEnter}, {Code: tea.KeySpace, Text: " "},
		{Code: tea.KeyTab}, {Code: 'x', Text: "x"},
		{Code: tea.KeyEnter, Mod: tea.ModCtrl},
		{Code: tea.KeySpace, Text: " ", Mod: tea.ModAlt},
		{Code: tea.KeyEnter, IsRepeat: true},
	}
	for _, state := range []toggleState{toggleUnknown, toggleOn, toggleOff, togglePending, toggleUnavailable, toggleUnsupported, 255} {
		for _, focused := range []bool{false, true} {
			for i, key := range keys {
				var toggle toggleModel
				toggle.setState(state, "not actionable")
				before := toggle
				intent, ok := toggle.intent(key, focused)
				want := focused && (state == toggleOn || state == toggleOff) && i < 2
				if ok != want || ok && intent.On != (state == toggleOff) {
					t.Fatalf("state %d focused=%t key=%s: %+v/%t", state, focused, key, intent, ok)
				}
				if toggle != before {
					t.Fatal("input changed an observation before external confirmation")
				}
			}
		}
	}
}

func TestSwitchFacesKeepTheirWidthAtEveryFrame(t *testing.T) {
	for _, tc := range []struct {
		style string
		want  [3]string
	}{
		{"slanted", [3]string{"〇╱╱╱╱", "◢◤〇╱╱", "◢◤◢◤〇"}},
		{"line", [3]string{"●──", "━●─", "━━●"}},
		{"slider", [3]string{"      ", "      ", "      "}},
		{"text", [3]string{"OFF", " ON", " ON"}},
	} {
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
			for i, want := range tc.want {
				state := toggleOn
				if i == 0 {
					state = toggleOff
				}
				o := toggleOptions{Style: tc.style, Monochrome: profile == colorprofile.ASCII}
				// In an explicitly wide-ambiguous environment check the actual fallback.
				if effectiveSwitchStyle(o.Style, o.Monochrome) == "text" {
					want = [3]string{"OFF", " ON", " ON"}[i]
				}
				control := toggleModel{state: state, sliding: true, position: i}
				idle := renderToggle(control, o)
				check := func(view string) {
					t.Helper()
					view = terminalTheme(view, profile)
					if got := ansi.Strip(view); got != want || ansi.StringWidth(view) != ansi.StringWidth(want) {
						t.Fatalf("%s %s frame %d: want %q, got %q", tc.style, profile, i, want, view)
					}
				}
				check(idle)
				o.Hovered = true
				hover := renderToggle(control, o)
				check(hover)
				o.Hovered, o.Focused = false, true
				focus := renderToggle(control, o)
				check(focus)
				if hover == idle || focus == idle || focus == hover {
					t.Fatalf("%s lost hover/focus appearance", tc.style)
				}
			}
		}
	}
}

func TestSwitchStatusAndAppearanceFallbacks(t *testing.T) {
	o := toggleOptions{Style: "slanted", Labels: toggleLabels{Unavailable: "Temporarily unavailable", Unsupported: "Unsupported", Unknown: "Could not verify"}}
	for state, want := range map[toggleState]string{
		togglePending: "...", toggleUnknown: "Could not verify", toggleUnavailable: "Temporarily unavailable", toggleUnsupported: "Unsupported", 255: "Could not verify",
	} {
		control := toggleModel{state: state}
		if got := ansi.Strip(renderToggle(control, o)); got != want || control.canToggle() {
			t.Fatalf("state %d: %q, actionable=%v", state, got, control.canToggle())
		}
	}
	for _, dim := range []bool{false, true} {
		o := toggleOptions{Style: "slider", Dim: dim, Monochrome: !dim}
		if got := ansi.Strip(renderToggle(toggleModel{state: toggleOn}, o)); got != " ON" {
			t.Fatalf("slider lost state without background colors: %q", got)
		}
	}
	control := toggleModel{state: toggleOn}
	if renderToggle(control, toggleOptions{Style: "bad"}) != renderToggle(control, toggleOptions{Style: "slanted"}) {
		t.Fatal("invalid style differs from the default")
	}
}

func TestSliderPaintsTwoCellThumbAcrossSixCellTrack(t *testing.T) {
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI} {
		for position := range 3 {
			control := toggleModel{state: toggleOn, sliding: true, position: position}
			face := terminalTheme(renderToggle(control, toggleOptions{Style: "slider"}), profile)
			screen := uv.NewScreenBuffer(6, 1)
			uv.NewStyledString(face).Draw(screen, screen.Bounds())
			track, thumb := switchOnTrack, switchOnThumb
			if position == 0 {
				track, thumb = divider, muted
			}
			for x := range 6 {
				background := track
				if x/2 == position {
					background = thumb
				}
				want := terminalColor(lipgloss.Color(background), profile)
				cell := screen.CellAt(x, 0)
				if cell == nil || cell.Content != " " || cell.Style.Bg == nil ||
					color.RGBA64Model.Convert(cell.Style.Bg) != color.RGBA64Model.Convert(want) {
					t.Fatalf("%s frame %d column %d: got %+v, want a space with background %v", profile, position, x, cell, want)
				}
			}
		}
	}
}

func TestSwitchSliderTrackContrast(t *testing.T) {
	for _, pair := range [][2]string{{muted, divider}, {switchOnThumb, switchOnTrack}} {
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256} {
			thumb := terminalColor(lipgloss.Color(pair[0]), profile)
			track := terminalColor(lipgloss.Color(pair[1]), profile)
			if contrast := terminalContrast(thumb, track); contrast < 3 {
				t.Fatalf("%s %v thumb/track contrast %.2f", profile, pair, contrast)
			}
		}
	}
}

func TestSwitchMotionFollowsConfirmedState(t *testing.T) {
	var a switchAnimations
	const id = "first"
	if a.observe(id, toggleOff, true) != nil || a.observe(id, togglePending, true) != nil {
		t.Fatal("initial or pending observation started an animation")
	}
	for _, target := range []toggleState{toggleOn, toggleOff} {
		cmd := a.observe(id, target, true)
		for i := range 3 {
			state := a.control(id, toggleModel{state: target})
			position := i
			if target == toggleOff {
				position = 2 - i
			}
			if position != switchEndpoint(target) {
				if !state.sliding || state.position != position || cmd == nil {
					t.Fatalf("target %d: missing frame %d", target, position)
				}
				// Repeated business observations must not restart or stretch the timer.
				if a.observe(id, target, true) != nil {
					t.Fatal("unchanged observation restarted animation")
				}
				msg := cmd().(switchFrameMsg)
				cmd = a.frame(msg)
				if a.frame(msg) != nil {
					t.Fatal("duplicate frame scheduled another tick")
				}
			} else if state.sliding || cmd != nil {
				t.Fatal("motion did not stop after the final frame")
			}
		}
	}
	if a.observe(id, toggleOn, false) != nil || a.control(id, toggleModel{state: toggleOn}).sliding {
		t.Fatal("a non-animated style still moved")
	}
}

func TestSwitchInterruptionAndStaleFrames(t *testing.T) {
	var a switchAnimations
	id := "row"
	a.observe(id, toggleOff, true)
	first := a.observe(id, toggleOn, true)().(switchFrameMsg)
	last := a.frame(first)().(switchFrameMsg)
	// Reverse directly from the visible middle frame.
	reverse := a.observe(id, toggleOff, true)
	if reverse == nil || a.frame(last) != nil || a.control(id, toggleModel{state: toggleOff}).position != 1 {
		t.Fatal("old animation replaced reversal")
	}
	next := reverse().(switchFrameMsg)
	a.forget(id)
	a.observe(id, toggleOn, true)
	reopened := a.observe(id, toggleOff, true)().(switchFrameMsg)
	if a.frame(next) != nil || a.control(id, toggleModel{state: toggleOff}).position != 2 {
		t.Fatal("frame from a destroyed control was applied to its replacement")
	}
	a.reset()
	if a.frame(reopened) != nil || len(a.items) != 0 {
		t.Fatal("reset did not invalidate pending frames")
	}
	a.observe(id, toggleOff, true)
	a.observe(id, togglePending, true)
	if a.observe(id, toggleOff, true) != nil {
		t.Fatal("failed operation animated despite unchanged confirmed state")
	}
	// An unknown observation carries no usable baseline.
	a.observe(id, toggleUnknown, true)
	if a.observe(id, toggleOn, true) != nil {
		t.Fatal("recovery animated from an unknown state")
	}
}

func TestSwitchUnavailableDuringRefreshOrShellAcknowledgement(t *testing.T) {
	const id = "terminal"
	var a switchAnimations
	observe := func(state toggleState) tea.Cmd {
		return a.observe(id, state, true)
	}
	// An unavailable first observation, including after switching pages, is
	// not an off observation.
	for range 2 {
		observe(toggleUnavailable)
		observe(toggleUnavailable)
		if observe(toggleOn) != nil {
			t.Fatal("initial confirmation animated from an invented off state")
		}
		a.reset()
	}
	observe(toggleOff)
	observe(togglePending)
	for range 3 {
		observe(toggleUnavailable)
		control := a.control(id, toggleModel{state: toggleUnavailable})
		if control.canToggle() || control.sliding {
			t.Fatal("waiting for shell acknowledgement exposed the old actionable value")
		}
	}
	if observe(toggleOff) != nil {
		t.Fatal("refreshing the same confirmed state animated")
	}
	observe(toggleUnavailable)
	if observe(toggleOn) == nil || a.control(id, toggleModel{state: toggleOn}).position != 0 {
		t.Fatal("fresh confirmation after waiting lost the transition from off")
	}
}

func TestSwitchExplicitAmbiguousWidths(t *testing.T) {
	for _, tc := range []struct {
		style  string
		frames [3]string
		width  int
	}{{"slanted", slantedFrames, 6}, {"line", lineFrames, 3}} {
		want := tc.style
		for _, frame := range tc.frames {
			if ansi.StringWidth(frame) != tc.width {
				want = "text"
			}
		}
		if got := effectiveSwitchStyle(tc.style, false); got != want {
			t.Fatalf("%s frames require style %s, got %s", tc.style, want, got)
		}
	}
}
