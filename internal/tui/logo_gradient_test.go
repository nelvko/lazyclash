package tui

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestSidebarLogoGradientUsesFixedColumnsAcrossClickFrames(t *testing.T) {
	baseline := logoText(sidebarBackground, false)
	colors := logoForegroundCells(baseline)
	if ansi.Strip(baseline) != logo || len(colors) != 10 {
		t.Fatal("gradient changed the wordmark or its width")
	}
	if colors[0] != logoRGB(lipgloss.Color(logoGradientStart)) || colors[9] != logoRGB(lipgloss.Color(logoGradientEnd)) {
		t.Fatal("wordmark did not begin in blue and end in purple")
	}
	seen := map[[3]uint32]bool{}
	for column, current := range colors {
		seen[current] = true
		if column > 0 && (current[0] < colors[column-1][0] || current[1] > colors[column-1][1]) {
			t.Fatalf("gradient reversed its blue-to-purple direction at column %d", column)
		}
	}
	if len(seen) < 4 {
		t.Fatal("wordmark did not render a gradual gradient")
	}
	for index := 1; index < len(logoFrames); index++ {
		frame := logoTextFrame(index, 0, sidebarBackground, false)
		actual := logoForegroundCells(frame)
		if len(actual) != len(colors) {
			t.Fatalf("click frame %d changed the wordmark width", index)
		}
		for _, column := range []int{0, 1, 4, 5, 6, 7, 8, 9} {
			want := colors[column]
			if logoFrameAt(index).wake {
				want = logoRGB(lipgloss.Color(focus))
			}
			if actual[column] != want {
				t.Fatalf("click frame %d moved the color at fixed wordmark column %d", index, column)
			}
		}
	}
}

func TestSidebarLogoGradientIntroBrightensAndRestoresEachColumn(t *testing.T) {
	baseline := logoForegroundCells(logoText(sidebarBackground, false))
	previous := baseline
	for frame := 1; frame <= logoIntroFrames; frame++ {
		current := logoForegroundCells(logoTextFrame(0, frame, sidebarBackground, false))
		for _, column := range []int{0, 1, 4, 5, 6, 7, 8, 9} {
			brightness := func(c [3]uint32) uint32 { return c[0] + c[1] + c[2] }
			before, after := brightness(previous[column]), brightness(current[column])
			if frame <= logoIntroFrames/2 && after < before || frame > logoIntroFrames/2 && after > before {
				t.Fatalf("intro reversed brightness at frame %d column %d", frame, column)
			}
			if frame == logoIntroFrames/2 && after <= brightness(baseline[column]) {
				t.Fatalf("intro did not brighten column %d", column)
			}
			if frame == logoIntroFrames && current[column] != baseline[column] {
				t.Fatalf("intro did not restore column %d", column)
			}
		}
		if current[0] == current[9] {
			t.Fatalf("intro frame %d lost the gradient", frame)
		}
		previous = current
	}
}

func TestSidebarLogoGradientRespectsTerminalColorsAndDimmedBackdrop(t *testing.T) {
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
		t.Run(profile.String(), func(t *testing.T) {
			for _, dimmed := range []bool{false, true} {
				actual := terminalTheme(logoText(sidebarBackground, dimmed), profile)
				if ansi.Strip(actual) != logo || ansi.StringWidth(actual) != 10 {
					t.Fatal("terminal color conversion changed the wordmark")
				}
				if profile == colorprofile.ASCII {
					if strings.Contains(actual, "\x1b") {
						t.Fatal("NO_COLOR fallback retained gradient styling")
					}
					continue
				}
				if profile < colorprofile.TrueColor && (strings.Contains(actual, "38;2;") || strings.Contains(actual, "48;2;")) {
					t.Fatal("low-color output leaked true-color sequences")
				}
				colors := logoForegroundCells(actual)
				if dimmed {
					want := logoRGB(terminalColor(lipgloss.Color(backdropText), profile))
					for _, c := range colors {
						if c != want {
							t.Fatal("gradient escaped the opaque modal backdrop")
						}
					}
				} else if profile >= colorprofile.ANSI256 && colors[0] == colors[9] {
					t.Fatal("terminal quantization collapsed the blue and purple endpoints")
				}
			}
		})
	}
}

func logoRGB(c color.Color) [3]uint32 {
	if c == nil {
		return [3]uint32{}
	}
	r, g, b, _ := c.RGBA()
	return [3]uint32{r, g, b}
}

// Read the foreground actually applied to each displayed cell, including wide
// emoji, rather than checking whether a color escape merely appears in output.
func logoForegroundCells(s string) [][3]uint32 {
	var cells [][3]uint32
	var foreground color.Color
	parser := ansi.GetParser()
	defer ansi.PutParser(parser)
	var state byte
	for len(s) > 0 {
		parser.Reset()
		seq, width, n, next := ansi.DecodeSequence(s, state, parser)
		if ansi.HasCsiPrefix(seq) && parser.Command() == 'm' {
			params := parser.Params()
			if len(params) == 0 {
				foreground = nil
			}
			for i := 0; i < len(params); i++ {
				switch value := params[i].Param(0); {
				case value == 0 || value == 39:
					foreground = nil
				case value == 38 || value == 48 || value == 58:
					var c color.Color
					if consumed := ansi.ReadStyleColor(params[i:], &c); consumed > 0 {
						if value == 38 {
							foreground = c
						}
						i += consumed - 1
					}
				case value >= 30 && value <= 37:
					foreground = ansi.BasicColor(value - 30)
				case value >= 90 && value <= 97:
					foreground = ansi.BasicColor(value - 90 + 8)
				}
			}
		}
		for range width {
			cells = append(cells, logoRGB(foreground))
		}
		s, state = s[n:], next
	}
	return cells
}
