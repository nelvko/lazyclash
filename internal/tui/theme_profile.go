package tui

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// themeProfile keeps a late terminal capability response from overriding the
// user's explicit request for no color. NO_COLOR uses its standard nonempty
// value semantics rather than treating the value as a boolean.
func themeProfile(environ []string, detected colorprofile.Profile) colorprofile.Profile {
	for _, entry := range environ {
		if value, ok := strings.CutPrefix(entry, "NO_COLOR="); ok && value != "" {
			return colorprofile.ASCII
		}
	}
	return detected
}

// terminalTheme adapts the completed view, including third-party text inputs.
// Layout and focus remain independent of color capability. Explicit fallbacks
// avoid automatic RGB approximation turning muted surfaces into saturated blue.
func terminalTheme(screen string, profile colorprofile.Profile) string {
	if profile == colorprofile.TrueColor || profile == colorprofile.Unknown {
		return screen
	}
	if profile <= colorprofile.ASCII {
		return ansi.Strip(screen)
	}
	var out strings.Builder
	out.Grow(len(screen))
	parser := ansi.GetParser()
	defer ansi.PutParser(parser)
	var state byte
	for len(screen) > 0 {
		parser.Reset()
		seq, _, n, next := ansi.DecodeSequence(screen, state, parser)
		if ansi.HasCsiPrefix(seq) && parser.Command() == 'm' {
			out.WriteString(terminalStyle(parser.Params(), profile))
		} else {
			out.WriteString(seq)
		}
		screen, state = screen[n:], next
	}
	return out.String()
}

func terminalStyle(params ansi.Params, profile colorprofile.Profile) string {
	var style ansi.Style
	for i := 0; i < len(params); i++ {
		param := params[i].Param(0)
		if param == 38 || param == 48 || param == 58 {
			var c color.Color
			if n := ansi.ReadStyleColor(params[i:], &c); n > 0 {
				switch param {
				case 38:
					style = style.ForegroundColor(terminalColor(c, profile))
				case 48:
					style = style.BackgroundColor(terminalColor(c, profile))
				case 58:
					style = style.UnderlineColor(terminalColor(c, profile))
				}
				i += n - 1
				continue
			}
		}
		// Preserve non-color attributes, including colon-separated subparameters
		// such as curly underlining, without changing their meaning.
		value := strconv.Itoa(param)
		for params[i].HasMore() && i+1 < len(params) {
			i++
			value += ":" + strconv.Itoa(params[i].Param(0))
		}
		style = append(style, value)
	}
	return style.String()
}

// terminalColor deliberately groups low-capability surfaces into neutral grays.
// ANSI's small user-defined palette cannot retain every surface distinction;
// borders and textual selection/focus markers remain essential there.
func terminalColor(c color.Color, profile colorprofile.Profile) color.Color {
	if c == nil {
		return nil
	}
	if profile == colorprofile.TrueColor || profile == colorprofile.Unknown {
		return c
	}
	if profile <= colorprofile.ASCII {
		return nil
	}
	if _, ok := c.(ansi.BasicColor); ok {
		return profile.Convert(c)
	}
	if _, ok := c.(ansi.IndexedColor); ok {
		return profile.Convert(c)
	}
	// This is the sole terminal fallback table for the semantic Mariana palette.
	// ANSI256 indices >=16 use the standardized palette, independent of a user's
	// customized base sixteen colors. ANSI16 necessarily uses that base palette.
	fallbacks := [...]struct {
		value           string
		ansi256, ansi16 uint8
	}{
		{bg, 236, 0},
		{panel, 235, 0},
		{raised, 237, 0},
		{sidebarBackground, 234, 0},
		{inputBackground, 235, 0},
		{text, 253, 15},
		{muted, 249, 7},
		{disabled, 245, 8},
		{selectionBackground, 240, 4},
		{hoverBackground, 238, 0},
		{border, 244, 8},
		{divider, 238, 8},
		{accent, 110, 14},
		{focus, 215, 11},
		{primaryButtonBackground, 68, 14},
		{success, 114, 10},
		{errorColor, 210, 9},
	}
	r, g, b, _ := c.RGBA()
	for _, fallback := range fallbacks {
		fr, fg, fb, _ := lipgloss.Color(fallback.value).RGBA()
		if r == fr && g == fg && b == fb {
			if profile == colorprofile.ANSI {
				return ansi.BasicColor(fallback.ansi16)
			}
			return ansi.IndexedColor(fallback.ansi256)
		}
	}
	return profile.Convert(c)
}
