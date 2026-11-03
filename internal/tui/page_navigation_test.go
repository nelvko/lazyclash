package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func pageNavigationModel() Model {
	m := ready(&fakeBackend{})
	m.width, m.height = 120, 32
	m.browsePage("overview")
	m.layout()
	return m
}

func TestPageNavigationArrowsSwitchContentWithoutLeavingSidebar(t *testing.T) {
	m := pageNavigationModel()
	for _, step := range []struct {
		key  tea.KeyPressMsg
		page string
	}{
		{special(tea.KeyUp), "overview"},
		{special(tea.KeyDown), "proxies"},
		{key("j"), "profiles"},
		{key("k"), "proxies"},
		{special(tea.KeyEnd), "settings"},
		{special(tea.KeyDown), "settings"},
		{special(tea.KeyHome), "overview"},
	} {
		m, _ = update(m, step.key)
		if m.page != step.page || m.focus != "nav."+step.page {
			t.Fatalf("%s: page=%s focus=%s; wanted navigation on %s", step.key.String(), m.page, m.focus, step.page)
		}
		g := dashboardGeometry(m.dashboardOptions())
		row := strings.Split(m.screen, "\n")[g.headerY]
		if !strings.Contains(ansi.Strip(ansi.Cut(row, g.x, m.width)), pageTitle(step.page)) {
			t.Fatal("navigation selection changed without updating the content header")
		}
		assertCanvas(t, m.screen, m.width, m.height)
		assertRegions(t, m.targets, m.width, m.height)
	}
}

func TestPageNavigationEnterRestoresActionFocusWithoutActivatingIt(t *testing.T) {
	calls := 0
	backend := &fakeBackend{control: func(context.Context, string) error { calls++; return nil }}
	m := ready(backend)
	m.width, m.height = 120, 32
	m.snapshot = app.Snapshot{Installed: true, ConfigReady: true, HasRun: true, GuideDismissed: true, Service: service.Status{Active: true, State: "active"}}
	m.focus = "core.stop"
	m.returnToNavigation()
	m.layout()
	id := m.id
	m, _ = update(m, special(tea.KeyEnter))
	if m.focus != "core.stop" || m.page != "overview" || m.id != id || m.busy || m.failure != "" || calls != 0 {
		t.Fatalf("entering content activated the restored action: focus=%s busy=%v calls=%d failure=%q", m.focus, m.busy, calls, m.failure)
	}
	m, cmd := update(m, special(tea.KeyEnter))
	if !m.busy || m.id == id {
		t.Fatal("a subsequent Enter on the focused stop button did not activate it")
	}
	actionCmd(t, cmd)()
	if calls != 1 {
		t.Fatalf("stop action ran %d times; expected exactly once", calls)
	}
	if m.cancel != nil {
		m.cancel()
	}
}

func TestPageNavigationOverviewStartsAtCardBeforeItsControls(t *testing.T) {
	m := pageNavigationModel()
	m.snapshot.GuideDismissed = true
	m.layout()
	m, _ = update(m, special(tea.KeyEnter))
	if !strings.HasPrefix(m.focus, "overview.card.") || m.busy || m.wizard.open || m.form != "" {
		t.Fatalf("Overview did not enter a safe card focus: %s", m.focus)
	}
	id := m.id
	m, _ = update(m, special(tea.KeyEnter))
	if strings.HasPrefix(m.focus, "overview.card.") || m.busy || m.id != id || m.wizard.open || m.form != "" {
		t.Fatal("entering a card's controls performed an action")
	}
}

func TestPageNavigationInstalledKernelManageRequiresExplicitActivation(t *testing.T) {
	m := pageNavigationModel()
	m.snapshot = onboardingDefaultSnapshot()
	m.focus = "overview.card.dashboard.kernel.title"
	m.layout()
	id := m.id
	m, _ = update(m, special(tea.KeyEnter))
	if m.focus != "card.install" || m.wizard.open || m.busy || m.id != id {
		t.Fatal("entering the kernel card did not focus Manage without activating it")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if !m.wizard.open || m.wizard.inline || m.wizard.step != stepCore || m.busy || m.id != id {
		t.Fatal("explicit Manage activation did not open installed-core management without a lookup")
	}
}

func TestPageNavigationEnvironmentFocusSurvivesInstallationStateChanges(t *testing.T) {
	m := pageNavigationModel()
	m.snapshot.GuideDismissed = true
	m.focus = "overview.card.dashboard.system.title"
	for _, installed := range []bool{true, false, true} {
		m.snapshot.Installed = installed
		m.layout()
		if m.focus != "overview.card.dashboard.system.title" || !hasAction(m.contentTargets(), m.focus) {
			t.Fatalf("installed=%v: adding or removing kernel information lost environment focus: %s", installed, m.focus)
		}
	}
}

func TestPageNavigationEscapeReturnsToSidebarWithoutDismissingToast(t *testing.T) {
	m := pageNavigationModel()
	m.navigate("overview")
	m.notification.items = []notificationItem{{ID: 77, Kind: "error", Page: "profiles", Context: "Update", Message: "Subscription is unavailable"}}
	m.notification.visible = true
	m.layout()
	m, _ = update(m, special(tea.KeyEscape))
	if m.focus != "nav.overview" || m.page != "overview" {
		t.Fatalf("Escape did not return to current-page navigation: %s / %s", m.page, m.focus)
	}
	if len(m.notification.items) != 1 || m.notification.items[0].ID != 77 {
		t.Fatal("an ordinary toast consumed or was dismissed by navigation Escape")
	}
}

func TestPageNavigationDigitKeysEnterContent(t *testing.T) {
	m := pageNavigationModel()
	for index, page := range navPages() {
		m, _ = update(m, key(fmt.Sprint(index+1)))
		if m.page != page || strings.HasPrefix(m.focus, "nav.") || m.focus == "menu" || m.sidebarOpen {
			t.Fatalf("shortcut %d did not enter %s content: page=%s focus=%s", index+1, page, m.page, m.focus)
		}
		if m.busy || m.wizard.open || m.form != "" {
			t.Fatal("a page shortcut activated an operation")
		}
	}
}

func TestPageNavigationRestoresScrollSelectionAndDraft(t *testing.T) {
	m := pageNavigationModel()
	for index := 0; index < 40; index++ {
		m.snapshot.Profiles = append(m.snapshot.Profiles, fmt.Sprintf("profile-%02d", index))
	}
	m.navigate("profiles")
	m.profilesUI.url.SetValue("https://example.invalid/unfinished-subscription")
	m.focusProfileControl("profile.20")
	m.layout()
	wantScroll := m.scroll
	if wantScroll == 0 {
		t.Fatal("fixture did not focus a profile below the fold")
	}
	m.returnToNavigation()
	m, _ = update(m, special(tea.KeyEnd))
	m, _ = update(m, key("3"))
	if m.page != "profiles" || m.focus != "profile.20" || m.scroll != wantScroll || m.profilesUI.url.Value() != "https://example.invalid/unfinished-subscription" {
		t.Fatalf("returning reset the profile view: focus=%s scroll=%d want=%d draft=%q", m.focus, m.scroll, wantScroll, m.profilesUI.url.Value())
	}
	m, _ = update(m, key("3"))
	if m.focus != "profile.20" || m.scroll != wantScroll {
		t.Fatal("selecting the current page reset its content position")
	}
}

func TestPageNavigationRestoresProfilesAndNodesByIdentity(t *testing.T) {
	t.Run("profile", func(t *testing.T) {
		m := pageNavigationModel()
		m.snapshot.Profiles = []string{"alpha", "beta", "gamma"}
		m.navigate("profiles")
		m.focus = "profile.2" // beta, following the default profile.
		m.browsePage("settings")
		m.snapshot.Profiles = []string{"gamma", "alpha", "beta"}
		m.navigate("profiles")
		if m.focus != "profile.3" {
			t.Fatalf("restored a different profile after reordering: %s", m.focus)
		}
	})
	t.Run("node", func(t *testing.T) {
		m := pageNavigationModel()
		m.snapshot.Installed, m.snapshot.Service.Active = true, true
		m.snapshot.Groups = []mihomo.Group{{Name: "A", Type: "Selector", All: []string{"a1"}}, {Name: "B", Type: "Selector", All: []string{"b1", "b2"}}}
		m.proxies.expanded = "B"
		m.navigate("proxies")
		m.focus = "node.1.1"
		m.browsePage("overview")
		m.snapshot.Groups = []mihomo.Group{{Name: "B", Type: "Selector", All: []string{"b2", "b1"}}, {Name: "A", Type: "Selector", All: []string{"a1"}}}
		m.navigate("proxies")
		if m.focus != "node.0.0" || m.proxies.expanded != "B" {
			t.Fatalf("restored a different proxy after reordering: focus=%s group=%s", m.focus, m.proxies.expanded)
		}
	})
}

func TestPageNavigationCompactDrawerBrowsesAndEntersContent(t *testing.T) {
	m := pageNavigationModel()
	m.width, m.height = 80, 24
	m.returnToNavigation()
	m.layout()
	m, _ = update(m, special(tea.KeyDown))
	if !m.sidebarOpen || m.page != "overview" || m.focus != "nav.overview" {
		t.Fatal("opening the drawer skipped the current page")
	}
	m, _ = update(m, special(tea.KeyDown))
	if !m.sidebarOpen || m.page != "proxies" || m.focus != "nav.proxies" {
		t.Fatal("drawer browsing failed to update content while keeping navigation open")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.sidebarOpen || m.page != "proxies" || strings.HasPrefix(m.focus, "nav.") || m.focus == "menu" {
		t.Fatal("Enter did not close the drawer and enter its current page")
	}
	m.returnToNavigation()
	m, _ = update(m, special(tea.KeyDown))
	m, _ = update(m, special(tea.KeyDown))
	if m.page != "profiles" {
		t.Fatal("fixture did not browse to another page")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.sidebarOpen || m.focus != "menu" || m.page != "profiles" {
		t.Fatal("closing the drawer reverted the page or did not return to Menu")
	}
}

func TestPageNavigationMouseFocusFollowsSidebarAndDrawer(t *testing.T) {
	for _, width := range []int{120, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := pageNavigationModel()
			m.width, m.height = width, 32
			if width < 100 {
				m.returnToNavigation()
				m.activate("menu")
			}
			m.layout()
			m, _ = click(m, findTarget(t, m, "nav.proxies"))
			if m.page != "proxies" {
				t.Fatal("menu click did not switch pages")
			}
			if width >= 100 {
				if m.focus != "nav.proxies" {
					t.Fatal("permanent sidebar click unexpectedly transferred focus into content")
				}
			} else if m.sidebarOpen || strings.HasPrefix(m.focus, "nav.") || m.focus == "menu" {
				t.Fatal("drawer click did not close the drawer and transfer focus into content")
			}
		})
	}
}

func TestPageNavigationCanBrowseDuringBackgroundWriteWithoutCanceling(t *testing.T) {
	m := pageNavigationModel()
	m.begin("Updating", "Updated", func(context.Context) error { return nil }, notificationOperation{Key: "profile.refresh:work", Context: "Work profile"})
	defer m.cancel()
	m.returnToNavigation()
	m.layout()
	id, operationCtx := m.id, m.opCtx
	m, _ = update(m, special(tea.KeyDown))
	if m.page != "proxies" || m.focus != "nav.proxies" || !m.busy || m.id != id || operationCtx.Err() != nil {
		t.Fatal("browsing during a background write canceled or replaced the operation")
	}
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, special(tea.KeyEscape))
	if m.focus != "nav.proxies" || !m.busy || m.id != id || operationCtx.Err() != nil {
		t.Fatal("returning to navigation canceled a background write")
	}
}

func TestPageNavigationActiveFocusSurvivesSnapshotReordering(t *testing.T) {
	t.Run("profile", func(t *testing.T) {
		m := pageNavigationModel()
		m.snapshot.Profiles = []string{"alpha", "beta", "gamma"}
		m.navigate("profiles")
		m.focus = "profile.2"
		m.layout()
		m.snapshot.Profiles = []string{"gamma", "alpha", "beta"}
		m.layout()
		if m.focus != "profile.3" || m.pageViews["profiles"].profile != "beta" {
			t.Fatalf("refresh reassigned active profile focus: focus=%s identity=%s", m.focus, m.pageViews["profiles"].profile)
		}
	})
	t.Run("node", func(t *testing.T) {
		m := pageNavigationModel()
		m.snapshot.Installed, m.snapshot.Service.Active = true, true
		m.snapshot.Groups = []mihomo.Group{{Name: "A", Type: "Selector", All: []string{"a1"}}, {Name: "B", Type: "Selector", All: []string{"b1", "b2"}}}
		m.proxies.expanded = "B"
		m.navigate("proxies")
		m.focus = "node.1.1"
		m.layout()
		m.snapshot.Groups = []mihomo.Group{{Name: "B", Type: "Selector", All: []string{"b2", "b1"}}, {Name: "A", Type: "Selector", All: []string{"a1"}}}
		m.layout()
		if m.focus != "node.0.0" || m.pageViews["proxies"].node != "b2" {
			t.Fatalf("refresh reassigned active proxy focus: focus=%s identity=%s", m.focus, m.pageViews["proxies"].node)
		}
		m.snapshot.Groups[0].All = []string{"b1"}
		m.layout()
		if m.focus != "group.0" {
			t.Fatalf("removed node did not fall back to its own group: %s", m.focus)
		}
	})
}

func TestPageNavigationRemovedOverviewGuideRestoresVisibleCardFocus(t *testing.T) {
	m := pageNavigationModel()
	m.navigate("overview")
	m.layout()
	oldFocus := m.focus
	if oldFocus != "setup.install" {
		t.Fatalf("fixture did not focus the setup action: %s", oldFocus)
	}
	m.snapshot.GuideDismissed = true
	m.layout()
	if m.focus == oldFocus || !strings.HasPrefix(m.focus, "overview.card.") {
		t.Fatalf("removed guide left an invisible content focus: %s", m.focus)
	}
	found := false
	for _, target := range m.contentTargets() {
		found = found || target.ID == m.focus
	}
	if !found {
		t.Fatal("fallback Overview card has no current content target")
	}
}

func TestPageNavigationOverviewCardIDsRemainUniqueAndStableAcrossResize(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, state := range []struct {
			name                      string
			installed, guideDismissed bool
		}{
			{name: "setup"},
			{name: "dismissed", guideDismissed: true},
			{name: "installed", installed: true},
			{name: "installed-dismissed", installed: true, guideDismissed: true},
		} {
			t.Run(fmt.Sprintf("%s/%s", language, state.name), func(t *testing.T) {
				m := pageNavigationModel()
				m.locale = i18n.New(language)
				m.snapshot.Installed, m.snapshot.GuideDismissed = state.installed, state.guideDismissed
				setup := !state.installed && !state.guideDismissed
				m.focus = "overview.card.dashboard.system.title"
				if setup {
					m.focus = "setup.install"
				}
				wantFocus := m.focus
				var wantIDs []string
				if !setup {
					wantIDs = []string{
						"overview.card.dashboard.control.core", "overview.card.dashboard.control.routingMode",
						"overview.card.dashboard.profile.title", "overview.card.dashboard.groups.title",
						"overview.card.dashboard.traffic.title",
					}
					if state.installed {
						wantIDs = append(wantIDs, "overview.card.dashboard.kernel.title")
					}
					wantIDs = append(wantIDs, "overview.card.dashboard.system.title")
				}
				for _, width := range []int{20, 30, 60, 120, 240, 20} {
					m.width = width
					m.layout()
					if width == 20 && !setup && dashboardBodyWidth(m.navigationOptions()) != 16 {
						t.Fatal("narrow fixture no longer exercises a 16-column card body")
					}
					cards := overviewCardRegionsFromModel(m)
					if len(cards) != len(wantIDs) {
						t.Fatalf("width %d: found %d cards; want %d", width, len(cards), len(wantIDs))
					}
					seen := make(map[string]bool)
					ids := make([]string, len(cards))
					for index, card := range cards {
						if seen[card.ID] {
							t.Fatalf("width %d: duplicate Overview card ID %s", width, card.ID)
						}
						seen[card.ID] = true
						ids[index] = card.ID
					}
					if strings.Join(ids, "\n") != strings.Join(wantIDs, "\n") {
						t.Fatalf("width %d: consolidated card IDs changed: got %v, want %v", width, ids, wantIDs)
					}
					if m.focus != wantFocus {
						t.Fatalf("width %d: resizing moved the system card focus to %s", width, m.focus)
					}
				}
			})
		}
	}
}
