package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestProxiesLatencyPaletteHighlightsFastAndSlowSuccessfulSamples(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	for _, tc := range []struct {
		state string
		delay int
		color string
	}{
		{"success", 42, success}, {"success", 149, success}, {"success", 150, warning},
		{"timeout", 0, muted}, {"failed", 0, muted},
	} {
		m.proxies.results[proxyResultKey("PROXY", "HK 01")] = proxyMeasurement{
			result: mihomo.ProxyDelay{State: tc.state, Delay: tc.delay}, path: m.proxyPathKey("HK 01"),
		}
		if _, color := m.proxyDelayLabel("PROXY", "HK 01"); color != tc.color {
			t.Errorf("state=%s delay=%d color=%s, want %s", tc.state, tc.delay, color, tc.color)
		}
	}
}

func TestProxiesDelayInteractionStylePreservesStateAndGeometry(t *testing.T) {
	states := []struct {
		name, result string
		delay        int
		emphasized   string
	}{
		{name: "untested", emphasized: proxyNodeEmphasizedMeta},
		{name: "fast", result: "success", delay: 149, emphasized: proxyNodeEmphasizedFast},
		{name: "slow", result: "success", delay: 150, emphasized: proxyNodeEmphasizedSlow},
		{name: "timeout", result: "timeout", emphasized: proxyNodeEmphasizedMeta},
		{name: "failed", result: "failed", emphasized: proxyNodeEmphasizedMeta},
		{name: "testing", emphasized: accent},
		{name: "queued", emphasized: accent},
	}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, state := range states {
			for _, selected := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/selected=%t", language, state.name, selected), func(t *testing.T) {
					m, _ := proxiesModel(t, 160, 40)
					m.locale = i18n.New(language)
					proxy := m.snapshot.Proxies["HK 01"]
					proxy.History = nil
					m.snapshot.Proxies["HK 01"] = proxy
					if selected {
						m.snapshot.Groups[0].Now = "HK 01"
					}
					switch {
					case state.result != "":
						m.proxies.results[proxyResultKey("PROXY", "HK 01")] = proxyMeasurement{
							result: mihomo.ProxyDelay{State: state.result, Delay: state.delay},
							path:   m.proxyPathKey("HK 01"),
						}
					case state.name == "testing":
						m.testProxyNode("PROXY", "HK 01")
					case state.name == "queued":
						for _, node := range []string{"AUTO", "HK 02", "JP 01", "US 01", "HK 01"} {
							m.testProxyNode("PROXY", node)
						}
					}
					defer m.stopProxyTests()
					label, foreground := m.proxyDelayLabel("PROXY", "HK 01")
					for _, interaction := range []string{"none", "card-hover", "delay-hover", "delay-focus"} {
						t.Run(interaction, func(t *testing.T) {
							current := m
							switch interaction {
							case "card-hover":
								current.hover = "node.0.1"
							case "delay-hover":
								current.hover = "proxies.delay.0.1"
							case "delay-focus":
								current.focus = "proxies.delay.0.1"
							}
							card := current.proxyNode(0, 1, 50)
							var slot hitRegion
							for _, target := range card.targets {
								if target.ID == "proxies.delay.0.1" {
									slot = target
								}
							}
							if slot.X != 39 || slot.Y != 1 || slot.W != 10 || slot.H != 1 {
								t.Fatalf("delay styling moved its action: %+v", slot)
							}
							line := card.lines[1]
							if strings.Contains(line, "\x1b[4m") {
								t.Fatalf("delay state gained an underline: %q", line)
							}
							surface := panel
							if selected {
								surface = proxyNodeSelectedBackground
							} else if interaction == "card-hover" || interaction == "delay-hover" {
								surface = raised
							}
							metaColor, delayColor, valueSurface := muted, foreground, surface
							if surface != panel {
								metaColor, delayColor = proxyNodeEmphasizedMeta, state.emphasized
							}
							if (interaction == "delay-hover" || interaction == "delay-focus") && state.name != "testing" && state.name != "queued" {
								delayColor, valueSurface = foreground, inputBackground
							}
							metaRow := uv.NewScreenBuffer(50, 1)
							uv.NewStyledString(card.lines[2]).Draw(metaRow, metaRow.Bounds())
							first := metaRow.CellAt(2, 0)
							wantMeta := uv.Style{Fg: lipgloss.Color(metaColor), Bg: lipgloss.Color(surface)}
							if first == nil || first.Content != "S" || !first.Style.Equal(&wantMeta) {
								t.Fatalf("Shadowsocks protocol label: got %+v, want style %+v", first, wantMeta)
							}
							padded := strings.Repeat(" ", slot.W-ansi.StringWidth(label)) + label
							if !strings.Contains(line, themed(padded, delayColor, valueSurface, false)) {
								t.Fatalf("delay lost its state color, alignment or interaction background: %q", line)
							}
							plain := ansi.Strip(line)
							if ansi.StringWidth(plain) != 50 || ansi.Cut(plain, slot.X, slot.X+slot.W) != padded ||
								!strings.Contains(line, themed(" ", text, surface, false)) ||
								!strings.Contains(line, themed(" ", delayColor, surface, false)) {
								t.Fatalf("delay styling changed card geometry or outside padding: %q", line)
							}
						})
					}
				})
			}
		}
	}
}
