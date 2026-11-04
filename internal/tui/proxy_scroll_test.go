package tui

import (
	"fmt"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestProxyWheelScrollsPastFocusAndBack(t *testing.T) {
	for _, count := range []int{48, 300} {
		for _, width := range []int{200, 80} {
			for _, focused := range []string{"group.0", "node.0.0"} {
				t.Run(fmt.Sprintf("%d/%d/%s", count, width, focused), func(t *testing.T) {
					m := proxyHoverModel(t, count)
					m.width, m.focus = width, focused
					m.layout()
					g := dashboardGeometry(m.dashboardOptions())
					initial := m.screen
					selected := m.snapshot.Groups[0].Now
					for _, button := range []tea.MouseButton{tea.MouseWheelDown, tea.MouseWheelUp} {
						for i := 0; i < 6; i++ {
							event := tea.MouseWheelMsg{X: g.x + 8, Y: g.y + 10, Button: button}
							want := m
							if i == 0 {
								want.mouse(event)
								want.layout()
							}
							var cmd tea.Cmd
							m, cmd = update(m, event)
							if i == 0 && (m.screen != want.screen || !reflect.DeepEqual(m.targets, want.targets)) {
								t.Fatal("wheel viewport differs from a fresh full layout")
							}
							offset := 3 * (i + 1)
							if button == tea.MouseWheelUp {
								offset = 18 - offset
							}
							if cmd != nil || m.scroll != offset || m.focus != focused || m.snapshot.Groups[0].Now != selected {
								t.Fatalf("wheel changed focus/selection or lost scrolling: offset=%d focus=%s", m.scroll, m.focus)
							}
						}
						if button == tea.MouseWheelDown && m.screen == initial {
							t.Fatal("scrolling changed the offset without updating the screen")
						}
					}
				})
			}
		}
	}
}

func TestProxyWheelKeepsSidebarAndPopupOwnership(t *testing.T) {
	for _, width := range []int{200, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := proxyHoverModel(t, 48)
			m.width = width
			m.sidebarOpen = width == 80
			m.layout()
			focus := m.focus
			for _, point := range [][2]int{{2, 1}, {2, 8}} {
				m, _ = update(m, tea.MouseWheelMsg{X: point[0], Y: point[1], Button: tea.MouseWheelDown})
				if m.scroll != 0 || m.focus != focus {
					t.Fatal("scrolling the sidebar moved the proxy viewport or focus")
				}
			}
			if width == 80 {
				m, _ = update(m, tea.MouseWheelMsg{X: 50, Y: 15, Button: tea.MouseWheelDown})
				if !m.sidebarOpen || m.scroll != 0 {
					t.Fatal("popup backdrop allowed scrolling the proxy viewport")
				}
			}
		})
	}
}

func TestProxyWheelClampsWithoutMovingFocus(t *testing.T) {
	m := proxyHoverModel(t, 48)
	g := dashboardGeometry(m.dashboardOptions())
	for _, boundary := range []struct {
		offset int
		button tea.MouseButton
	}{{0, tea.MouseWheelUp}, {m.total - g.height, tea.MouseWheelDown}} {
		m.scroll = boundary.offset
		m.layout()
		screen, focus := m.screen, m.focus
		var cmd tea.Cmd
		m, cmd = update(m, tea.MouseWheelMsg{X: g.x + 8, Y: g.y + 10, Button: boundary.button})
		if cmd != nil || m.scroll != boundary.offset || m.screen != screen || m.focus != focus {
			t.Fatal("wheel at the boundary changed the viewport or keyboard focus")
		}
	}
}

func TestProxyWheelRespectsNotifications(t *testing.T) {
	m := proxyHoverModel(t, 48)
	m.message, m.job = "", ""
	m.notification.source = m.notificationSource()
	m.notification.items = []notificationItem{{ID: 1, Kind: "error", Message: "Example failure"}}
	m.layout()
	o := m.dashboardOptions()
	o.Body, o.BodyTargets = m.pageBody(dashboardGeometry(o).width)
	toasts := notificationLayout(o)
	if len(toasts) != 1 {
		t.Fatal("fixture did not expose one notification")
	}
	toast := toasts[0].Bounds
	m, _ = update(m, tea.MouseWheelMsg{X: toast.X + 1, Y: toast.Y + 1, Button: tea.MouseWheelDown})
	if m.scroll != 0 || len(m.notification.items) != 1 {
		t.Fatal("wheel over a notification scrolled the proxy viewport or dismissed the notification")
	}
	g := dashboardGeometry(m.dashboardOptions())
	m, _ = update(m, tea.MouseWheelMsg{X: g.x + 8, Y: g.y + 10, Button: tea.MouseWheelDown})
	if m.scroll != 3 || len(m.notification.items) != 1 {
		t.Fatal("notification prevented scrolling outside its own bounds")
	}
}

func TestProxyWheelKeepsSortingMenuOwnership(t *testing.T) {
	m := proxyHoverModel(t, 48)
	m.openProxySort()
	m.layout()
	if !m.menu.open || m.menu.selected != 0 {
		t.Fatal("fixture did not open the default sorting option")
	}
	bounds := m.menu.bounds
	m, _ = update(m, tea.MouseWheelMsg{X: bounds.X + 1, Y: bounds.Y + 1, Button: tea.MouseWheelDown})
	if !m.menu.open || m.menu.selected != 1 || m.scroll != 0 {
		t.Fatal("wheel did not remain inside the sorting menu")
	}
}

func BenchmarkProxyWheel(b *testing.B) {
	for _, count := range []int{48, 300} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := proxyHoverModel(b, count)
			g := dashboardGeometry(m.dashboardOptions())
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				button := tea.MouseWheelDown
				if i%2 != 0 {
					button = tea.MouseWheelUp
				}
				next, _ := m.Update(tea.MouseWheelMsg{X: g.x + 8, Y: g.y + 10, Button: button})
				m = next.(Model)
				_ = m.View()
			}
		})
	}
}
