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
	backend := &fakeBackend{snapshot: app.Snapshot{Installed: true, ConfigReady: true, HasRun: true, GuideDismissed: true, Service: service.Status{Active: true, State: "active"}}, control: func(context.Context, string) error { calls++; return nil }}
	m := ready(backend)
	m.width, m.height = 120, 32
	m.snapshot = app.Snapshot{Installed: true, ConfigReady: true, HasRun: true, GuideDismissed: true, Service: service.Status{Active: true, State: "active"}}
	m.focus = "core.toggle"
	m.returnToNavigation()
	m.layout()
	id := m.id
	m, _ = update(m, special(tea.KeyEnter))
	if m.focus != "core.toggle" || m.page != "overview" || m.id != id || m.busy || m.failure != "" || calls != 0 {
		t.Fatalf("entering content activated the restored action: focus=%s busy=%v calls=%d failure=%q", m.focus, m.busy, calls, m.failure)
	}
	m, cmd := update(m, special(tea.KeyEnter))
	if !m.busy || m.id == id {
		t.Fatal("a subsequent Enter on the focused core toggle did not activate it")
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

func TestOverviewHeadingActionsRequireExplicitActivationAfterEnteringCard(t *testing.T) {
	for _, action := range []struct{ card, id string }{
		{"dashboard.control.core", "core.manage"},
		{"dashboard.profile.title", "card.profiles"},
		{"dashboard.overview.network", "network.primaryConfig"},
		{"dashboard.control.routingMode", "card.proxies"},
	} {
		t.Run(action.id, func(t *testing.T) {
			reads, mutations := 0, 0
			backend := &fakeBackend{snapshot: app.Snapshot{
				Installed: true, ConfigReady: true, HasRun: true, GuideDismissed: true,
				Service: service.Status{Active: true, State: "active"},
				Mode:    app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
			}}
			backend.getSettings = func(context.Context) (app.RuntimeSettings, error) { reads++; return app.RuntimeSettings{}, nil }
			backend.control = func(context.Context, string) error { mutations++; return nil }
			backend.mode = func(context.Context, string) error { mutations++; return nil }
			m := ready(backend)
			defer m.stopRuntime()
			m.width, m.height, m.focus = 160, 45, "overview.card."+action.card
			m.layout()
			id := m.id
			m, _ = update(m, special(tea.KeyEnter))
			if m.page != "overview" || m.focus != action.id || m.id != id || m.busy || m.ports.open || m.wizard.open || reads != 0 || mutations != 0 {
				t.Fatalf("entering a card activated its heading navigation: page=%s focus=%s", m.page, m.focus)
			}
		})
	}
}

func TestPageNavigationCoreCardControlsAndManagementShortcut(t *testing.T) {
	m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
	directionCleanup(t, &m)
	m.width, m.height, m.focus = 120, 32, "overview.card.dashboard.control.core"
	m.layout()
	id := m.id
	m, _ = update(m, special(tea.KeyEnter))
	if m.focus != "core.manage" || m.wizard.open || m.busy || m.id != id {
		t.Fatalf("entering the core card did not safely focus Manage: %s", m.focus)
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "core.toggle" || m.busy {
		t.Fatalf("Tab did not focus the service control: %s", m.focus)
	}
	m, _ = update(m, key("i"))
	if m.page != "overview" || !m.wizard.open || m.wizard.step != stepCore || m.wizard.focus != "check-update" || m.busy || m.id != id {
		t.Fatal("the independent i shortcut did not open version details directly")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.wizard.open || m.page != "overview" || m.focus != "core.toggle" {
		t.Fatal("closing shortcut-opened details lost the original core control")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focus != "core.manage" || m.contextHelp() != m.controlActionHelp(m.tr("navigation.controls.manageCore")) {
		t.Fatalf("Shift+Tab did not restore Manage and its action hint: %s / %s", m.focus, m.contextHelp())
	}
}

func TestPageNavigationOverviewManageRevealsInstalledVersionAndReturns(t *testing.T) {
	for _, tc := range []struct {
		entry, language string
		width, height   int
		openDetails     bool
	}{
		{"enter", i18n.English, 160, 45, true},
		{"space", i18n.Chinese, 80, 24, false},
		{"click", i18n.Chinese, 40, 16, true},
	} {
		t.Run(tc.entry, func(t *testing.T) {
			m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
			directionCleanup(t, &m)
			m.locale, m.width, m.height = i18n.New(tc.language), tc.width, tc.height
			m.focus = "overview.card.dashboard.control.core"
			if tc.entry == "click" {
				m.scroll = 1 // The link remains visible in the partially scrolled card.
				m.pageViews["settings"] = pageViewState{focus: "restart", scroll: 200}
			}
			m.layout()
			id, scroll := m.id, m.scroll
			if tc.entry == "click" {
				m, _ = click(m, findTarget(t, m, "core.manage"))
			} else {
				m, _ = update(m, special(tea.KeyEnter))
				if m.focus != "core.manage" || m.page != "overview" {
					t.Fatalf("entering the card focused %s/%s", m.page, m.focus)
				}
				activate := special(tea.KeyEnter)
				if tc.entry == "space" {
					activate = special(tea.KeySpace)
				}
				m, _ = update(m, activate)
			}
			if m.page != "settings" || m.focus != "install" || m.wizard.open || m.busy || m.id != id {
				t.Fatalf("Manage should navigate to the installed-version row: page=%s focus=%s details=%t busy=%t", m.page, m.focus, m.wizard.open, m.busy)
			}
			row := settingsPaintedTarget(t, m.screen, m.targets, "install")
			if !strings.HasPrefix(row, "›") || !strings.Contains(row, m.snapshot.Version) {
				t.Fatalf("installed version and focus should both be visible: %q", row)
			}
			if tc.width == 40 {
				g := dashboardGeometry(m.navigationOptions())
				hit := findTarget(t, m, "install")
				before := strings.Split(m.screen, "\n")[g.y:hit.Y]
				if m.scroll == 0 || !strings.Contains(ansi.Strip(strings.Join(before, "\n")), m.tr("settings.core")) {
					t.Fatal("narrow settings did not reveal the core management title above the focused version")
				}
			}
			m, _ = update(m, special(tea.KeyTab))
			if m.focus != "core.checkUpdate" || !hasAction(m.targets, m.focus) || m.wizard.open || m.busy || m.id != id {
				t.Fatalf("Tab from the installed version should focus Check for updates: focus=%s", m.focus)
			}
			m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			if m.focus != "install" || !hasAction(m.targets, m.focus) {
				t.Fatalf("Shift+Tab from Check for updates should focus the installed version: focus=%s", m.focus)
			}
			if tc.openDetails {
				m, _ = update(m, special(tea.KeyEnter))
				if !m.wizard.open || m.wizard.step != stepCore || m.wizard.focus != "check-update" || m.busy || m.id != id {
					t.Fatal("activating the installed version did not open its details")
				}
				m, _ = update(m, special(tea.KeyEscape))
				if m.wizard.open || m.page != "settings" || m.focus != "install" || !hasAction(m.targets, m.focus) {
					t.Fatalf("Esc from details should restore the visible version row: %s/%s", m.page, m.focus)
				}
			}
			m, _ = update(m, special(tea.KeyEscape))
			if m.page != "overview" || m.focus != "core.manage" || !hasAction(m.targets, m.focus) || m.scroll != scroll {
				t.Fatalf("Esc from settings should restore the Manage link: page=%s focus=%s scroll=%d", m.page, m.focus, m.scroll)
			}
		})
	}
}

func TestPageNavigationOrdinarySettingsKeepsItsOwnBackAction(t *testing.T) {
	for _, entry := range []string{"after leaving settings", "same-page digit", "same-page sidebar"} {
		t.Run(entry, func(t *testing.T) {
			m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
			directionCleanup(t, &m)
			m.width, m.height = 160, 45
			m.layout()
			m, _ = click(m, findTarget(t, m, "core.manage"))
			if m.page != "settings" {
				t.Fatal("Manage did not open Settings")
			}
			if entry == "after leaving settings" {
				m, _ = update(m, key("6"))
			}
			if entry == "same-page sidebar" {
				m, _ = click(m, findTarget(t, m, "nav.settings"))
				m, _ = update(m, special(tea.KeyEnter))
			} else {
				m, _ = update(m, key("7"))
			}
			m, _ = update(m, special(tea.KeyEscape))
			if m.page != "settings" || m.focus != "nav.settings" {
				t.Fatalf("opening Settings from navigation inherited a previous Manage link: %s/%s", m.page, m.focus)
			}
		})
	}
}

func TestOverviewManageIgnoresStaleActivation(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		ready, installed, busy, quitting bool
	}{
		{name: "missing snapshot", installed: true},
		{name: "uninstalled", ready: true},
		{name: "busy", ready: true, installed: true, busy: true},
		{name: "quitting", ready: true, installed: true, quitting: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := ready(&fakeBackend{snapshot: onboardingDefaultSnapshot()})
			m.focus = "core.manage"
			m.snapshotReady, m.snapshot.Installed, m.busy, m.quitting = tc.ready, tc.installed, tc.busy, tc.quitting
			if cmd := m.activate(m.focus); cmd != nil || m.page != "overview" || m.wizard.open || m.wizard.inline || strings.Contains(m.contextHelp(), "Enter") {
				t.Fatal("stale Manage focus exposed installation or management")
			}
		})
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
		m.focus = "profile.1" // beta
		m.browsePage("settings")
		m.snapshot.Profiles = []string{"gamma", "alpha", "beta"}
		m.navigate("profiles")
		if m.focus != "profile.2" {
			t.Fatalf("restored a different profile after reordering: %s", m.focus)
		}
	})
	t.Run("node", func(t *testing.T) {
		m := pageNavigationModel()
		m.snapshot.Installed, m.snapshot.Service.Active = true, true
		m.snapshot.Groups = []mihomo.Group{{Name: "A", Type: "Selector", All: []string{"a1"}}, {Name: "B", Type: "Selector", All: []string{"b1", "b2"}}}
		setProxyTestGroup(&m, "B")
		m.navigate("proxies")
		m.focus = "node.1.1"
		m.browsePage("overview")
		m.snapshot.Groups = []mihomo.Group{{Name: "B", Type: "Selector", All: []string{"b2", "b1"}}, {Name: "A", Type: "Selector", All: []string{"a1"}}}
		m.navigate("proxies")
		if m.focus != "node.0.0" || m.proxies.activeGroup != "B" {
			t.Fatalf("restored a different proxy after reordering: focus=%s group=%s", m.focus, m.proxies.activeGroup)
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
			if width < sidebarMinWidth {
				m.returnToNavigation()
				m.activate("menu")
			}
			m.layout()
			m, _ = click(m, findTarget(t, m, "nav.proxies"))
			if m.page != "proxies" {
				t.Fatal("menu click did not switch pages")
			}
			if width >= sidebarMinWidth {
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
		m.focus = "profile.1"
		m.layout()
		m.snapshot.Profiles = []string{"gamma", "alpha", "beta"}
		m.layout()
		if m.focus != "profile.2" || m.pageViews["profiles"].profile != "beta" {
			t.Fatalf("refresh reassigned active profile focus: focus=%s identity=%s", m.focus, m.pageViews["profiles"].profile)
		}
	})
	t.Run("node", func(t *testing.T) {
		m := pageNavigationModel()
		m.snapshot.Installed, m.snapshot.Service.Active = true, true
		m.snapshot.Groups = []mihomo.Group{{Name: "A", Type: "Selector", All: []string{"a1"}}, {Name: "B", Type: "Selector", All: []string{"b1", "b2"}}}
		setProxyTestGroup(&m, "B")
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
				m.focus = "overview.card.dashboard.overview.ipInfo"
				if setup {
					m.focus = "setup.install"
				}
				wantFocus := m.focus
				var wantIDs []string
				if !setup {
					wantIDs = []string{
						"overview.card.dashboard.control.core", "overview.card.dashboard.profile.title",
						"overview.card.dashboard.overview.network", "overview.card.dashboard.control.routingMode",
						"overview.card.dashboard.traffic.title", "overview.card.dashboard.system.title", "overview.card.dashboard.overview.ipInfo",
					}
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
						t.Fatalf("width %d: resizing moved focus from %s to %s", width, wantFocus, m.focus)
					}
				}
			})
		}
	}
}
