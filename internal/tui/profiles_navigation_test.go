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
	"github.com/nelvko/lazyclash/internal/diagnostic"
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
	if len(cards) != 5 || cards[0].Y != cards[1].Y || cards[0].X == cards[1].X || cards[2].Y <= cards[0].Y {
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
	profileNavigationPress(t, &m, "end", 4)
	profileNavigationVisible(t, m)
	m.width, m.height = 160, 32
	m.layout()
	if m.focus != "profile.4" {
		t.Fatal("resizing changed the selected card")
	}
	profileNavigationPress(t, &m, "home", 0)
	profileNavigationVisible(t, m)
}

func TestWideProfileGridNavigationAfterResize(t *testing.T) {
	m := profileNavigationModel(240, 32)
	profileNavigationPress(t, &m, "down", 4)
	profileNavigationPress(t, &m, "up", 0)
	for i := 1; i < 4; i++ {
		profileNavigationPress(t, &m, "right", i)
	}
	profileNavigationPress(t, &m, "down", 3) // Only the first slot in the last row is occupied.
	m = profileMenuRightClick(t, m, "profiles.menu.3")
	if m.profilesUI.menuProfile != "delta" {
		t.Fatalf("fourth-column menu opened for %q", m.profilesUI.menuProfile)
	}
	m, _ = update(m, special(tea.KeyEscape))
	m, _ = update(m, tea.WindowSizeMsg{Width: 200, Height: 32})
	if m.focus != "profile.3" {
		t.Fatalf("reflow changed the focused card to %s", m.focus)
	}
	profileNavigationPress(t, &m, "up", 0)
	profileNavigationPress(t, &m, "right", 1)
	profileNavigationPress(t, &m, "down", 4)
	card, empty := findTarget(t, m, "profile.4"), findTarget(t, m, "profile.2")
	m, _ = update(m, tea.MouseClickMsg{X: empty.X + 3, Y: card.Y + 2, Button: tea.MouseRight})
	if m.menu.open {
		t.Fatal("empty third slot in the last row opened a card menu")
	}
	m = profileMenuRightClick(t, m, card.ID)
	if m.profilesUI.menuProfile != "epsilon" {
		t.Fatalf("last-row menu opened for %q", m.profilesUI.menuProfile)
	}
}

func TestProfileGridNavigationReachesCardsOutsideClippedTargets(t *testing.T) {
	m := profileNavigationModel(80, 20)
	for _, hit := range m.targets {
		if hit.ID == "profile.4" {
			t.Fatal("fixture did not put the last card below the fold")
		}
	}
	profileNavigationPress(t, &m, "end", 4)
	if m.scroll == 0 {
		t.Fatal("moving to an offscreen card did not scroll the body")
	}
	profileNavigationVisible(t, m)
	m.layout()
	findTarget(t, m, "profile.4")
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
	for _, state := range []string{"other-page", "navigation", "sidebar", "form", "inline-input", "ports", "wizard", "network", "quitting"} {
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
	for _, target := range []string{"profiles.url.clear", "profiles.url.paste", "profiles.add"} {
		m, _ = update(m, special(tea.KeyTab))
		if m.focus != target || m.profilesUI.editing || m.profilesUI.url.Focused() {
			t.Fatalf("Tab focused %s instead of %s", m.focus, target)
		}
	}
	for _, target := range []string{"profiles.url.paste", "profiles.url.clear"} {
		m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		if m.focus != target {
			t.Fatalf("Shift+Tab focused %s instead of %s", m.focus, target)
		}
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
	b := &fakeBackend{add: func(_ context.Context, name, source string) error {
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
	b := &fakeBackend{add: func(context.Context, string, string) error {
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

func TestProfileRefreshControlsKeepTheCurrentSelection(t *testing.T) {
	var refreshed string
	refreshes, all := 0, 0
	b := &fakeBackend{
		snapshot: app.Snapshot{Installed: true, Applied: "default", Profiles: []string{"work"},
			ProfileDetails: []app.ProfileInfo{{ID: "default", Name: "default", Validated: true}, {ID: "work", Name: "work", Validated: true}}},
		refresh: func(_ context.Context, name string) error { refreshed = name; refreshes++; return nil },
		refreshAllProfiles: func(context.Context) (app.ProfileRefreshSummary, error) {
			all++
			return app.ProfileRefreshSummary{Updated: 1}, nil
		},
	}
	m := profileInteractionsModel(b, 160, 40)
	backend := &profileBackend{fakeBackend: b}
	m.backend = backend
	m, cmd := click(m, findTarget(t, m, "profiles.refresh.0"))
	m = finishProfileJob(t, m, cmd)
	if refreshed != "work" || refreshes != 1 || backend.applied != 0 || m.form != "" || m.snapshot.Applied != "default" {
		t.Fatal("card refresh opened, selected, or applied its containing card")
	}
	if len(m.notification.items) != 1 || m.notification.items[0].Message != m.tr("feedback.refreshed", "work") {
		t.Fatal("single-profile refresh did not report its own result", m.notification.items)
	}
	// Dismiss the single-profile result before using the header button beneath it.
	closeID := "notification.close." + strconv.FormatUint(m.notification.items[0].ID, 10)
	m, _ = click(m, findTarget(t, m, closeID))
	m, cmd = click(m, findTarget(t, m, "profiles.update-all"))
	m = finishProfileJob(t, m, cmd)
	if all != 1 || backend.applied != 0 || m.snapshot.Applied != "default" || m.message != m.tr("feedback.refreshed_all", 1) {
		t.Fatal("Update all bypassed the shared refresh use case or changed the runtime")
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

func TestSelectedProfileWarnsOnlyOnRuntimeErrors(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		active                 bool
		apiError, serviceError string
		warn                   bool
	}{
		{name: "stopped", apiError: "Start the service to view proxy groups"},
		{name: "running", active: true},
		{name: "API unavailable", active: true, apiError: "unavailable", warn: true},
		{name: "service unavailable", serviceError: "unavailable", warn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := profileInteractionsModel(&fakeBackend{snapshot: app.Snapshot{Installed: true, Applied: "work", Profiles: []string{"work"}, ProfileDetails: profileFixtures("work"), Service: service.Status{Active: tc.active}, APIError: tc.apiError, ServiceError: tc.serviceError}}, 160, 40)
			text := ansi.Strip(strings.Join(m.profileCard(0, 64).lines, "\n"))
			if strings.Contains(text, m.tr("profiles.selected_unavailable")) != tc.warn || strings.Contains(text, "IN USE") || strings.Contains(text, "SELECTED") {
				t.Fatalf("%s: unexpected state on card: %s", tc.name, text)
			}
		})
	}
}

func TestProfileRefreshBatchShowsCommittedCountAndFailedDetails(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Profiles: []string{"work"}, ProfileDetails: []app.ProfileInfo{{ID: "work", Name: "Work"}}},
		refreshAllProfiles: func(context.Context) (app.ProfileRefreshSummary, error) {
			return app.ProfileRefreshSummary{Updated: 2, Failed: 1}, errors.New("Work: download unavailable")
		}}
	m := profileInteractionsModel(b, 120, 32)
	m, cmd := click(m, findTarget(t, m, "profiles.update-all"))
	m = finishProfileJob(t, m, cmd)
	if len(m.notification.items) != 1 {
		t.Fatal("batch notifications", m.notification.items)
	}
	n := m.notification.items[0]
	if n.Kind != "error" || n.Message != m.tr("feedback.refresh_partial", 2, 1) || n.Details != "Work: download unavailable" {
		t.Fatalf("batch lost the committed count or per-profile failure: %+v", n)
	}
}

func TestProfileRefreshCancellationOnlySilencesUncommittedWork(t *testing.T) {
	for _, tt := range []struct {
		label, kind string
		err         error
		failed      int
	}{
		{"canceled validation", "info", diagnostic.New("diagnostic.profile.validate", "validation: %w", context.Canceled), 0},
		{"saved but not applied", "error", diagnostic.New("diagnostic.profile.refreshNotApplied", "source saved, runtime unchanged: %w", context.Canceled), 1},
		{"recovery failed", "error", errors.Join(context.Canceled, errors.New("service recovery failed")), 1},
	} {
		t.Run(tt.label, func(t *testing.T) {
			b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Profiles: []string{"work"}, ProfileDetails: []app.ProfileInfo{{ID: "work", Name: "Work"}}},
				refreshAllProfiles: func(context.Context) (app.ProfileRefreshSummary, error) {
					return app.ProfileRefreshSummary{Failed: tt.failed}, tt.err
				}}
			m := profileInteractionsModel(b, 120, 32)
			m, cmd := click(m, findTarget(t, m, "profiles.update-all"))
			m = finishProfileJob(t, m, cmd)
			if len(m.notification.items) != 1 {
				t.Fatal(m.notification.items)
			}
			item := m.notification.items[0]
			if item.Kind != tt.kind || item.Silent != (tt.failed == 0) {
				t.Fatal("incorrect cancellation disposition", item)
			}
			if tt.failed != 0 && (item.Message != m.tr("feedback.refresh_partial", 0, 1) || item.Details != tt.err.Error()) {
				t.Fatal("missing partial failure", item)
			}
		})
	}
}
