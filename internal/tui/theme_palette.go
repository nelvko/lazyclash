package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

const (
	mistCanvas    = "#eef2f5"
	mistSidebar   = "#e0e6eb"
	mistCard      = "#e2e8ee"
	mistSelection = "#d0e1ed"
	mistBody      = "#283746"
	mistMuted     = "#5e6d7c"
	mistBlue      = "#306a9e"
	mistGreen     = "#3d713e"
	mistRed       = "#a84a4a"
	mistAmber     = "#8d641a"
	mistLine      = "#ccd5dd"
)

type terminalSwatch struct {
	rgb     color.Color
	indexed ansi.IndexedColor
	basic   ansi.BasicColor
}

func (s terminalSwatch) forProfile(profile colorprofile.Profile) color.Color {
	switch profile {
	case colorprofile.ANSI:
		return s.basic
	case colorprofile.ANSI256:
		return s.indexed
	default:
		return s.rgb
	}
}

type themeSwatches struct {
	dark, light terminalSwatch
}

func rgba(c color.Color) color.RGBA {
	return color.RGBAModel.Convert(c).(color.RGBA)
}

var (
	// Mariana shares this foreground with its sidebar background. Mist uses
	// white ink on solid action buttons and a separate color for the sidebar.
	actionLabelSource = rgba(lipgloss.Color(primaryButtonForeground))
	mistActionLabel   = terminalSwatch{color.White, 231, 15}
)

// Index the canonical colors once. Each entry pairs Mariana and Mist, with
// explicit indices for 256-color and 16-color terminals respectively.
var interfaceSwatches = func() map[color.RGBA]themeSwatches {
	swatches := make(map[color.RGBA]themeSwatches)
	add := func(source string, dark256 ansi.IndexedColor, dark16 ansi.BasicColor, light string, light256 ansi.IndexedColor, light16 ansi.BasicColor) {
		dark := lipgloss.Color(source)
		swatches[rgba(dark)] = themeSwatches{
			dark:  terminalSwatch{dark, dark256, dark16},
			light: terminalSwatch{lipgloss.Color(light), light256, light16},
		}
	}
	add(bg, 236, 0, mistCanvas, 255, 15)
	add(shellBackground, 236, 0, mistCanvas, 255, 15)
	add(panel, 235, 0, mistCard, 254, 7)
	add(gridCardBackground, 235, 0, mistCard, 254, 7)
	add(raised, 237, 0, mistCard, 254, 7)
	add(tooltipBackground, 233, 0, "#cfdae3", 252, 7)
	add(sidebarBackground, 234, 0, mistSidebar, 253, 7)
	add(inputBackground, 235, 0, "#f8fafb", 231, 15)
	add(text, 253, 15, mistBody, 237, 0)
	add(pageTitleForeground, 255, 15, mistBody, 237, 0)
	add(pageTitleMarker, 67, 8, mistBlue, 24, 4)
	add(muted, 249, 7, mistMuted, 240, 8)
	add(disabled, 245, 8, "#697885", 241, 8)
	add(border, 244, 8, "#889aa8", 246, 8)
	add(divider, 238, 8, mistLine, 252, 8)
	add(accent, 110, 14, mistBlue, 24, 4)
	add(overviewAddressColor, 211, 13, "#883965", 89, 5)
	add(websiteSlowLatency, 222, 11, "#756019", 58, 3)
	add(focus, 215, 11, mistAmber, 94, 3)
	add(selectionBackground, 240, 4, mistSelection, 153, 7)
	add(hoverBackground, 238, 0, "#d9e4eb", 253, 7)
	add(primaryButtonBackground, 68, 14, mistBlue, 24, 4)
	add(buttonSecondaryBackground, 239, 0, mistCard, 254, 7)
	add(buttonSecondaryHoverBackground, 240, 0, "#d9e3eb", 253, 7)
	add(success, 114, 10, mistGreen, 22, 2)
	add(errorColor, 210, 9, mistRed, 124, 1)
	add(buttonHoverBackground, 239, 0, "#d9e3eb", 253, 7)
	add(buttonSelectedBackground, 24, 4, mistSelection, 153, 7)
	add(buttonSelectedHoverBackground, 25, 4, "#bfd6e6", 152, 7)
	add(buttonPrimaryHoverBackground, 75, 14, "#255b89", 24, 4)
	add(buttonDangerForeground, 217, 9, mistRed, 124, 1)
	add(buttonDangerHoverBackground, 238, 0, "#ecd9da", 224, 7)
	add(routingIdleText, 252, 7, mistMuted, 240, 8)
	add(routingIdleBackground, 237, 0, "#dce4eb", 253, 7)
	add(routingSelectedText, 231, 15, mistBody, 237, 0)
	add(routingSelectedBackground, 60, 4, mistSelection, 153, 7)
	add(routingSelectedHoverBackground, 25, 4, "#bfd6e6", 152, 7)
	add(routingFocus, 222, 11, mistBlue, 24, 4)
	add(proxyNodeSelectedBackground, 24, 4, mistSelection, 153, 7)
	add(proxyNodeSelectedLink, 153, 14, mistBlue, 24, 4)
	add(proxyNodeEmphasizedMeta, 188, 7, mistMuted, 240, 8)
	add(proxyNodeEmphasizedFast, 157, 10, mistGreen, 22, 2)
	add(switchOnThumb, 117, 6, mistBlue, 24, 4)
	add(noticeSlate, 24, 4, "#d0dce5", 252, 7)
	add(noticeText, 255, 15, mistBody, 237, 0)
	add(noticeCheck, 194, 10, mistGreen, 22, 2)
	add(noticeWarning, 223, 11, mistAmber, 94, 3)
	add(noticeErrorFill, 95, 1, "#efdcdd", 224, 7)
	add(noticeErrorText, 255, 15, "#663c42", 95, 0)
	add(noticeErrorMark, 255, 15, mistRed, 124, 1)

	// These accents previously used the terminal library's standard Mariana approximation.
	addApproximate := func(source, light string, indexed ansi.IndexedColor, basic ansi.BasicColor) {
		c := lipgloss.Color(source)
		add(source, ansi.Convert256(c), ansi.Convert16(c), light, indexed, basic)
	}
	addApproximate(settingsRowHighlight, "#d9e4eb", 253, 7)
	addApproximate(trafficDownloadColor, mistBlue, 24, 4)
	return swatches
}()

// The logo's generated colors are looked up after semantic colors, so an
// intermediate animation frame cannot override an interface token.
var mistWordmark = func() map[color.RGBA]terminalSwatch {
	colors := make(map[color.RGBA]terminalSwatch)
	base := lipgloss.Blend1D(logoColumns, lipgloss.Color(mistBlue), lipgloss.Color("#566583"))
	peak := lipgloss.Blend1D(logoColumns, lipgloss.Color("#204d76"), lipgloss.Color("#413e70"))
	for frame := 0; frame < logoIntroFrames; frame++ {
		for column := 0; column < logoColumns; column++ {
			dark := lipgloss.Color(logoGradientColor(column, frame))
			light := lipgloss.Color(blendedLogoColor(base[column], peak[column], frame))
			colors[rgba(dark)] = terminalSwatch{light, ansi.Convert256(light), 4}
		}
	}
	return colors
}()

func (p framePalette) color(c color.Color, role int) color.Color {
	if p.profile == colorprofile.Unknown {
		p.profile = colorprofile.TrueColor
	}
	if c == nil || p.profile <= colorprofile.ASCII {
		return nil
	}
	if !p.light && p.profile == colorprofile.TrueColor {
		return c
	}
	// Indexed colors may be customized by the terminal. Only RGB tokens belong
	// to the application's palette, even if an index happens to have the same RGB.
	switch c.(type) {
	case ansi.BasicColor, ansi.IndexedColor:
		return p.profile.Convert(c)
	}
	key := rgba(c)
	if p.light && role == 38 && key == actionLabelSource {
		return mistActionLabel.forProfile(p.profile)
	}
	if swatches, ok := interfaceSwatches[key]; ok {
		if p.light {
			return swatches.light.forProfile(p.profile)
		}
		return swatches.dark.forProfile(p.profile)
	}
	if p.light {
		if swatch, ok := mistWordmark[key]; ok {
			return swatch.forProfile(p.profile)
		}
	}
	return p.profile.Convert(c)
}
