package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNotificationLeavesProxySearchVisible(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 18}, {40, 9}, {20, 7}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m, _ := proxiesModel(t, size[0], size[1])
			m, _ = update(m, key("/"))
			search := findTarget(t, m, "proxies.search")
			m.notification.items = []notificationItem{{ID: 1, Page: "proxies", Kind: "error", Message: strings.Repeat("Proxy test failed. ", 8)}}
			o := m.navigationOptions()
			o.Scroll = m.scroll
			m.populatePageBody(&o)
			inputs := notificationInputs(o, dashboardGeometry(o))
			if len(inputs) != 1 || inputs[0] != search {
				t.Fatalf("proxy search lost notification protection: got %+v, want %+v", inputs, search)
			}
			for _, operation := range []string{"proxy.test", "mode.direct"} {
				m.notification.items[0].Key = operation
				layers := notificationLayout(o)
				if len(layers) == 0 {
					t.Fatal("failure lost its notification entry point")
				}
				for _, layer := range layers {
					if regionsOverlap(layer.Bounds, search) {
						t.Fatalf("%s notification covers proxy search: %+v", operation, layer.Bounds)
					}
				}
				m.layout()
				if got := findTarget(t, m, "proxies.search"); got != search {
					t.Fatalf("%s notification moved or removed proxy search: %+v", operation, got)
				}
			}
		})
	}
}

func TestProxyModeErrorKeepsTheNextModeClickable(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {80, 24}, {40, 16}, {20, 7}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m, backend := proxyDirectModel(t, size[0])
			m.height = size[1]
			m.layout()
			ids := []string{"mode.rule", "mode.global", "mode.direct", "proxies.providers"}
			var anchors []hitRegion
			for _, id := range ids {
				anchors = append(anchors, findTarget(t, m, id))
			}
			m, _ = click(m, anchors[2])
			m, _ = update(m, resultMsg{id: m.id, err: errors.New("mode switch failed"), hasSnapshot: true, snapshot: m.snapshot})
			o := m.navigationOptions()
			m.populatePageBody(&o)
			if layers := notificationLayout(o); len(layers) == 0 {
				t.Fatal("mode failure should remain available")
			}
			for _, anchor := range anchors {
				if got := findTarget(t, m, anchor.ID); got != anchor {
					t.Fatalf("mode feedback displaced or covered %s: got %+v, want %+v", anchor.ID, got, anchor)
				}
			}
			m, cmd := click(m, anchors[1])
			if !m.busy || cmd == nil {
				t.Fatal("could not switch mode while its previous failure remained visible")
			}
			m, _ = update(m, actionCmd(t, cmd)())
			if m.snapshot.Mode.Actual != "global" || backend.modes != 1 {
				t.Fatalf("the next click changed the wrong mode: %+v", m.snapshot.Mode)
			}
		})
	}
}
