package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestTrafficPaletteUsesExistingTerminalColorAdaptation(t *testing.T) {
	for _, foreground := range []string{trafficUploadColor, trafficDownloadColor} {
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
