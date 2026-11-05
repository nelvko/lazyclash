package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestProxiesDelayInteractionStylePreservesStateAndGeometry(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, state := range []string{"untested", "success", "timeout", "failed", "testing", "queued"} {
			for _, selected := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/selected=%t", language, state, selected), func(t *testing.T) {
					m, _ := proxiesModel(t, 160, 40)
					m.locale = i18n.New(language)
					proxy := m.snapshot.Proxies["HK 01"]
					proxy.History = nil
					m.snapshot.Proxies["HK 01"] = proxy
					if selected {
						m.snapshot.Groups[0].Now = "HK 01"
					}
					switch state {
					case "success", "timeout", "failed":
						m.proxies.results[proxyResultKey("PROXY", "HK 01")] = proxyMeasurement{
							result: mihomo.ProxyDelay{State: state, Delay: 57},
							path:   m.proxyPathKey("HK 01"),
						}
					case "testing":
						m.testProxyNode("PROXY", "HK 01")
					case "queued":
						for _, node := range []string{"AUTO", "HK 02", "JP 01", "US 01", "HK 01"} {
							m.testProxyNode("PROXY", node)
						}
					}
					defer m.stopProxyTests()
					label, foreground := m.proxyDelayLabel("PROXY", "HK 01")
					for _, interaction := range []string{"none", "hover", "focus"} {
						t.Run(interaction, func(t *testing.T) {
							current := m
							if interaction == "hover" {
								current.hover = "proxies.delay.0.1"
							} else if interaction == "focus" {
								current.focus = "proxies.delay.0.1"
							}
							card := current.proxyNode(0, 1, 50)
							var slot hitRegion
							for _, target := range card.targets {
								if target.ID == "proxies.delay.0.1" {
									slot = target
								}
							}
							if slot.X != 36 || slot.Y != 1 || slot.W != 12 || slot.H != 1 {
								t.Fatalf("delay styling moved its action: %+v", slot)
							}
							line := card.lines[1]
							if strings.Contains(line, "\x1b[4m") {
								t.Fatalf("delay state gained an underline: %q", line)
							}
							surface := panel
							if selected {
								surface = selectionBackground
							} else if interaction == "hover" {
								surface = hoverBackground
							}
							valueSurface := surface
							if interaction != "none" && state != "testing" && state != "queued" {
								valueSurface = inputBackground
							}
							padded := cellPad(strings.Repeat(" ", (12-ansi.StringWidth(label))/2)+label, 12)
							if !strings.Contains(line, themed(padded, foreground, valueSurface, false)) {
								t.Fatalf("delay lost its state color, centering or interaction background: %q", line)
							}
							plain := ansi.Strip(line)
							if ansi.StringWidth(plain) != 50 || ansi.Cut(plain, 36, 48) != padded ||
								!strings.Contains(line, themed("  ", text, surface, false)) ||
								!strings.Contains(line, themed(" ", foreground, surface, false)) {
								t.Fatalf("delay styling changed card geometry or outside padding: %q", line)
							}
						})
					}
				})
			}
		}
	}
}
