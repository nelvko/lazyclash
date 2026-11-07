package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/service"
)

func profileNavigationModel(width, height int) Model {
	m := ready(&fakeBackend{snapshot: app.Snapshot{
		Installed: true, Applied: "alpha",
		Profiles:       []string{"alpha", "beta", "gamma", "delta", "epsilon"},
		ProfileDetails: profileFixtures("default", "alpha", "beta", "gamma", "delta", "epsilon"),
	}})
	m.page, m.focus = "profiles", "profile.0"
	m.width, m.height = width, height
	m.layout()
	return m
}

func profileNavigationPress(t *testing.T, m *Model, button string, expected int) {
	t.Helper()
	message := key(button)
	switch button {
	case "up":
		message = special(tea.KeyUp)
	case "down":
		message = special(tea.KeyDown)
	case "left":
		message = special(tea.KeyLeft)
	case "right":
		message = special(tea.KeyRight)
	case "home":
		message = special(tea.KeyHome)
	case "end":
		message = special(tea.KeyEnd)
	}
	if !m.profileGridKey(message) {
		t.Fatalf("card navigation did not handle %q", button)
	}
	if want := "profile." + strconv.Itoa(expected); m.focus != want {
		t.Fatalf("%q focused %s, expected %s", button, m.focus, want)
	}
}

func profileNavigationVisible(t *testing.T, m Model) {
	t.Helper()
	for _, card := range m.profileGridCards() {
		if card.ID != m.focus {
			continue
		}
		height := dashboardGeometry(dashboardOptions{Width: m.width, Height: m.height, Page: m.page}).height
		if card.Y < m.scroll || card.Y+card.H > m.scroll+height {
			t.Fatalf("focused card is outside the viewport: card=%+v scroll=%d height=%d", card, m.scroll, height)
		}
		return
	}
	t.Fatalf("focused card %s is missing", m.focus)
}

func TestProfileGridArrowsFollowRowsAndColumns(t *testing.T) {
	m := profileNavigationModel(160, 32)
	cards := m.profileGridCards()
	if len(cards) != 6 || cards[0].Y != cards[1].Y || cards[0].X == cards[1].X || cards[2].Y <= cards[0].Y {
		t.Fatalf("wide Profiles did not render a two-column grid: %+v", cards)
	}
	profileNavigationPress(t, &m, "right", 1)
	profileNavigationPress(t, &m, "down", 3)
	profileNavigationPress(t, &m, "left", 2)
	profileNavigationPress(t, &m, "up", 0)
	profileNavigationPress(t, &m, "left", 0)
	profileNavigationPress(t, &m, "up", 0)
	profileNavigationPress(t, &m, "j", 2)
	profileNavigationPress(t, &m, "l", 3)
	profileNavigationPress(t, &m, "k", 1)
	profileNavigationPress(t, &m, "h", 0)
	if m.snapshot.Applied != "alpha" {
		t.Fatal("browsing cards changed the applied profile")
	}
}

func TestProfileGridSingleColumnAndReflowPreserveFocus(t *testing.T) {
	m := profileNavigationModel(160, 32)
	m.focus = "profile.3"
	m.width, m.height = 80, 24
	m.layout()
	cards := m.profileGridCards()
	if cards[0].X != cards[1].X || cards[0].Y == cards[1].Y {
		t.Fatalf("narrow Profiles did not render one column: %+v", cards)
	}
	profileNavigationPress(t, &m, "down", 4)
	profileNavigationPress(t, &m, "right", 4)
	profileNavigationPress(t, &m, "l", 4)
	profileNavigationPress(t, &m, "up", 3)
	profileNavigationPress(t, &m, "end", 5)
	profileNavigationVisible(t, m)
	m.width, m.height = 160, 32
	m.layout()
	if m.focus != "profile.5" {
		t.Fatal("resizing changed the selected card")
	}
	profileNavigationPress(t, &m, "home", 0)
	profileNavigationVisible(t, m)
}

func TestProfileGridNavigationReachesCardsOutsideClippedTargets(t *testing.T) {
	m := profileNavigationModel(80, 20)
	for _, hit := range m.targets {
		if hit.ID == "profile.5" {
			t.Fatal("fixture did not put the last card below the fold")
		}
	}
	profileNavigationPress(t, &m, "end", 5)
	if m.scroll == 0 {
		t.Fatal("moving to an offscreen card did not scroll the body")
	}
	profileNavigationVisible(t, m)
	m.layout()
	findTarget(t, m, "profile.5")
	profileNavigationPress(t, &m, "home", 0)
	profileNavigationVisible(t, m)
}

func TestProfileToolbarDownEntersFirstCard(t *testing.T) {
	for _, toolbar := range []string{"profiles.url", "profiles.add", "add", "profiles.update-all"} {
		m := profileNavigationModel(160, 32)
		m.focus = toolbar
		profileNavigationPress(t, &m, "down", 0)
	}
}

func TestProfileGridIgnoresGlobalKeysInputsAndModals(t *testing.T) {
	for _, message := range []tea.KeyPressMsg{key("q"), key("a"), special(tea.KeyEnter), special(tea.KeySpace), {Code: 'r', Mod: tea.ModCtrl}, key("?")} {
		m := profileNavigationModel(160, 32)
		if m.profileGridKey(message) || m.focus != "profile.0" {
			t.Fatalf("grid captured unrelated shortcut %q", message.String())
		}
	}
	for _, state := range []string{"other-page", "navigation", "sidebar", "form", "inline-input", "help", "ports", "wizard", "network", "quitting"} {
		t.Run(state, func(t *testing.T) {
			m := profileNavigationModel(160, 32)
			switch state {
			case "other-page":
				m.page = "overview"
			case "navigation":
				m.focus = "nav.profiles"
			case "sidebar":
				m.sidebarOpen = true
			case "form":
				m.form = "source"
			case "inline-input":
				m.profilesUI.editing = true
			case "help":
				m.helpOpen = true
			case "ports":
				m.ports.open = true
			case "wizard":
				m.wizard.open = true
			case "network":
				m.wizard.networkOpen = true
			case "busy":
				m.busy = true
			case "quitting":
				m.quitting = true
			}
			before := m.focus
			if m.profileGridKey(special(tea.KeyDown)) || m.focus != before {
				t.Fatal("grid navigation escaped another input or modal")
			}
		})
	}
}

func TestProfileGridDetailsCloseOnEscapeOrBrowsingAnotherCard(t *testing.T) {
	m := profileNavigationModel(80, 20)
	m.focus, m.profilesUI.detail, m.scroll = "profiles.delete.3", "gamma", 999
	if !m.profileGridKey(special(tea.KeyEscape)) || m.profilesUI.detail != "" || m.focus != "profile.3" {
		t.Fatal("Escape did not close the named profile's details and restore its card")
	}
	profileNavigationVisible(t, m)
	m.width, m.height = 160, 32
	m.focus, m.profilesUI.detail = "profile.0", "default"
	profileNavigationPress(t, &m, "right", 1)
	if m.profilesUI.detail != "" {
		t.Fatal("moving to another card left actions for the old profile open")
	}
	if m.profileGridKey(special(tea.KeyEscape)) {
		t.Fatal("grid captured Escape without an open detail panel")
	}
}

func profileInteractionsModel(b *fakeBackend, width, height int) Model {
	m := ready(b)
	m.page, m.focus = "profiles", "profile.0"
	m.width, m.height = width, height
	m.layout()
	return m
}

func TestProfileURLInputOwnsTextPasteAndFocusKeys(t *testing.T) {
	m := profileInteractionsModel(&fakeBackend{}, 120, 32)
	m.activate("profiles.url")
	for _, text := range []string{"https://", "1", "q"} {
		m, _ = update(m, key(text))
	}
	m, _ = update(m, tea.PasteMsg{Content: ".example/private?token=secret123"})
	want := "https://1q.example/private?token=secret123"
	if m.profilesUI.url.Value() != want || m.page != "profiles" || m.quitting || !m.profilesUI.editing {
		t.Fatalf("URL text escaped the editor: url=%q page=%s quitting=%v", m.profilesUI.url.Value(), m.page, m.quitting)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), want) {
		t.Fatal("subscription URL is not visible for editing")
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "profiles.url.clear" || m.profilesUI.editing || m.profilesUI.url.Focused() {
		t.Fatalf("Tab did not reach the nonempty field's clear control: %s", m.focus)
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.focus != "profiles.add" || m.profilesUI.editing || m.profilesUI.url.Focused() {
		t.Fatalf("Tab did not leave the URL editor for Add: %s", m.focus)
	}
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focus != "profiles.url.clear" {
		t.Fatalf("Shift+Tab did not return to the clear control: %s", m.focus)
	}
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focus != "profiles.url" || !m.profilesUI.editing || !m.profilesUI.url.Focused() {
		t.Fatalf("Shift+Tab did not return to the URL editor: %s", m.focus)
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.focus != "profiles.add" || m.profilesUI.editing || m.profilesUI.url.Value() != want {
		t.Fatal("Escape did not leave the editor while preserving its draft")
	}
	m.activate("profiles.url")
	m, _ = update(m, tea.PasteMsg{Content: strings.Repeat("x", 160) + "visible-tail"})
	if !strings.Contains(ansi.Strip(m.View().Content), "visible-tail") {
		t.Fatal("long subscription URL did not scroll to its cursor")
	}
	m, _ = update(m, special(tea.KeyHome))
	if !strings.Contains(ansi.Strip(m.View().Content), "https://1q.example") {
		t.Fatal("Home did not reveal the beginning of the URL")
	}
	m, _ = update(m, special(tea.KeyEnd))
	m, _ = update(m, tea.WindowSizeMsg{Width: 60, Height: 20})
	if !strings.Contains(ansi.Strip(m.View().Content), "visible-tail") {
		t.Fatal("resizing lost the URL cursor's visible text")
	}
}

func TestProfileURLImportUsesAutomaticNameWithoutModalOrApply(t *testing.T) {
	addedName, addedURL := "not called", ""
	b := &fakeBackend{addSubscription: func(_ context.Context, name, source string) error {
		addedName, addedURL = name, source
		return nil
	}}
	m := profileInteractionsModel(b, 120, 32)
	backend := &profileBackend{fakeBackend: b}
	m.backend = backend
	m.activate("profiles.url")
	m, _ = update(m, tea.PasteMsg{Content: "https://example.com/private-token"})
	m, cmd := update(m, special(tea.KeyEnter))
	if m.form != "" || !m.busy {
		t.Fatal("URL submission opened a naming modal instead of adding immediately")
	}
	m = finishProfileJob(t, m, cmd)
	if addedName != "" || addedURL != "https://example.com/private-token" || backend.applied != 0 || m.form != "" || m.profilesUI.url.Value() != "" {
		t.Fatal("automatic import did not save and clear its input without applying")
	}
}

func TestProfileFailedImportRetainsURLForRetry(t *testing.T) {
	attempts := 0
	b := &fakeBackend{addSubscription: func(context.Context, string, string) error {
		attempts++
		if attempts == 1 {
			return errors.New("subscription download failed")
		}
		return nil
	}}
	m := profileInteractionsModel(b, 120, 32)
	m.activate("profiles.url")
	address := "https://example.invalid/private-retry-token"
	m, _ = update(m, tea.PasteMsg{Content: address})
	var cmd tea.Cmd
	for attempt := range 2 {
		m, cmd = update(m, special(tea.KeyEnter))
		if m.form != "" || !m.busy {
			t.Fatal("retry opened a naming form instead of reusing its URL")
		}
		m = finishProfileJob(t, m, cmd)
		if attempt == 0 && (m.failure == "" || m.profilesUI.url.Value() != address) {
			t.Fatal("failed import lost its editable source URL")
		}
		if attempt == 0 && !strings.Contains(ansi.Strip(m.View().Content), "private-retry-token") {
			t.Fatal("failed import URL is not visible for correction")
		}
		// The completed attempt focuses Add; Enter retries without retyping.
	}
	if attempts != 2 || m.failure != "" || m.profilesUI.url.Value() != "" {
		t.Fatal("successful retry did not clear the URL and error")
	}
}

func TestProfileCardEnterOpensDetailsAndApplyIsExplicit(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Applied: "default", Profiles: []string{"work"}, ProfileDetails: profileFixtures("default", "work")}}
	m := profileInteractionsModel(b, 120, 32)
	backend := &profileBackend{fakeBackend: b}
	m.backend, m.focus = backend, "profile.1"
	m, cmd := update(m, special(tea.KeyEnter))
	if cmd != nil || m.busy || backend.applied != 0 || m.snapshot.Applied != "default" || m.profilesUI.detail != "work" || m.focus != "profiles.apply.1" {
		t.Fatal("opening a profile card changed the runtime instead of showing its actions")
	}
	m, cmd = update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if backend.applied != 1 || !strings.Contains(m.message, "Configuration applied") {
		t.Fatal("explicit Use profile action did not invoke the shared Apply use case")
	}
}

func TestProfileRefreshButtonAndUpdateAllDoNotOpenOrApplyCards(t *testing.T) {
	var refreshed string
	refreshes, all := 0, 0
	b := &fakeBackend{
		snapshot: app.Snapshot{Installed: true, Applied: "default", Profiles: []string{"work"},
			ProfileDetails: []app.ProfileInfo{{ID: "default", Name: "default", Validated: true}, {ID: "work", Name: "work", Refreshable: true, Validated: true}}},
		refresh:            func(_ context.Context, name string) error { refreshed = name; refreshes++; return nil },
		refreshAllProfiles: func(context.Context) error { all++; return nil },
	}
	m := profileInteractionsModel(b, 160, 40)
	backend := &profileBackend{fakeBackend: b}
	m.backend = backend
	m, cmd := click(m, findTarget(t, m, "profiles.refresh.1"))
	m = finishProfileJob(t, m, cmd)
	if refreshed != "work" || refreshes != 1 || backend.applied != 0 || m.profilesUI.detail != "" || m.snapshot.Applied != "default" {
		t.Fatal("card refresh opened, selected, or applied its containing card")
	}
	// The result occupies the header action area until it is dismissed.
	m.dismissNotification()
	m.layout()
	m, cmd = click(m, findTarget(t, m, "profiles.update-all"))
	m = finishProfileJob(t, m, cmd)
	if all != 1 || backend.applied != 0 || m.snapshot.Applied != "default" || !strings.Contains(m.message, "apply a profile") {
		t.Fatal("Update all bypassed the shared refresh use case or changed the runtime")
	}
	m.focus = "profile.1"
	m, _ = update(m, special(tea.KeyEnter))
	m, cmd = click(m, findTarget(t, m, "profiles.update.1"))
	m = finishProfileJob(t, m, cmd)
	if refreshed != "work" || refreshes != 2 || backend.applied != 0 || m.profilesUI.detail != "work" {
		t.Fatal("details Update did not refresh the same profile while retaining its details")
	}
}

func TestProfileDetailTabControlsRemainReachableAcrossResize(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Profiles: []string{"alpha", "beta", "gamma"},
		ProfileDetails: []app.ProfileInfo{{ID: "default", Name: "default", Validated: true}, {ID: "alpha", Name: "alpha", Validated: true}, {ID: "beta", Name: "beta", Validated: true}, {ID: "gamma", Name: "gamma", Refreshable: true, Validated: true}}}}
	m := profileInteractionsModel(b, 160, 40)
	m.focus = "profile.3"
	m, _ = update(m, special(tea.KeyEnter))
	_, hits := m.pageBody(120)
	seen := map[string]bool{}
	for _, hit := range hits {
		if seen[hit.ID] {
			t.Fatalf("card and details share a focus target: %s", hit.ID)
		}
		seen[hit.ID] = true
	}
	for _, size := range [][2]int{{80, 24}, {60, 20}, {120, 32}, {160, 40}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if m.profilesUI.detail != "gamma" || m.focus != "profiles.apply.3" {
			t.Fatalf("resize retargeted the open profile at %v: detail=%s focus=%s", size, m.profilesUI.detail, m.focus)
		}
		findTarget(t, m, "profiles.apply.3")
		for _, id := range []string{"profiles.update.3", "profiles.rename.3", "profiles.description.3", "profiles.url-edit.3", "profiles.overrides.3", "profiles.delete.3", "profiles.close"} {
			m, _ = update(m, special(tea.KeyTab))
			if m.focus != id {
				t.Fatalf("Tab did not reach %s at %v; focused %s", id, size, m.focus)
			}
			findTarget(t, m, id)
		}
		for range 7 {
			m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		}
		if m.focus != "profiles.apply.3" {
			t.Fatalf("Shift+Tab did not return to Apply at %v: %s", size, m.focus)
		}
	}
}

func TestProfileURLCannotStartAnotherOperationWhileBusy(t *testing.T) {
	m := profileInteractionsModel(&fakeBackend{}, 120, 32)
	m.focus, m.busy = "profiles.url", true
	m.profilesUI.url.SetValue("https://example.com/new")
	before := m.id
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, key("q"))
	if m.form != "" || m.id != before || !m.quitting {
		t.Fatal("busy URL input opened a second job or swallowed Quit")
	}
	m = profileInteractionsModel(&fakeBackend{}, 120, 32)
	m.focus = "profiles.url"
	m.newJob("action")
	m, _ = update(m, special(tea.KeyEscape))
	if m.opCtx.Err() != nil || m.focus != "nav.profiles" {
		t.Fatal("Esc did not return to navigation while preserving the background operation")
	}
}

func TestProfileDetailFocusFollowsNameWhenSnapshotOrderChanges(t *testing.T) {
	for _, action := range []string{"apply", "update", "rename", "url-edit", "overrides", "delete"} {
		t.Run(action, func(t *testing.T) {
			initial := app.Snapshot{Installed: true, Profiles: []string{"alpha", "gamma"},
				ProfileDetails: []app.ProfileInfo{{ID: "default", Name: "default", Validated: true}, {ID: "alpha", Name: "alpha", Validated: true}, {ID: "gamma", Name: "gamma", Refreshable: true, Validated: true}}}
			m := profileInteractionsModel(&fakeBackend{snapshot: initial}, 160, 40)
			m.focus = "profile.2"
			m, _ = update(m, special(tea.KeyEnter))
			m.focus = "profiles." + action + ".2"
			next := initial
			next.Profiles = []string{"aardvark", "alpha", "gamma"}
			next.ProfileDetails = append(append([]app.ProfileInfo{}, initial.ProfileDetails...), profileFixtures("aardvark")...)
			m, _ = update(m, resultMsg{id: m.id, snapshot: next, hasSnapshot: true})
			if m.profilesUI.detail != "gamma" || m.focus != "profiles."+action+".3" {
				t.Fatalf("snapshot reorder retargeted %s to another profile: detail=%s focus=%s", action, m.profilesUI.detail, m.focus)
			}
			findTarget(t, m, m.focus)
		})
	}
}

func TestProfileDetailClosesIfExternallyDeleted(t *testing.T) {
	m := profileInteractionsModel(&fakeBackend{snapshot: app.Snapshot{Installed: true, Profiles: []string{"alpha", "gamma"}, ProfileDetails: profileFixtures("default", "alpha", "gamma")}}, 160, 40)
	m.focus = "profile.2"
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, resultMsg{id: m.id, snapshot: app.Snapshot{Installed: true, Profiles: []string{"alpha"}, ProfileDetails: profileFixtures("default", "alpha")}, hasSnapshot: true})
	if m.profilesUI.detail != "" || strings.HasPrefix(m.focus, "profiles.apply.") {
		t.Fatalf("deleted profile retained actionable details: detail=%s focus=%s", m.profilesUI.detail, m.focus)
	}
	findTarget(t, m, m.focus)
}

func TestProfileSelectedStatusDistinguishesStoppedFromUnavailableRuntime(t *testing.T) {
	for _, tc := range []struct {
		name         string
		active       bool
		apiError     string
		serviceError string
		status       string
	}{
		{name: "stopped", status: "SELECTED · Next start"},
		{name: "running", active: true, status: "IN USE"},
		{name: "API unavailable", active: true, apiError: "unavailable", status: "SELECTED · Runtime unavailable"},
		{name: "service unavailable", serviceError: "unavailable", status: "SELECTED · Runtime unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := profileInteractionsModel(&fakeBackend{snapshot: app.Snapshot{Installed: true, Applied: "work", Profiles: []string{"work"},
				ProfileDetails: profileFixtures("default", "work"),
				Service:        service.Status{Active: tc.active}, APIError: tc.apiError, ServiceError: tc.serviceError}}, 160, 40)
			card := ansi.Strip(strings.Join(m.profileCard(1, 64).lines, "\n"))
			if !strings.Contains(card, tc.status) {
				t.Fatalf("wrong selected state for %s; expected %s in %s", tc.name, tc.status, card)
			}
		})
	}
}
