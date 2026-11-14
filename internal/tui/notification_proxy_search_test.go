package tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestNotificationLeavesProxySearchVisible(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 18}, {40, 9}} {
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
			layers := notificationLayout(o)
			if len(layers) == 0 {
				t.Fatal("failure lost its notification entry point")
			}
			for _, layer := range layers {
				if regionsOverlap(layer.Bounds, search) {
					t.Fatalf("notification covers proxy search: %+v", layer.Bounds)
				}
			}
			m.layout()
			if got := findTarget(t, m, "proxies.search"); got != search {
				t.Fatalf("notification moved or removed proxy search: %+v", got)
			}
		})
	}
}
