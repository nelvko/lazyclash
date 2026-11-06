package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func rulesMouseModel(t testing.TB, count, width, height int) Model {
	t.Helper()
	backend := &fakeBackend{}
	backend.snapshot.Installed, backend.snapshot.ConfigReady = true, true
	backend.snapshot.Service = service.Status{Active: true, State: "active"}
	m := New(context.Background(), backend, "zh-Hans")
	t.Cleanup(m.cancel)
	m.snapshot, m.snapshotReady = backend.snapshot, true
	m.busy, m.job, m.logoIntro.used = false, "", true
	m.width, m.height, m.page, m.focus = width, height, "rules", "rule.0"
	m.colorProfile, m.noColor = colorprofile.TrueColor, false
	m.rules.active, m.rules.received = true, true
	rules := make([]mihomo.Rule, count)
	for i := range rules {
		rules[i] = mihomo.Rule{Index: i, Type: "RuleSet", Payload: fmt.Sprintf("%04d.中文规则.example.invalid", i), Proxy: "默认代理", Size: 100}
	}
	m.rules.snapshot = app.RuleSnapshot{State: "ready", Rules: rules}
	m.message, m.failure = "", ""
	m.notification.items = nil
	m.notification.source = m.notificationSource()
	m.reconcileRules()
	m.layout()
	return m
}

func TestRulesWindowMatchesCompleteBody(t *testing.T) {
	for _, state := range []string{"list", "filtered sparse", "empty", "error", "invalid regex", "loading", "expanded"} {
		for _, size := range [][2]int{{160, 45}, {200, 60}, {80, 24}, {60, 20}, {40, 16}, {40, 9}, {20, 7}, {5, 2}} {
			t.Run(fmt.Sprintf("%s/%v", state, size), func(t *testing.T) {
				m := rulesMouseModel(t, 30, size[0], size[1])
				switch state {
				case "filtered sparse":
					m.rules.snapshot.Rules = []mihomo.Rule{
						{Index: 95, Type: "DomainSuffix", Payload: "needle.中文.example", Proxy: "DIRECT", Size: -1},
						{Index: 1, Type: "Domain", Payload: "other.example", Proxy: "PROXY", Size: -1},
						{Index: 3, Type: "Domain", Payload: "needle.example", Proxy: "REJECT", Size: -1, Extra: &mihomo.RuleExtra{Disabled: true}},
					}
					m.rules.search.SetValue("needle")
					m.rules.selected = ruleIdentity(m.rules.snapshot.Rules[2])
					m.focus, m.hover = "rule.3", "rule.95"
				case "empty":
					m.rules.snapshot.Rules = nil
				case "error":
					m.rules.snapshot = app.RuleSnapshot{State: "unavailable"}
					m.rules.err = errors.New("fixture API unavailable")
				case "invalid regex":
					m.rules.search.SetValue("[")
					m.rules.regex = true
				case "loading":
					m.rules.received = false
				case "expanded":
					r := m.rules.snapshot.Rules[0]
					r.Payload = strings.Repeat("中文🚀.example.invalid/", 12)
					r.Extra = &mihomo.RuleExtra{Disabled: true, HitCount: 4, MissCount: 8}
					m.rules.snapshot.Rules[0] = r
					m.rules.expanded, m.rules.selected, m.focus = ruleIdentity(r), ruleIdentity(r), "rule.0"
				}
				for _, offset := range []int{0, 1, 2, 3, 17, 80, 10000} {
					m.scroll = offset
					full := m.navigationOptions()
					full.Scroll, full.Header = offset, m.pageHeader()
					g := dashboardGeometry(full)
					if full.BodyTopRows > 0 {
						full.BodyTop = m.rulesToolbar(g.width, full.BodyTopRows)
					}
					window := full
					full.Body, full.BodyTargets = m.rulesBody(g.width)
					m.populatePageBody(&window)
					want, wantHits, wantTotal := renderDashboard(full)
					got, gotHits, gotTotal := renderDashboard(window)
					if got != want || !reflect.DeepEqual(gotHits, wantHits) || gotTotal != wantTotal {
						t.Fatalf("window differs at scroll %d: total=%d want=%d", offset, gotTotal, wantTotal)
					}
					assertCanvas(t, got, size[0], size[1])
					assertRegions(t, gotHits, size[0], size[1])
				}
			})
		}
	}
}

func TestRulesMouseHoverMatchesFreshLayout(t *testing.T) {
	for _, count := range []int{670, 2000} {
		for _, size := range [][2]int{{160, 45}, {200, 60}} {
			t.Run(fmt.Sprintf("%d/%v", count, size), func(t *testing.T) {
				m := rulesMouseModel(t, count, size[0], size[1])
				for _, id := range []string{"rule.1", "rule.2", "rule.2", "", "rules.case", "rules.search", "nav.profiles"} {
					event := tea.MouseMotionMsg{X: size[0] - 1, Y: size[1] - 1}
					if id != "" {
						hit := findTarget(t, m, id)
						event.X, event.Y = hit.X+hit.W/2, hit.Y
					}
					focus, selected, generation, scroll := m.focus, m.rules.selected, m.rules.generation, m.scroll
					want := m
					want.mouse(event)
					want.layout()
					var cmd tea.Cmd
					m, cmd = update(m, event)
					if m.screen != want.screen || !reflect.DeepEqual(m.targets, want.targets) {
						t.Fatalf("hover %q differs from fresh layout", id)
					}
					if cmd != nil || m.focus != focus || m.rules.selected != selected || m.rules.generation != generation || m.scroll != scroll || m.rules.expanded != "" {
						t.Fatalf("hover %q changed focus, selection, request or viewport", id)
					}
					screen := m.screen
					m, cmd = update(m, event)
					if m.screen != screen || cmd != nil {
						t.Fatalf("same-target hover %q changed view or started work", id)
					}
				}
			})
		}
	}
}

func TestRulesWheelPreservesSelectionAndMapsNewHover(t *testing.T) {
	for _, count := range []int{670, 2000} {
		for _, size := range [][2]int{{160, 45}, {200, 60}} {
			t.Run(fmt.Sprintf("%d/%v", count, size), func(t *testing.T) {
				m := rulesMouseModel(t, count, size[0], size[1])
				g := dashboardGeometry(m.dashboardOptions())
				focus, selected, generation := m.focus, m.rules.selected, m.rules.generation
				for _, button := range []tea.MouseButton{tea.MouseWheelDown, tea.MouseWheelUp} {
					for i := 0; i < 20; i++ {
						var cmd tea.Cmd
						m, cmd = update(m, tea.MouseWheelMsg{X: g.x + 8, Y: g.y + 1, Button: button})
						offset := 3 * (i + 1)
						if button == tea.MouseWheelUp {
							offset = 60 - offset
						}
						if m.scroll != offset || m.hover != fmt.Sprintf("rule.%d", offset/3) {
							t.Fatalf("wheel lost viewport or hovered old rule: scroll=%d hover=%q want=%d", m.scroll, m.hover, offset)
						}
						if cmd != nil || m.focus != focus || m.rules.selected != selected || m.rules.generation != generation || m.rules.expanded != "" {
							t.Fatal("wheel changed focus, selection or request")
						}
					}
				}
				for _, boundary := range []struct {
					offset int
					button tea.MouseButton
				}{{0, tea.MouseWheelUp}, {m.total - g.height, tea.MouseWheelDown}} {
					m.scroll = boundary.offset
					m.renderLayout()
					m, _ = update(m, tea.MouseMotionMsg{X: g.x + 8, Y: g.y + 1})
					screen, hover := m.screen, m.hover
					var cmd tea.Cmd
					m, cmd = update(m, tea.MouseWheelMsg{X: g.x + 8, Y: g.y + 1, Button: boundary.button})
					if cmd != nil || m.scroll != boundary.offset || m.screen != screen || m.hover != hover || m.focus != focus || m.rules.selected != selected || m.rules.generation != generation {
						t.Fatal("boundary wheel changed view, focus or selection")
					}
				}
			})
		}
	}
}

func TestRulesMousePreservesOverlayOwnership(t *testing.T) {
	t.Run("sidebar", func(t *testing.T) {
		m := rulesMouseModel(t, 670, 160, 45)
		focus, selected := m.focus, m.rules.selected
		m, _ = update(m, tea.MouseWheelMsg{X: 2, Y: 8, Button: tea.MouseWheelDown})
		if m.scroll != 0 || m.focus != focus || m.rules.selected != selected {
			t.Fatal("sidebar wheel moved rules or keyboard focus")
		}
	})
	t.Run("popup backdrop", func(t *testing.T) {
		m := rulesMouseModel(t, 670, 80, 24)
		m.sidebarOpen = true
		m.layout()
		focus, selected := m.focus, m.rules.selected
		g := dashboardGeometry(m.dashboardOptions())
		m, _ = update(m, tea.MouseWheelMsg{X: 50, Y: g.y + 1, Button: tea.MouseWheelDown})
		if !m.sidebarOpen || m.scroll != 0 || m.focus != focus || m.rules.selected != selected || strings.HasPrefix(m.hover, "rule.") {
			t.Fatal("popup backdrop allowed wheel into rules")
		}
	})
	t.Run("menu", func(t *testing.T) {
		m := rulesMouseModel(t, 670, 160, 45)
		m.menu.show([]menuItem{{ID: "first", Label: "First"}, {ID: "second", Label: "Second"}}, 30, 10)
		m.renderLayout()
		bounds := m.menu.bounds
		event := tea.MouseWheelMsg{X: bounds.X + 1, Y: bounds.Y + 1, Button: tea.MouseWheelDown}
		if m.updateRulesMouse(event) {
			t.Fatal("rules fast path intercepted menu wheel")
		}
		m.mouse(event)
		if !m.menu.open || m.menu.selected != 1 || m.scroll != 0 {
			t.Fatal("menu did not retain wheel ownership")
		}
	})
	t.Run("notification", func(t *testing.T) {
		m := rulesMouseModel(t, 670, 160, 45)
		m.notification.items = []notificationItem{{ID: 1, Kind: "error", Message: "Fixture notification"}}
		m.layout()
		o := m.navigationOptions()
		o.Scroll = m.scroll
		m.populatePageBody(&o)
		toasts := notificationLayout(o)
		if len(toasts) != 1 {
			t.Fatal("fixture did not expose one notification")
		}
		focus, selected, generation := m.focus, m.rules.selected, m.rules.generation
		toast := toasts[0].Bounds
		m, _ = update(m, tea.MouseWheelMsg{X: toast.X + 1, Y: toast.Y, Button: tea.MouseWheelDown})
		if m.scroll != 0 || len(m.notification.items) != 1 || m.focus != focus || m.rules.selected != selected || m.rules.generation != generation {
			t.Fatal("notification wheel affected rules or dismissed notification")
		}
		g := dashboardGeometry(m.dashboardOptions())
		m, _ = update(m, tea.MouseWheelMsg{X: g.x + 8, Y: g.y + 10, Button: tea.MouseWheelDown})
		if m.scroll != 3 || len(m.notification.items) != 1 || m.focus != focus || m.rules.selected != selected {
			t.Fatal("notification blocked wheel outside its bounds")
		}
	})
}

func TestRulesNotificationInputsFollowLogicalScroll(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {40, 9}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := rulesMouseModel(t, 670, size[0], size[1])
			m.scroll = 30
			m.renderLayout()
			o := m.navigationOptions()
			o.Scroll = m.scroll
			m.populatePageBody(&o)
			g := dashboardGeometry(o)
			inputs := notificationInputs(o, g)
			if o.BodyTopRows == 0 {
				if len(inputs) != 0 {
					t.Fatalf("scrolled-off search still reserves notification space: %+v", inputs)
				}
			} else if len(inputs) != 1 || inputs[0] != findTarget(t, m, "rules.search") {
				t.Fatalf("fixed search lost notification protection: %+v", inputs)
			}
		})
	}
}

var rulesMouseBenchmarkView tea.View

func BenchmarkRulesMouse(b *testing.B) {
	for _, count := range []int{670, 2000} {
		for _, size := range [][2]int{{160, 45}, {200, 60}} {
			for _, mode := range []string{"motion-change", "motion-same", "wheel"} {
				b.Run(fmt.Sprintf("%d/%dx%d/%s", count, size[0], size[1], mode), func(b *testing.B) {
					m := rulesMouseModel(b, count, size[0], size[1])
					g := dashboardGeometry(m.dashboardOptions())
					m, _ = update(m, tea.MouseMotionMsg{X: g.x + 8, Y: g.y})
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						var msg tea.Msg
						switch mode {
						case "motion-change":
							msg = tea.MouseMotionMsg{X: g.x + 8, Y: g.y + 3*((i+1)%2)}
						case "motion-same":
							msg = tea.MouseMotionMsg{X: g.x + 8, Y: g.y}
						case "wheel":
							button := tea.MouseWheelDown
							if i%2 != 0 {
								button = tea.MouseWheelUp
							}
							msg = tea.MouseWheelMsg{X: g.x + 8, Y: g.y + 1, Button: button}
						}
						next, _ := m.Update(msg)
						m = next.(Model)
						rulesMouseBenchmarkView = m.View()
					}
				})
			}
		}
	}
}
