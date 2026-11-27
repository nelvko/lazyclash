package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

type headerProviderBackend struct{ *fakeBackend }

func (b *headerProviderBackend) RuleProviders(context.Context) (app.RuleProviderSnapshot, error) {
	return app.RuleProviderSnapshot{State: "ready"}, nil
}

func (b *headerProviderBackend) UpdateRuleProvider(context.Context, string) error { return nil }

func (b *headerProviderBackend) ProxyProviders(context.Context) (app.ProxyProviderSnapshot, error) {
	return app.ProxyProviderSnapshot{State: "ready"}, nil
}

func (b *headerProviderBackend) UpdateProxyProvider(context.Context, string) error { return nil }

func headerModel(page string, width, height int) Model {
	profiles := profileFixtures("default", "alpha", "beta", "gamma", "delta", "epsilon")
	m := ready(&fakeBackend{snapshot: app.Snapshot{
		Installed: true, Applied: "default", Profiles: []string{"alpha", "beta", "gamma", "delta", "epsilon"},
		ProfileDetails: profiles,
		Service:        service.Status{Active: true, State: "active"},
		Groups:         []mihomo.Group{{Name: "PROXY", Type: "Selector"}, {Name: "AUTO", Type: "URLTest"}},
	}})
	m.page, m.width, m.height = page, width, height
	m.backend = &headerProviderBackend{fakeBackend: m.backend.(*fakeBackend)}
	m.runtime.received = true
	m.runtime.snapshot = app.RuntimeSnapshot{State: "ready", Connections: &mihomo.ConnectionSnapshot{Connections: []mihomo.Connection{{ID: "one"}, {ID: "two"}}}}
	m.rules.received = true
	m.rules.snapshot = app.RuleSnapshot{State: "ready", Rules: sampleRules()}
	m.logs.paused = true
	m.layout()
	return m
}

func TestPageHeadersUsePageActionsAcrossSizesAndFocus(t *testing.T) {
	for page, actions := range map[string][]string{
		"proxies":     {"proxies.providers"},
		"profiles":    {"profiles.update-all"},
		"connections": {"conn.close-all"},
		"logs":        {"logs.clear", "logs.follow"},
	} {
		for _, size := range [][2]int{{20, 7}, {30, 12}, {60, 20}, {70, 20}, {80, 24}, {120, 32}, {160, 45}, {216, 55}} {
			for _, focused := range actions {
				t.Run(fmt.Sprintf("%s/%dx%d/%s", page, size[0], size[1], focused), func(t *testing.T) {
					m := headerModel(page, size[0], size[1])
					m.focus = focused
					m.layout()
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
					if page == "logs" && (hasAction(m.targets, "logs.retry") || hasAction(m.focusTargets(), "logs.retry")) {
						t.Fatal("logs retained a visible or tabbable reconnect button")
					}
					g := dashboardGeometry(dashboardOptions{Width: m.width, Height: m.height, Page: page})
					lines := strings.Split(m.screen, "\n")
					if strings.Contains(ansi.Strip(strings.Join(lines[:g.y], "\n")), "Mihomo ·") {
						t.Fatal("page retained the repeated core summary")
					}
					if size[0] >= 30 && !strings.Contains(ansi.Strip(lines[g.headerY]), pageTitle(page)) {
						t.Fatal("header actions overwrote the page title")
					}
					for _, id := range actions {
						target := findTarget(t, m, id)
						if target.Y >= g.headerHeight {
							t.Fatalf("%s extends below the header: %+v", id, target)
						}
						painted := ansi.Strip(ansi.Cut(lines[target.Y], target.X, target.X+target.W))
						if strings.TrimSpace(strings.TrimPrefix(painted, " ›")) == "" {
							t.Fatalf("header button was clipped: %s=%q", id, painted)
						}
						if id == focused && !strings.HasPrefix(painted, " ›") {
							t.Fatalf("header focus marker was clipped: %s=%q", id, painted)
						}
						count := 0
						for _, hit := range m.targets {
							if hit.ID == id {
								count++
							}
						}
						if count != 1 {
							t.Fatalf("header and body duplicate %s (%d hits)", id, count)
						}
					}
				})
			}
		}
	}
	m := headerModel("overview", 120, 32)
	if strings.Contains(headerText(m), "Mihomo ·") || strings.Contains(headerText(m), "Running") {
		t.Fatal("Overview header retained the global core state")
	}
}

func TestPageHeaderSpacingAndBoundsStayConsistentAcrossPages(t *testing.T) {
	for _, size := range [][2]int{{30, 12}, {60, 20}, {80, 24}, {120, 18}, {120, 32}, {160, 45}, {216, 55}} {
		for _, page := range navPages() {
			t.Run(fmt.Sprintf("%s/%dx%d", page, size[0], size[1]), func(t *testing.T) {
				m := headerModel(page, size[0], size[1])
				g := dashboardGeometry(m.dashboardOptions())
				lines := strings.Split(m.screen, "\n")
				right := size[0]
				if strings.Contains(ansi.Strip(strings.Join(lines[:g.headerHeight+1], "\n")), "▔") {
					t.Fatal("header retained a horizontal divider")
				}
				bodyStart := g.y - m.dashboardOptions().BodyTopRows
				gap := 0
				if size[1] < 24 {
					gap = 1
				}
				if bodyStart != g.headerHeight+gap {
					t.Fatal("body has incorrect spacing below the header")
				}
				for y := 0; y < g.headerHeight; y++ {
					if padding := ansi.Cut(lines[y], right-1, right); !strings.Contains(padding, themed(" ", text, bg, false)) {
						t.Fatalf("header background does not reach the right edge: %q", padding)
					}
				}
				for _, action := range m.pageHeader().actions {
					if !action.enabled {
						continue
					}
					if hit := findTarget(t, m, action.id); hit.X+hit.W > g.headerX+g.headerWidth {
						t.Fatalf("header action exceeds the shared right edge: %+v", hit)
					}
				}
			})
		}
	}
}

func TestProfilesHeaderStaysFixedAndReachableWhileScrolled(t *testing.T) {
	m := headerModel("profiles", 60, 20)
	m.scroll = 1000
	m.focus = "menu"
	m.layout()
	if m.scroll == 0 {
		t.Fatal("fixture did not scroll the body")
	}
	before := m.scroll
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "profiles.update-all" || m.scroll != before {
		t.Fatalf("Tab did not reach fixed header control without moving the body: focus=%s scroll=%d", m.focus, m.scroll)
	}
	target := findTarget(t, m, "profiles.update-all")
	if target.Y != 1 {
		t.Fatalf("narrow header moved with the body: %+v", target)
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "profiles.url" || m.scroll != 0 {
		t.Fatal("Tab did not continue from header to the revealed URL input")
	}
}

func TestProfilesHeaderUpdateUsesSharedUseCaseWithoutApplying(t *testing.T) {
	updates := 0
	m := headerModel("profiles", 120, 32)
	b := &profileBackend{fakeBackend: &fakeBackend{snapshot: m.snapshot, refreshAllProfiles: func(context.Context) (app.ProfileRefreshSummary, error) {
		updates++
		return app.ProfileRefreshSummary{Updated: 1}, nil
	}}}
	m.backend = b
	m, cmd := click(m, findTarget(t, m, "profiles.update-all"))
	m = finishProfileJob(t, m, cmd)
	if updates != 1 || b.applied != 0 || m.snapshot.Applied != "default" {
		t.Fatal("header Update all bypassed profile refresh or applied a profile")
	}
}

func TestProfilesHeaderUpdateIsDisabledWithoutProfiles(t *testing.T) {
	m := headerModel("profiles", 120, 32)
	m.snapshot.Profiles = nil
	m.snapshot.ProfileDetails = nil
	m.layout()
	if !strings.Contains(headerText(m), "Update all") {
		t.Fatal("disabled Update all disappeared from the page header")
	}
	for _, hit := range m.targets {
		if hit.ID == "profiles.update-all" {
			t.Fatal("Update all remained actionable with an empty library")
		}
	}
}

func TestProfilesHeaderLosingLastSubscriptionRepairsFocusAndBlocksUpdate(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {120, 32}, {216, 55}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := headerModel("profiles", size[0], size[1])
			calls := 0
			m.backend = &fakeBackend{snapshot: m.snapshot, refreshAllProfiles: func(context.Context) (app.ProfileRefreshSummary, error) {
				calls++
				return app.ProfileRefreshSummary{Updated: 1}, nil
			}}
			m.focus = "profiles.update-all"
			m.layout()
			findTarget(t, m, m.focus)
			next := m.snapshot
			next.Profiles = nil
			next.ProfileDetails = profileFixtures("default")
			beforeID := m.id
			m, _ = update(m, coreStatusMsg{generation: m.statusGeneration, snapshot: next})
			if m.focus == "profiles.update-all" || hasAction(m.targets, "profiles.update-all") {
				t.Fatal("disabled Update all retained focus or a mouse target after the last subscription disappeared")
			}
			m, _ = update(m, special(tea.KeyEnter))
			if m.busy || m.id != beforeID || calls != 0 {
				t.Fatal("Enter started an operation through the disabled header action")
			}
			if handled, cmd := m.profileAction("profiles.update-all"); !handled || cmd != nil || m.busy || m.id != beforeID {
				t.Fatal("a stale Update all action bypassed the current snapshot availability check")
			}
		})
	}
}

func TestPageCountsRequireAvailableObservations(t *testing.T) {
	m := headerModel("rules", 120, 32)
	field := findTarget(t, m, "rules.search")
	line := strings.Split(m.screen, "\n")[field.Y+1]
	if count := strings.TrimSpace(ansi.Strip(ansi.Cut(line, field.X+field.W, m.width))); count != "3" {
		t.Fatalf("rules count must appear next to the search field: %q", count)
	}
	m.rules.received = false
	m.layout()
	field = findTarget(t, m, "rules.search")
	line = strings.Split(m.screen, "\n")[field.Y+1]
	if count := strings.TrimSpace(ansi.Strip(ansi.Cut(line, field.X+field.W, m.width))); count != "" {
		t.Fatal("rules presented an unavailable count as current")
	}
}

func headerText(m Model) string {
	g := dashboardGeometry(dashboardOptions{Width: m.width, Height: m.height, Page: m.page})
	return ansi.Strip(strings.Join(strings.Split(m.screen, "\n")[:g.headerHeight], "\n"))
}

func TestRulesFailureRetryUsesExistingReader(t *testing.T) {
	calls := 0
	m := headerModel("rules", 120, 32)
	m.backend = &fakeBackend{snapshot: m.snapshot, rules: func(context.Context) (app.RuleSnapshot, error) {
		calls++
		return app.RuleSnapshot{State: "ready", Rules: sampleRules()}, nil
	}}
	m.rules.snapshot.State = "unavailable"
	m.layout()
	m, cmd := click(m, findTarget(t, m, "rules.retry"))
	if cmd == nil || m.busy {
		t.Fatal("rules retry did not start an independent page reader")
	}
	m, _ = update(m, cmd())
	if calls != 1 {
		t.Fatalf("rules retry read the wrong use case: %d reads", calls)
	}
	m.stopRuntime()
	m.stopRules()
	m.stopCoreStatus()
}

func TestRulesReadyHeaderOmitsRefreshAndFailureOffersRetry(t *testing.T) {
	m := headerModel("rules", 120, 32)
	if hasAction(m.targets, "rules.retry") || hasAction(m.focusTargets(), "rules.retry") || m.pageHeader().owns("rules.retry") {
		t.Fatal("available rules retained a permanent refresh action")
	}
	m.rules.snapshot = app.RuleSnapshot{State: "unavailable", Error: "fixture unavailable"}
	m.layout()
	target := findTarget(t, m, "rules.retry")
	if target.Y < dashboardGeometry(m.dashboardOptions()).y {
		t.Fatal("failed rules placed retry in the permanent header")
	}
}

func TestLogsHeaderPauseResumeEndsSearchAndKeepsCapture(t *testing.T) {
	m := headerModel("logs", 120, 32)
	m.logs.editing = true
	m.logs.search.Focus()
	m.layout()
	m, cmd := click(m, findTarget(t, m, "logs.follow"))
	if cmd != nil || !m.logs.active || m.logs.paused || m.logs.editing || m.logs.search.Focused() {
		t.Fatal("Resume did not leave search and preserve the existing capture")
	}
	defer m.stopRuntime()
	defer m.stopCoreStatus()
	m, _ = click(m, findTarget(t, m, "logs.follow"))
	if !m.logs.active || !m.logs.paused {
		t.Fatal("Pause interrupted capture instead of freezing display")
	}
}

func TestHeaderActionsAreUnavailableDuringBusyOrConnectionConfirmation(t *testing.T) {
	m := headerModel("profiles", 120, 32)
	m.busy = true
	m.layout()
	for _, hit := range m.targets {
		if hit.ID == "profiles.update-all" {
			t.Fatal("busy header retained actionable controls")
		}
	}
	m = headerModel("connections", 120, 32)
	m.connectionActivate("conn.close-all")
	m.layout()
	for _, hit := range m.targets {
		if hit.ID == "conn.close-all" {
			t.Fatal("connection confirmation retained a clickable Close all action")
		}
	}
}
