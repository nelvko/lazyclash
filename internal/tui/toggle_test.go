package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestToggleStatesAndTheme(t *testing.T) {
	for _, sample := range []struct {
		state             toggleState
		label, foreground string
		actionable        bool
	}{
		{toggleOn, "● 已开启", success, true},
		{toggleOff, "○ 已关闭", muted, true},
		{togglePending, "◌ 切换中…", accent, false},
		{toggleUnavailable, "不可用", disabled, false},
		{toggleUnsupported, "暂不支持", disabled, false},
		{toggleUnknown, "状态未知", disabled, false},
		{toggleState(255), "状态未知", disabled, false},
	} {
		t.Run(sample.label+fmt.Sprint(sample.state), func(t *testing.T) {
			var toggle toggleModel
			toggle.setState(sample.state, "parent-owned explanation")
			for _, surface := range []string{bg, panel, raised} {
				view := renderToggle(toggle, toggleOptions{Surface: surface})
				if got := ansi.Strip(view); strings.TrimSpace(got) != sample.label || ansi.StringWidth(view) != 9 {
					t.Fatalf("wrong state or unstable default width: %q", got)
				}
				want := themed(cellPad(sample.label, 9), sample.foreground, surface, false)
				if view != want {
					t.Fatalf("wrong semantic color/surface: got %q, want %q", view, want)
				}
			}
			if toggle.canToggle() != sample.actionable || toggle.reason != "parent-owned explanation" {
				t.Fatal("availability or parent-supplied reason lost")
			}
		})
	}
	var zero toggleModel
	if zero.canToggle() || strings.TrimSpace(ansi.Strip(renderToggle(zero, toggleOptions{}))) != "状态未知" {
		t.Fatal("zero value must be unknown, never off")
	}
}

func TestToggleCustomLabelsAndSingleLine(t *testing.T) {
	labels := toggleLabels{
		On: "运行中", Off: "已停止", Pending: "停止中…",
		Unavailable: "等待权限", Unsupported: "平台不支持", Unknown: "查询中",
	}
	var toggle toggleModel
	for _, sample := range []struct {
		state toggleState
		want  string
	}{
		{toggleOn, "● 运行中"}, {toggleOff, "○ 已停止"}, {togglePending, "◌ 停止中…"},
		{toggleUnavailable, "等待权限"}, {toggleUnsupported, "平台不支持"}, {toggleUnknown, "查询中"},
	} {
		toggle.setState(sample.state, "")
		view := renderToggle(toggle, toggleOptions{Labels: labels})
		if strings.TrimSpace(ansi.Strip(view)) != sample.want || ansi.StringWidth(view) != 10 {
			t.Fatalf("custom label/width lost: %q", view)
		}
	}
	toggle.setState(toggleOn, "")
	labels = toggleLabels{On: "\x1b[31mCore 核心\x1b[0m\n\t运行"}
	view := renderToggle(toggle, toggleOptions{Labels: labels})
	if got := strings.TrimSpace(ansi.Strip(view)); got != "● Core 核心 运行" || strings.Contains(view, "\x1b[31m") {
		t.Fatalf("custom label changed theme or row geometry: %q", view)
	}
	toggle.setState(toggleOff, "")
	if got := strings.TrimSpace(ansi.Strip(renderToggle(toggle, toggleOptions{Labels: labels}))); got != "○ 已关闭" {
		t.Fatalf("partial overrides lost defaults: %q", got)
	}
}

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

func TestTogglePendingAndExternalObservationsPreserveLastConfirmation(t *testing.T) {
	for _, confirmed := range []toggleState{toggleOn, toggleOff} {
		var toggle toggleModel
		toggle.setState(confirmed, "")
		for _, state := range []toggleState{togglePending, toggleUnavailable, toggleUnsupported, toggleUnknown} {
			toggle.setState(state, "latest reason")
			if toggle.lastConfirmed != confirmed || toggle.state != state {
				t.Fatal("non-confirming observation replaced the last confirmed state")
			}
			if _, ok := toggle.intent(tea.KeyPressMsg{Code: tea.KeyEnter}, true); ok {
				t.Fatal("historical state made a non-actionable observation toggleable")
			}
		}
		toggle.setState(toggleOff, "")
		if toggle.lastConfirmed != toggleOff || toggle.reason != "" {
			t.Fatal("fresh external observation did not replace state and clear the reason")
		}
	}
	var toggle toggleModel
	toggle.setState(togglePending, "")
	if toggle.lastConfirmed != toggleUnknown {
		t.Fatal("pending invented a previously confirmed state")
	}
}

func TestToggleFocusAndTerminalProfilesKeepGeometryAndState(t *testing.T) {
	for _, state := range []toggleState{toggleOn, toggleOff, togglePending, toggleUnavailable, toggleUnsupported, toggleUnknown} {
		var toggle toggleModel
		toggle.setState(state, "")
		before := toggle
		for _, right := range []bool{false, true} {
			options := toggleOptions{Width: 16, RightAligned: right, Surface: panel}
			idle := renderToggle(toggle, options)
			options.Focused = true
			focused := renderToggle(toggle, options)
			labels := options.Labels.resolved()
			_, foreground := labels.face(state)
			open, _, _ := strings.Cut(themed("x", foreground, panel, false), "x")
			underline := ansi.NewStyle().Underline(true).UnderlineColor(lipgloss.Color(focus)).String()
			if !strings.HasPrefix(focused, open) || !strings.Contains(focused, underline) || ansi.Strip(focused) != ansi.Strip(idle) {
				t.Fatalf("focus changed the value/geometry or lost its themed underline: %q", focused)
			}
			options.Focused = false
			if renderToggle(toggle, options) != idle || toggle != before {
				t.Fatal("blur did not restore the view, or focus changed state")
			}
			for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
				view := terminalTheme(focused, profile)
				if ansi.StringWidth(view) != 16 || ansi.Strip(view) != ansi.Strip(idle) {
					t.Fatalf("%s moved the control: %q", profile, view)
				}
				if profile == colorprofile.ASCII && strings.Contains(view, "\x1b") {
					t.Fatal("NO_COLOR retained styles")
				}
			}
			options.Dim = true
			dimmed := renderToggle(toggle, options)
			if dimmed != themed(ansi.Strip(idle), text, panel, true) {
				t.Fatal("toggle did not reuse the modal backdrop theme")
			}
		}
	}
}

func TestToggleWidthsAlignmentAndUnicode(t *testing.T) {
	for _, labels := range []toggleLabels{
		{},
		{On: "Core 核心 e\u0301 🇨🇳 👩‍💻", Off: "Stopped", Pending: "重启中…"},
	} {
		for _, state := range []toggleState{toggleOn, toggleOff, togglePending, toggleUnavailable, toggleUnsupported, toggleUnknown} {
			var toggle toggleModel
			toggle.setState(state, "")
			resolved := labels.resolved()
			face, _ := resolved.face(state)
			for _, width := range []int{1, 2, 3, 7, 9, 12, 24, 40, 9} {
				for _, right := range []bool{false, true} {
					view := renderToggle(toggle, toggleOptions{Labels: labels, Width: width, RightAligned: right, Focused: true})
					if ansi.StringWidth(view) != width || strings.ContainsAny(ansi.Strip(view), "\r\n\t") {
						t.Fatalf("state %d width %d right=%t escaped its row: %q", state, width, right, view)
					}
					cropped := ansi.Strip(cellCrop(face, width))
					padding := strings.Repeat(" ", width-ansi.StringWidth(cropped))
					want := cropped + padding
					if right {
						want = padding + cropped
					}
					if ansi.Strip(view) != want {
						t.Fatalf("wrong terminal-cell alignment: got %q want %q", ansi.Strip(view), want)
					}
				}
			}
		}
	}
	for value, width := range map[string]int{"●": 1, "○": 1, "◌": 1, "已开启": 6, "e\u0301": 1, "🇨🇳": 2, "👩‍💻": 2} {
		if got := ansi.StringWidth(value); got != width {
			t.Fatalf("%q: got %d terminal cells, want %d", value, got, width)
		}
	}
}
