package tui

import (
	"image/color"
	"math"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestMarianaTerminalProfilesPreserveContentAndAttributes(t *testing.T) {
	content := "> Install · 已选择\nFailure: proxy unavailable\nnormal text 38;2;1;2;3"
	screen := ansi.NewStyle().Bold().ForegroundColor(lipgloss.Color(text)).BackgroundColor(lipgloss.Color(panel)).String() + content + ansi.ResetStyle
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII, colorprofile.NoTTY} {
		t.Run(profile.String(), func(t *testing.T) {
			actual := terminalTheme(screen, profile)
			if ansi.Strip(actual) != content {
				t.Fatalf("terminal adaptation changed text: %q", actual)
			}
			if profile == colorprofile.TrueColor && actual != screen {
				t.Fatal("true color output was changed")
			}
			if profile >= colorprofile.ANSI && !strings.Contains(actual, "\x1b[1;") {
				t.Fatal("lost non-color bold attribute")
			}
			if profile < colorprofile.ANSI && strings.Contains(actual, "\x1b") {
				t.Fatal("uncolored output retained styling")
			}
			if profile == colorprofile.ANSI256 && (!strings.Contains(actual, "48;5;235") || strings.Contains(actual, "48;5;17m")) {
				t.Fatal("panel did not use neutral gray fallback")
			}
			if profile == colorprofile.ANSI && (!strings.Contains(actual, ";40m") || strings.Contains(actual, ";48;")) {
				t.Fatal("ANSI16 surface did not use basic black")
			}
		})
	}
}

func TestMarianaTerminalProfilePreservesSubparametersAndUnderlineColor(t *testing.T) {
	screen := "\x1b[4:3;58:2::133:179:224;48:2::43:50:58mFocused\x1b[m"
	actual := terminalTheme(screen, colorprofile.ANSI256)
	if !strings.Contains(actual, "4:3;58;5;110;48;5;235m") || ansi.Strip(actual) != "Focused" {
		t.Fatalf("lost SGR meaning: %q", actual)
	}
}

func TestNoColorCannotBeOverriddenByCapabilityResponse(t *testing.T) {
	for _, value := range []string{"1", "true", "yes", "false", "0", "please"} {
		profile := themeProfile([]string{"TERM=xterm-256color", "NO_COLOR=" + value}, colorprofile.TrueColor)
		if profile != colorprofile.ASCII {
			t.Fatalf("NO_COLOR=%s did not disable color", value)
		}
		if terminalColor(lipgloss.Color(bg), profile) != nil {
			t.Fatal("uncolored View still changes terminal background")
		}
	}
	if themeProfile([]string{"NO_COLOR="}, colorprofile.TrueColor) != colorprofile.TrueColor {
		t.Fatal("empty NO_COLOR unexpectedly disabled color")
	}
}

func TestMarianaTrueColorAnd256Contrast(t *testing.T) {
	for _, pair := range []struct {
		name, foreground, background string
	}{
		{"body", text, bg},
		{"modal body", text, raised},
		{"muted on modal", muted, raised},
		{"selection", selectionForeground, selectionBackground},
		{"button", primaryButtonForeground, primaryButtonBackground},
		{"error", errorColor, inputBackground},
		{"focus", focus, raised},
		{"accent", accent, raised},
	} {
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256} {
			t.Run(profile.String()+"/"+pair.name, func(t *testing.T) {
				foreground := terminalColor(lipgloss.Color(pair.foreground), profile)
				background := terminalColor(lipgloss.Color(pair.background), profile)
				if ratio := terminalContrast(foreground, background); ratio < 4.5 {
					t.Fatalf("contrast %.2f:1 is below 4.5:1", ratio)
				}
			})
		}
	}
}

func TestMarianaModelProfileAndResizeKeepWizardTaskAndFocus(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		name, env := "colored", ""
		if noColor {
			name, env = "NO_COLOR", "please"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("NO_COLOR", env)
			m := ready(&fakeBackend{})
			m, _ = update(m, key("i"))
			m.wizard.step = stepVersion
			m.wizard.specific = true
			m.wizard.version.SetValue("v1.19.33")
			m.wizard.focus = "version-input"
			ctx := m.newJob("resolve")
			defer m.cancel()
			id := m.id
			for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {120, 18}} {
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				for _, profile := range []colorprofile.Profile{colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII, colorprofile.TrueColor} {
					m, _ = update(m, tea.ColorProfileMsg{Profile: profile})
					view := m.View()
					if !m.wizard.open || m.wizard.step != stepVersion || !m.wizard.specific || m.wizard.version.Value() != "v1.19.33" || m.wizard.focus != "version-input" {
						t.Fatal("profile/resize changed wizard selection or focus")
					}
					if m.id != id || m.job != "resolve" || !m.busy || ctx.Err() != nil {
						t.Fatal("profile/resize changed the asynchronous task")
					}
					if view.BackgroundColor != nil || view.ForegroundColor != nil {
						t.Fatal("model view changes terminal default colors through OSC10/11")
					}
					if noColor || profile == colorprofile.ASCII {
						if strings.Contains(view.Content, "\x1b") {
							t.Fatal("uncolored model view retained terminal colors")
						}
					}
					assertCanvas(t, view.Content, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
				}
			}
			m.abortLookup()
			before := m.wizard.focus
			m, _ = update(m, special(tea.KeyTab))
			if m.wizard.focus == before || !strings.Contains(ansi.Strip(m.View().Content), "›") {
				t.Fatal("keyboard focus is not operable and visible after terminal changes")
			}
		})
	}
}

func terminalContrast(a, b color.Color) float64 {
	luminance := func(c color.Color) float64 {
		r, g, b, _ := c.RGBA()
		linear := func(v uint32) float64 {
			f := float64(v) / 65535
			if f <= 0.04045 {
				return f / 12.92
			}
			return math.Pow((f+0.055)/1.055, 2.4)
		}
		return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
	}
	l1, l2 := luminance(a), luminance(b)
	return (math.Max(l1, l2) + 0.05) / (math.Min(l1, l2) + 0.05)
}
