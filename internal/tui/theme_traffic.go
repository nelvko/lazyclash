package tui

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

type trafficPalette struct {
	upload, download, overlap string
}

// The chart keeps Mariana's orange and a nearby blue whose encoded sRGB
// midpoint is the specified overlap color. Other accent-colored UI is unchanged.
const trafficDownloadColor = "#7baed0"

func trafficCurveColors() trafficPalette {
	return makeTrafficPalette(focus, trafficDownloadColor)
}

func makeTrafficPalette(upload, download string) trafficPalette {
	return trafficPalette{upload: upload, download: download, overlap: blendSRGB50(upload, download)}
}

// Average encoded sRGB channels directly, with half values rounded upward.
// A terminal character has only one foreground color, so this color represents
// both series occupying a character cell, including distinct Braille subdots.
func blendSRGB50(first, second string) string {
	a := color.RGBAModel.Convert(lipgloss.Color(first)).(color.RGBA)
	b := color.RGBAModel.Convert(lipgloss.Color(second)).(color.RGBA)
	average := func(left, right uint8) int { return (int(left) + int(right) + 1) / 2 }
	return fmt.Sprintf("#%02X%02X%02X", average(a.R, b.R), average(a.G, b.G), average(a.B, b.B))
}
