package tui

import (
	"image/color"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) lowColor() bool {
	return m.noColor || m.colorProfile != colorprofile.Unknown && m.colorProfile <= colorprofile.ANSI
}

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

// terminalTheme is the Mariana profile adapter used by standalone components.
func terminalTheme(screen string, profile colorprofile.Profile) string {
	return paintTerminalTheme(screen, profile, false)
}

// Apply the selected palette and terminal capability to the final composed screen,
// including nested Bubbles controls. Only control sequences require decoding.
func paintTerminalTheme(screen string, profile colorprofile.Profile, light bool) string {
	if !light && (profile == colorprofile.TrueColor || profile == colorprofile.Unknown) {
		return screen
	}
	if profile != colorprofile.Unknown && profile <= colorprofile.ASCII {
		return ansi.Strip(screen)
	}
	colors := framePalette{profile, light}
	var out strings.Builder
	out.Grow(len(screen))
	styles := make(map[string]string)
	parser := ansi.GetParser()
	defer ansi.PutParser(parser)
	for len(screen) > 0 {
		// In a UTF-8 string raw C1 control bytes (CSI, OSC, etc.) decode as
		// RuneError. Copy regular text in runs without splitting CJK or emoji.
		start := strings.IndexFunc(screen, func(r rune) bool { return r == ansi.ESC || r == utf8.RuneError })
		if start < 0 {
			out.WriteString(screen)
			break
		}
		out.WriteString(screen[:start])
		seq, _, n, _ := ansi.DecodeSequence(screen[start:], ansi.NormalState, nil)
		screen = screen[start+n:]
		if !ansi.HasCsiPrefix(seq) || seq[len(seq)-1] != 'm' {
			out.WriteString(seq)
			continue
		}
		value, ok := styles[seq]
		if !ok {
			value = seq
			parser.Reset()
			ansi.DecodeSequence(seq, ansi.NormalState, parser)
			if parser.Command() == 'm' {
				value = colors.style(parser.Params())
			}
			styles[seq] = value
		}
		out.WriteString(value)
	}
	return out.String()
}

type framePalette struct {
	profile colorprofile.Profile
	light   bool
}

func (p framePalette) style(params ansi.Params) string {
	var style ansi.Style
	for i := 0; i < len(params); i++ {
		param := params[i].Param(0)
		if param == 38 || param == 48 || param == 58 {
			var c color.Color
			if n := ansi.ReadStyleColor(params[i:], &c); n > 0 {
				switch param {
				case 38:
					style = style.ForegroundColor(p.color(c, param))
				case 48:
					style = style.BackgroundColor(p.color(c, param))
				case 58:
					style = style.UnderlineColor(p.color(c, param))
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

func terminalColor(c color.Color, profile colorprofile.Profile) color.Color {
	return terminalForeground(c, profile, false)
}

func terminalForeground(c color.Color, profile colorprofile.Profile, light bool) color.Color {
	return (framePalette{profile, light}).color(c, 38)
}
