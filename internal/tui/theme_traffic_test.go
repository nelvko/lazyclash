package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestTrafficPaletteMixesEncodedSRGBChannelsEqually(t *testing.T) {
	for _, sample := range []struct{ first, second, want string }{
		{"#000000", "#ffffff", "#808080"},
		{"#ff0000", "#0000ff", "#800080"},
		{"#010305", "#020406", "#020406"},
		{"#204060", "#204060", "#204060"},
		{"#f9ae58", "#7baed0", "#BAAE94"},
		{focus, accent, "#BFB19C"},
	} {
		t.Run(sample.first+"/"+sample.second, func(t *testing.T) {
			palette := makeTrafficPalette(sample.first, sample.second)
			if palette.upload != sample.first || palette.download != sample.second || palette.overlap != sample.want {
				t.Fatalf("palette = %+v, want original series colors and overlap %s", palette, sample.want)
			}
			if swapped := blendSRGB50(sample.second, sample.first); swapped != sample.want {
				t.Fatalf("swapping the two directions changed their equal mix: %s", swapped)
			}
		})
	}
}

func TestTrafficPaletteUsesExistingTerminalColorAdaptation(t *testing.T) {
	palette := trafficCurveColors()
	if palette.upload != focus || palette.download != trafficDownloadColor || palette.overlap != "#BAAE94" {
		t.Fatalf("Mariana traffic palette = %+v, want orange/blue with #BAAE94 overlap", palette)
	}
	for _, foreground := range []string{palette.upload, palette.download, palette.overlap} {
		original := themed("⠿", foreground, panel, false)
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
			t.Run(foreground+"/"+profile.String(), func(t *testing.T) {
				actual := terminalTheme(original, profile)
				if ansi.Strip(actual) != "⠿" {
					t.Fatalf("profile changed the Braille data glyph: %q", actual)
				}
				if profile == colorprofile.TrueColor && actual != original {
					t.Fatal("true color changed the selected palette")
				}
				if profile == colorprofile.ANSI256 && strings.Contains(actual, ";2;") {
					t.Fatal("ANSI256 retained a true-color sequence")
				}
				if profile <= colorprofile.ASCII && strings.Contains(actual, "\x1b") {
					t.Fatal("uncolored output retained color escapes")
				}
			})
		}
	}
}
