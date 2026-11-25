package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type proxyDirectBackend struct{ *proxiesPTYBackend }

func (b *proxyDirectBackend) SetMode(ctx context.Context, mode string) error {
	if err := b.proxiesPTYBackend.SetMode(ctx, mode); err != nil {
		return err
	}
	b.mu.Lock()
	b.snapshot.Generation += "-" + mode
	b.mu.Unlock()
	return nil
}

func (b *proxyDirectBackend) ProxyProviders(ctx context.Context) (app.ProxyProviderSnapshot, error) {
	return app.ProxyProviderSnapshot{State: "ready"}, ctx.Err()
}

func (b *proxyDirectBackend) UpdateProxyProvider(ctx context.Context, _ string) error {
	return ctx.Err()
}

func proxyDirectModel(t *testing.T, width int) (Model, *proxyDirectBackend) {
	t.Helper()
	m, original := proxiesModel(t, width, 45)
	b := &proxyDirectBackend{proxiesPTYBackend: original}
	m.backend = b
	return m, b
}

func proxyDirectSetMode(t *testing.T, m Model, mode string) Model {
	t.Helper()
	cmd := m.activate("mode." + mode)
	if cmd == nil || !m.busy {
		t.Fatalf("mode %s did not use the shared mode action", mode)
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.snapshot.Mode.Actual != mode {
		t.Fatalf("mode %s was not confirmed: busy=%t mode=%+v", mode, m.busy, m.snapshot.Mode)
	}
	return m
}

func TestProxiesDirectModeHidesBrowsingAndKeepsModeAndProviderControls(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{40, 80, 160} {
			t.Run(fmt.Sprintf("%s/%d", language, width), func(t *testing.T) {
				m, _ := proxyDirectModel(t, width)
				m.locale = i18n.New(language)
				m.focus = "node.0.0"
				m.browseProxyReference("AUTO")
				m.proxies.sorts["AUTO"] = "delay"
				m = proxyDirectSetMode(t, m, "direct")
				body, _ := m.proxiesBody(dashboardBodyWidth(m.dashboardOptions()))
				plain := ansi.Strip(body)
				if strings.TrimSpace(plain) == "" || strings.Contains(plain, m.tr("proxies.no_groups")) {
					t.Fatalf("Direct mode did not display its own empty state: %q", plain)
				}
				if !strings.HasSuffix(strings.TrimSpace(plain), m.tr("proxies.direct.description")) {
					t.Fatalf("Direct mode did not end with its single-line description: %q", plain)
				}
				for _, name := range []string{"PROXY", "AUTO", "HK 02"} {
					if strings.Contains(plain, name) {
						t.Fatalf("Direct mode leaked browsing content %s: %q", name, plain)
					}
				}
				if groups := m.visibleProxyGroups(); len(groups) != 0 {
					t.Fatalf("Direct mode retained visible groups: %v", groups)
				}
				if m.dashboardOptions().BodyRange {
					t.Fatal("Direct mode reserved the hidden node-range footer")
				}
				for _, target := range m.contentTargets() {
					if strings.HasPrefix(target.ID, "group.") || strings.HasPrefix(target.ID, "node.") ||
						strings.HasPrefix(target.ID, "proxies.") && target.ID != "proxies.providers" {
						t.Fatalf("Direct mode retained a browsing control: %+v", target)
					}
				}
				for _, id := range []string{"mode.rule", "mode.global", "mode.direct", "proxies.providers"} {
					findTarget(t, m, id)
				}
				if cmd := m.activate("proxies.providers"); cmd == nil || !m.providers.open {
					t.Fatal("Direct mode disabled the proxy provider panel")
				}
				m.closeProviders()
			})
		}
	}
}

func TestProxiesDirectModeEmptyStateDoesNotDependOnReturnedGroups(t *testing.T) {
	m, _ := proxyDirectModel(t, 160)
	m = proxyDirectSetMode(t, m, "direct")
	if m.dashboardOptions().BodyRange {
		t.Fatal("suspended expanded groups reserved the node-range footer in Direct mode")
	}
	width := dashboardBodyWidth(m.dashboardOptions())
	withGroups, _ := m.proxiesBody(width)
	m.snapshot.Groups, m.snapshot.Proxies = nil, nil
	m.syncProxies()
	withoutGroups, hits := m.proxiesBody(width)
	if strings.TrimSpace(ansi.Strip(withoutGroups)) == "" || withGroups != withoutGroups || len(hits) != 0 {
		t.Fatalf("missing core groups changed the Direct empty state: with=%q without=%q hits=%+v", ansi.Strip(withGroups), ansi.Strip(withoutGroups), hits)
	}
	if strings.TrimSpace(ansi.Strip(withoutGroups)) != m.tr("proxies.direct.description") {
		t.Fatalf("Direct empty state contains more than its single description: %q", ansi.Strip(withoutGroups))
	}
}

func TestProxiesDirectModePreservesObservedModeDiagnostics(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, state := range []struct{ desired, actual, err string }{
			{"rule", "direct", ""},
			{"direct", "direct", "mode observation failed"},
			{"direct", "", ""},
			{"direct", "rule", ""},
		} {
			t.Run(fmt.Sprintf("%s/%s/%s/%s", language, state.desired, state.actual, state.err), func(t *testing.T) {
				m, _ := proxyDirectModel(t, 160)
				m.locale = i18n.New(language)
				m.snapshot.Mode = app.ModeStatus{Desired: state.desired, Actual: state.actual, Error: state.err}
				m.syncProxies()
				body, _ := m.proxiesBody(120)
				actual := m.tr("dashboard.status.unknown")
				if state.actual != "" && state.err == "" {
					actual = dashboardModeLabel(state.actual, m.locale)
				}
				if !strings.Contains(ansi.Strip(body), m.tr("dashboard.settings.actualMode", actual)) {
					t.Fatalf("Direct mode hid the observed-mode diagnostic: %q", ansi.Strip(body))
				}
				if state.err != "" && !strings.Contains(ansi.Strip(body), state.err) {
					t.Fatal("Direct mode hid the mode observation failure")
				}
				if state.actual == "rule" && len(m.visibleProxyGroups()) == 0 {
					t.Fatal("saved Direct preference hid groups while the running mode remained Rule")
				}
			})
		}
	}
}

func TestProxiesDirectRoundTripRestoresBrowsingAcrossGenerationChanges(t *testing.T) {
	for _, mode := range []string{"rule", "global"} {
		for _, width := range []int{80, 160} {
			for _, view := range []string{"directory", "members", "reference"} {
				t.Run(fmt.Sprintf("%s/%d/%s", mode, width, view), func(t *testing.T) {
					m, b := proxyDirectModel(t, width)
					if mode == "global" {
						m = proxyDirectSetMode(t, m, mode)
					}
					group, gi := "PROXY", 0
					if mode == "global" {
						group, gi = "GLOBAL", 8
					}
					setProxyTestGroup(&m, "")
					m.focus = fmt.Sprintf("group.%d", gi)
					if view != "directory" {
						m.expandProxyGroup(group)
						m.focus = fmt.Sprintf("node.%d.0", gi)
						m.proxies.sorts[group] = "name"
					}
					if view == "members" && width >= sidebarMinWidth {
						m.expandProxyGroup("AUTO")
					}
					if view == "reference" {
						m.browseProxyReference("AUTO")
					}
					m.layout()
					location := m.proxyLocation()
					history := append([]proxyLocation(nil), m.proxies.history...)
					sorts := make(map[string]string)
					for group, order := range m.proxies.sorts {
						sorts[group] = order
					}
					generation := m.snapshot.Generation
					selected := b.selectedLocked()
					m = proxyDirectSetMode(t, m, "direct")
					m = proxyDirectSetMode(t, m, mode)
					if m.snapshot.Generation == generation {
						t.Fatal("fixture did not change the configuration generation")
					}
					if !reflect.DeepEqual(m.proxyLocation(), location) || !reflect.DeepEqual(m.proxies.history, history) ||
						!reflect.DeepEqual(m.proxies.sorts, sorts) {
						t.Fatalf("Direct round trip changed browsing context: location=%+v want=%+v history=%+v sorts=%v", m.proxyLocation(), location, m.proxies.history, m.proxies.sorts)
					}
					if !reflect.DeepEqual(b.selectedLocked(), selected) || b.selections != 0 {
						t.Fatal("Direct round trip changed selected nodes")
					}
				})
			}
		}
	}
}

func TestProxiesExplicitModeSwitchKeepsActivatedControlFocused(t *testing.T) {
	for _, width := range []int{40, 160} {
		for _, input := range []string{"mouse", "keyboard"} {
			t.Run(fmt.Sprintf("%s/%d", input, width), func(t *testing.T) {
				m, _ := proxiesModel(t, width, 12)
				setProxyTestGroup(&m, "PROXY")
				m.focus, m.scroll = "mode.global", 100
				m.layout()
				scroll := m.scroll
				if scroll == 0 {
					t.Fatal("fixture has no scrolled members")
				}
				for _, mode := range []string{"direct", "rule", "global"} {
					id := "mode." + mode
					var cmd tea.Cmd
					if input == "mouse" {
						m, cmd = click(m, findTarget(t, m, id))
					} else {
						for range len(m.focusTargets()) {
							if m.focus == id {
								break
							}
							m, _ = update(m, special(tea.KeyTab))
						}
						if m.focus != id {
							t.Fatalf("Tab did not reach %s", id)
						}
						m, cmd = update(m, special(tea.KeyEnter))
					}
					m, _ = update(m, actionCmd(t, cmd)())
					if m.focus != id || m.snapshot.Mode.Actual != mode {
						t.Fatalf("activated %s, got focus %q and mode %q", id, m.focus, m.snapshot.Mode.Actual)
					}
					if mode == "rule" && (m.scroll != scroll || !m.proxyGroupExpanded("PROXY")) {
						t.Fatalf("return to Rule lost the original list: scroll=%d (want %d), expanded=%v", m.scroll, scroll, m.proxies.expanded)
					}
					m, cmd = update(m, special(tea.KeyEnter))
					m, _ = update(m, actionCmd(t, cmd)())
					if m.snapshot.Mode.Actual != mode || m.focus != id {
						t.Fatalf("repeated Enter changed the last choice: mode=%s focus=%s", m.snapshot.Mode.Actual, m.focus)
					}
				}
			})
		}
	}
}

func TestProxiesBackgroundModeObservationRestoresBrowsingFocus(t *testing.T) {
	m, _ := proxiesModel(t, 160, 24)
	m.focus = "node.0.2"
	m.layout()
	changeMode := func(mode string) {
		snapshot := m.snapshot
		snapshot.Mode.Desired, snapshot.Mode.Actual = mode, mode
		m, _ = update(m, coreStatusMsg{generation: m.statusGeneration, snapshot: snapshot})
	}
	changeMode("direct")
	// A focus change alone did not request a switch. A live observation must
	// still restore the browsing position saved before entering Direct.
	m.focus = "mode.rule"
	m.layout()
	changeMode("rule")
	if m.focus != "node.0.2" || !m.proxyGroupExpanded("PROXY") {
		t.Fatalf("focus on an unactivated control displaced browsing: focus=%s expanded=%v", m.focus, m.proxies.expanded)
	}
}

func TestProxiesDirectReturnDoesNotRestoreAnotherSource(t *testing.T) {
	for _, source := range []string{"profile", "process", "version"} {
		t.Run(source, func(t *testing.T) {
			m, b := proxyDirectModel(t, 160)
			m.focus = "node.0.0"
			m.browseProxyReference("AUTO")
			m.proxies.sorts["AUTO"] = "name"
			m = proxyDirectSetMode(t, m, "direct")
			b.mu.Lock()
			switch source {
			case "profile":
				b.snapshot.Applied += "-new"
			case "process":
				b.snapshot.Service.PID++
			case "version":
				b.snapshot.RunningVersion += "-new"
			}
			b.mu.Unlock()
			m = proxyDirectSetMode(t, m, "rule")
			if m.hasExpandedProxyGroups() || m.proxies.activeGroup != "" || len(m.proxies.history) != 0 ||
				m.proxySort("AUTO") != "default" {
				t.Fatalf("%s change restored an obsolete browsing context: %+v", source, m.proxies)
			}
		})
	}
}

func TestProxiesDirectModeRejectsStaleBrowsingActionsAndTests(t *testing.T) {
	m, b := proxyDirectModel(t, 160)
	m.focus = "node.0.0"
	m.browseProxyReference("AUTO")
	m = proxyDirectSetMode(t, m, "direct")
	location := m.proxyLocation()
	history := append([]proxyLocation(nil), m.proxies.history...)
	for _, id := range []string{
		"group.0", "node.0.1", "proxies.reference.0.0", "proxies.delay.0.1",
		"proxies.sort.0", "proxies.test.0", "proxies.collapse_all",
		"proxies.back", "proxies.directory",
	} {
		if cmd := m.activate(id); cmd != nil || m.busy || m.menu.open {
			t.Fatalf("stale %s started a browsing action in Direct mode", id)
		}
		if !reflect.DeepEqual(m.proxyLocation(), location) || !reflect.DeepEqual(m.proxies.history, history) {
			t.Fatalf("stale %s changed the suspended browsing context", id)
		}
	}
	if m.testProxyGroup() != nil || m.testProxyNode("PROXY", "HK 01") != nil || m.proxies.testing ||
		len(m.proxies.nodeTests.running) != 0 || b.selections != 0 || b.tests != 0 {
		t.Fatal("Direct mode allowed node selection or a latency test")
	}
}

func TestProxiesDirectModeCancelsPendingTestsAndRejectsTheirResults(t *testing.T) {
	m, _ := proxyDirectModel(t, 160)
	m.focus = "group.0"
	if cmd := m.testProxyGroup(); cmd == nil || !m.proxies.testing {
		t.Fatal("fixture did not start the group test")
	}
	groupContext := m.proxies.testCtx
	groupResult := proxyTestMessage(m, "PROXY", "HK 01", 777)
	if cmd := m.testProxyNode("AUTO", "HK 01"); cmd == nil {
		t.Fatal("fixture did not start the independent node test")
	}
	request := m.proxies.nodeTests.running[proxyResultKey("AUTO", "HK 01")]
	nodeResult := proxyNodeDelayMsg{
		generation: request.generation, target: request.target,
		result: mihomo.ProxyDelay{State: "success", Delay: 888},
	}
	// A mode change observed from another client must cancel work too.
	m.snapshot.Mode.Actual, m.snapshot.Mode.Desired = "direct", "direct"
	m.syncProxies()
	if groupContext.Err() != context.Canceled || m.proxies.testing ||
		len(m.proxies.nodeTests.running) != 0 || len(m.proxies.nodeTests.queue) != 0 {
		t.Fatal("observed Direct mode retained pending latency tests")
	}
	m, _ = update(m, groupResult)
	m, _ = update(m, nodeResult)
	for _, key := range []string{proxyResultKey("PROXY", "HK 01"), proxyResultKey("AUTO", "HK 01")} {
		if _, exists := m.proxies.results[key]; exists {
			t.Fatalf("Direct mode accepted a cancelled latency result: %q", key)
		}
	}
}

func TestProxiesDirectReturnRestoresLocationBeforeInterruptedSortMenu(t *testing.T) {
	m, _ := proxyDirectModel(t, 160)
	m.focus = "node.0.2"
	m.layout()
	location := m.proxyLocation()
	m.openProxySort()
	if !m.menu.open || !m.proxies.sortMenu || m.proxies.sortReturn.focus != location.focus || m.focus == location.focus {
		t.Fatal("fixture did not move focus from a node into its sort menu")
	}
	m.snapshot.Mode.Actual, m.snapshot.Mode.Desired = "direct", "direct"
	m.syncProxies()
	m.layout()
	if m.menu.open || m.proxies.sortMenu || m.proxies.sortReturn.focus != "" {
		t.Fatal("Direct mode retained the interrupted sort menu")
	}
	m.snapshot.Mode.Actual, m.snapshot.Mode.Desired = "rule", "rule"
	m.syncProxies()
	m.layout()
	if !reflect.DeepEqual(m.proxyLocation(), location) {
		t.Fatalf("Direct return restored the temporary menu anchor instead of its originating node: got=%+v want=%+v", m.proxyLocation(), location)
	}
}

func TestProxiesDirectReturnRestoresScrollWhileNavigationKeepsFocus(t *testing.T) {
	m := proxyHoverModel(t, 120)
	m.height = 24
	m.focus = "node.0.95"
	m.revealContentFocus()
	m.layout()
	m.returnToNavigation()
	m.layout()
	scroll, focus := m.scroll, m.focus
	if scroll <= 0 || focus != "nav.proxies" || m.pageViews["proxies"].scroll != scroll {
		t.Fatalf("fixture did not retain a scrolled proxy view under navigation focus: scroll=%d focus=%s saved=%+v", scroll, focus, m.pageViews["proxies"])
	}
	m.snapshot.Mode.Actual, m.snapshot.Mode.Desired = "direct", "direct"
	m.syncProxies()
	m.layout()
	if m.focus != focus || m.scroll != 0 {
		t.Fatalf("Direct fixture changed navigation ownership or retained hidden scrolling: focus=%s scroll=%d", m.focus, m.scroll)
	}
	m.snapshot.Mode.Actual, m.snapshot.Mode.Desired = "rule", "rule"
	m.syncProxies()
	m.layout()
	if m.focus != focus || m.scroll != scroll || m.pageViews["proxies"].scroll != scroll {
		t.Fatalf("Direct return lost the scroll position while navigation retained focus: focus=%s scroll=%d saved=%d want=%d", m.focus, m.scroll, m.pageViews["proxies"].scroll, scroll)
	}
}

func TestProxiesDirectMessageCentersInRemainingBody(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{40, 80, 160} {
			for _, height := range []int{16, 24, 45} {
				for _, hideHints := range []bool{false, true} {
					for _, diagnostic := range []string{"none", "mismatch", "error"} {
						t.Run(fmt.Sprintf("%s/%dx%d/hideHints=%t/%s", language, width, height, hideHints, diagnostic), func(t *testing.T) {
							m, _ := proxyDirectModel(t, width)
							m.locale, m.height, m.keyHints.hidden = i18n.New(language), height, hideHints
							m = proxyDirectSetMode(t, m, "direct")
							var notices []string
							switch diagnostic {
							case "mismatch":
								m.snapshot.Mode.Desired = "rule"
								notices = append(notices, m.tr("dashboard.settings.actualMode", dashboardModeLabel("direct", m.locale)))
							case "error":
								m.snapshot.Mode.Error = "mode observation failed"
								notices = append(notices, m.snapshot.Mode.Error, m.tr("dashboard.settings.actualMode", m.tr("dashboard.status.unknown")))
							}
							m.layout()
							g := dashboardGeometry(m.dashboardOptions())
							body, _ := m.proxiesBody(g.width)
							plain := ansi.Strip(body)
							description := m.tr("proxies.direct.description")
							if strings.Count(plain, description) != 1 {
								t.Fatalf("Direct body did not contain exactly one description: %q", plain)
							}
							lines := strings.Split(plain, "\n")
							if len(lines) > g.height || m.scroll != 0 {
								t.Fatalf("Direct empty state overflowed its available height: rows=%d height=%d scroll=%d", len(lines), g.height, m.scroll)
							}
							prefixRows := 0 // Fixed mode controls precede the scrolling body.
							for _, notice := range notices {
								wrapped := strings.Split(wrapText(notice, g.width), "\n")
								for i, want := range wrapped {
									if prefixRows+i >= len(lines) || strings.TrimSpace(lines[prefixRows+i]) != want {
										t.Fatalf("mode diagnostic moved out of its leading rows: notice=%q row=%d body=%q", want, prefixRows+i, plain)
									}
								}
								prefixRows += len(wrapped)
							}
							messageX, messageY := -1, -1
							for y, line := range lines {
								if index := strings.Index(line, description); index >= 0 {
									messageX, messageY = ansi.StringWidth(line[:index]), y
									if strings.TrimSpace(line) != description {
										t.Fatalf("Direct description shared its row with unrelated text: %q", line)
									}
								}
							}
							left, right := messageX, g.width-messageX-ansi.StringWidth(description)
							top, bottom := messageY-prefixRows, g.height-messageY-1
							if min(left, right, top, bottom) < 0 || max(left-right, right-left) > 1 || max(top-bottom, bottom-top) > 1 {
								t.Fatalf("Direct description is not centered in the remaining body: x=%d y=%d width=%d height=%d prefix=%d left/right=%d/%d top/bottom=%d/%d", messageX, messageY, g.width, g.height, prefixRows, left, right, top, bottom)
							}
							screen := ansi.Strip(m.screen)
							if strings.Count(screen, description) != 1 {
								t.Fatal("rendered screen duplicated or clipped the Direct description")
							}
							screenLine := strings.Split(screen, "\n")[g.y+messageY]
							index := strings.Index(screenLine, description)
							if index < 0 || ansi.StringWidth(screenLine[:index]) != g.x+messageX {
								t.Fatalf("screen placement disagrees with the centered body coordinates: %q", screenLine)
							}
							findTarget(t, m, "proxies.providers")
							if diagnostic != "error" {
								for _, mode := range []string{"rule", "global", "direct"} {
									findTarget(t, m, "mode."+mode)
								}
							}
							assertCanvas(t, m.screen, width, height)
							assertRegions(t, m.targets, width, height)
						})
					}
				}
			}
		}
	}
}
