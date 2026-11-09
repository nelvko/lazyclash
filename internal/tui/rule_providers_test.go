package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type ruleProvidersTestBackend struct {
	*fakeBackend
	providers []mihomo.RuleProvider
	read      func(context.Context) (app.RuleProviderSnapshot, error)
	update    func(context.Context, string) error
}

func (b *ruleProvidersTestBackend) RuleProviders(ctx context.Context) (app.RuleProviderSnapshot, error) {
	if b.read != nil {
		return b.read(ctx)
	}
	return app.RuleProviderSnapshot{State: "ready", Providers: append([]mihomo.RuleProvider(nil), b.providers...)}, ctx.Err()
}

func (b *ruleProvidersTestBackend) UpdateRuleProvider(ctx context.Context, name string) error {
	if b.update != nil {
		return b.update(ctx, name)
	}
	return ctx.Err()
}

func ruleProvidersModel(t *testing.T, count int) (Model, *ruleProvidersTestBackend) {
	t.Helper()
	backend := &ruleProvidersTestBackend{fakeBackend: &fakeBackend{}}
	for index := 0; index < count; index++ {
		vehicle := "HTTP"
		if index == count-1 {
			vehicle = "Inline"
		} else if index == 1 {
			vehicle = "File"
		}
		backend.providers = append(backend.providers, mihomo.RuleProvider{
			Name: fmt.Sprintf("provider-%02d-中文", index), Type: "Rule", VehicleType: vehicle,
			Behavior: "Domain", Format: "yaml", RuleCount: 100 + index,
			UpdatedAt: time.Date(2026, 11, 6, 8, 30, 0, 0, time.UTC),
		})
	}
	m := rulesModel(t, backend.fakeBackend)
	m.backend = backend
	m.width, m.height, m.scroll = 160, 45, 0
	m.logoIntro.used, m.message = true, ""
	m.notification.items = nil
	m.notification.source = m.notificationSource()
	m.providers = newProviderPanelState(m.locale)
	m.providers.open, m.providers.active, m.providers.received = true, true, true
	m.providers.generation = 7
	m.providers.snapshot = ruleProviderSnapshot(app.RuleProviderSnapshot{State: "ready", Providers: backend.providers})
	m.providers.source = m.providerSource()
	m.reconcileProviders()
	m.focus = "provider.row." + m.providers.selected
	return m, backend
}

func providerMessages(command tea.Cmd) []tea.Msg {
	if command == nil {
		return nil
	}
	message := command()
	if batch, ok := message.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, next := range batch {
			messages = append(messages, providerMessages(next)...)
		}
		return messages
	}
	return []tea.Msg{message}
}

func TestRulesProvidersReadCancelsAndRejectsStaleRequests(t *testing.T) {
	m, backend := ruleProvidersModel(t, 4)
	m.providers.active, m.providers.received = false, false
	var requests []context.Context
	backend.read = func(ctx context.Context) (app.RuleProviderSnapshot, error) {
		requests = append(requests, ctx)
		return app.RuleProviderSnapshot{State: "ready", Providers: backend.providers}, nil
	}
	first := m.syncProviders()
	if first == nil || !m.providers.pending || m.syncProviders() != nil {
		t.Fatal("provider reader did not start exactly one request")
	}
	old := first().(providersResultMsg)
	second := m.retryProviders()
	if requests[0].Err() == nil || second == nil {
		t.Fatal("explicit reload did not cancel its prior request")
	}
	m.acceptProviders(old)
	if m.providers.received || !m.providers.pending {
		t.Fatal("old read result replaced the fresh request")
	}
	fresh := second().(providersResultMsg)
	if m.acceptProviders(fresh) == nil || m.providers.pending || !m.providers.received || len(m.filteredProviders()) != 4 {
		t.Fatal("fresh provider observation did not publish or schedule polling")
	}
	third := m.providersCmd()
	late := third().(providersResultMsg)
	m.stopProviders()
	generation := m.providers.generation
	m.acceptProviders(late)
	if m.providers.active || m.providers.pending || generation != m.providers.generation {
		t.Fatal("response after leaving reactivated the provider reader")
	}
}

func TestRulesProvidersReopeningWaitsForCurrentCoreObservation(t *testing.T) {
	m, backend := ruleProvidersModel(t, 3)
	oldName := backend.providers[0].Name
	m.providers.search.SetValue("provider")
	m.providersActivate("providers.back")
	backend.providers = []mihomo.RuleProvider{{Name: "current-provider", VehicleType: "HTTP", Behavior: "Domain"}}
	command := m.openProviders(providerRules)
	if command == nil || m.providers.received || len(m.filteredProviders()) != 0 || m.providersCanUpdateAll() {
		t.Fatal("reopened panel exposed cached mutation targets before reading the current core")
	}
	if m.providers.search.Value() != "provider" || m.providers.selected != oldName {
		t.Fatal("reopening unexpectedly discarded the prior query or selection")
	}
	if m.providersActivate("provider.update."+oldName) != nil {
		t.Fatal("cached source remained mutable before a current observation")
	}
	m.acceptProviders(command().(providersResultMsg))
	if list := m.filteredProviders(); len(list) != 1 || list[0].Name != "current-provider" || m.providers.selected != "current-provider" {
		t.Fatal("fresh observation did not replace cached sources and reconcile selection")
	}
}

func TestRulesProvidersReloadDiscardsPriorPollingTimer(t *testing.T) {
	m, _ := ruleProvidersModel(t, 3)
	first := m.providersCmd()().(providersResultMsg)
	m.acceptProviders(first)
	second := m.retryProviders()().(providersResultMsg)
	m.acceptProviders(second)
	m, _ = update(m, providersTickMsg{kind: first.kind, source: first.source, generation: first.generation, request: first.request})
	if m.providers.pending {
		t.Fatal("a superseded read timer started another polling chain")
	}
	m, command := update(m, providersTickMsg{kind: second.kind, source: second.source, generation: second.generation, request: second.request})
	if command == nil || !m.providers.pending {
		t.Fatal("the current read timer did not continue polling")
	}
}

func TestRulesProvidersUpdateAllIsBoundedDeduplicatedAndReportsFailures(t *testing.T) {
	m, backend := ruleProvidersModel(t, 8)
	calls := make(map[string]int)
	fail := backend.providers[1].Name
	backend.update = func(ctx context.Context, name string) error {
		calls[name]++
		if name == fail && calls[name] == 1 {
			return errors.New("fixture provider unavailable")
		}
		return ctx.Err()
	}
	command := m.providersActivate("providers.all")
	if m.providers.running() != 3 || len(m.providers.queue) != 4 {
		t.Fatalf("bulk update did not reserve three slots: running=%d queued=%d", m.providers.running(), len(m.providers.queue))
	}
	if duplicate := m.providersActivate("providers.all"); duplicate != nil || len(m.providers.queue) != 4 {
		t.Fatal("duplicate Update All created more work")
	}
	messages := providerMessages(command)
	if len(messages) != 3 || len(calls) != 3 {
		t.Fatal("the initial batch exceeded its three-worker budget")
	}
	for len(messages) > 0 {
		message := messages[0]
		messages = messages[1:]
		if result, ok := message.(providerUpdateResultMsg); ok {
			messages = append(messages, providerMessages(m.acceptProviderUpdate(result))...)
		}
		if m.providers.running() > 3 {
			t.Fatal("a completion overfilled the update worker budget")
		}
	}
	if len(calls) != 7 || calls[backend.providers[7].Name] != 0 || m.providers.running() != 0 || len(m.providers.queue) != 0 {
		t.Fatalf("updates omitted a source or included Inline: %#v", calls)
	}
	if m.providers.updates[fail].state != "failed" || m.providers.updates[backend.providers[0].Name].state != "success" {
		t.Fatal("individual failure erased another provider's success")
	}
	for _, result := range providerMessages(m.providersActivate("provider.update." + fail)) {
		m.acceptProviderUpdate(result.(providerUpdateResultMsg))
	}
	if calls[fail] != 2 || m.providers.updates[fail].state != "success" {
		t.Fatal("retry did not update exactly its failed source")
	}
}

func TestRulesProvidersDepartureCancelsQueueAndLateUpdates(t *testing.T) {
	m, backend := ruleProvidersModel(t, 6)
	var contexts []context.Context
	backend.update = func(ctx context.Context, name string) error {
		contexts = append(contexts, ctx)
		return nil
	}
	results := providerMessages(m.providersActivate("providers.all"))
	if len(contexts) != 3 {
		t.Fatal("fixture did not start a full batch")
	}
	m.providersActivate("providers.back")
	for _, ctx := range contexts {
		if ctx.Err() == nil {
			t.Fatal("leaving provider view did not cancel a running update")
		}
	}
	for _, message := range results {
		if m.acceptProviderUpdate(message.(providerUpdateResultMsg)) != nil {
			t.Fatal("late update scheduled work after leaving")
		}
	}
	if m.providers.open || m.focus != "rules.providers" || m.providers.running() != 0 || len(m.providers.queue) != 0 || len(m.providers.updates) != 0 {
		t.Fatal("departed view retained pending or successful update indicators")
	}
}

func TestRulesProvidersQuitAndCancellationStopReadsAndUpdates(t *testing.T) {
	for _, trigger := range []struct {
		name string
		stop func(*Model)
	}{
		{"quit", func(m *Model) { *m, _ = update(*m, key("q")) }},
		{"interrupt", func(m *Model) { *m, _ = update(*m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}) }},
		{"cancel task", func(m *Model) { m.cancelTask(true) }},
		{"parent canceled", func(m *Model) { *m, _ = update(*m, parentCanceledMsg{}) }},
	} {
		t.Run(trigger.name, func(t *testing.T) {
			m, backend := ruleProvidersModel(t, 7)
			var contexts []context.Context
			backend.read = func(ctx context.Context) (app.RuleProviderSnapshot, error) {
				contexts = append(contexts, ctx)
				return app.RuleProviderSnapshot{State: "ready", Providers: backend.providers}, nil
			}
			backend.update = func(ctx context.Context, name string) error {
				contexts = append(contexts, ctx)
				return nil
			}
			read := m.providersCmd()().(providersResultMsg)
			updates := providerMessages(m.providersActivate("providers.all"))
			if len(contexts) != 4 || len(m.providers.queue) != 3 {
				t.Fatal("fixture did not begin one read, three updates and queued work")
			}
			trigger.stop(&m)
			for _, ctx := range contexts {
				if ctx.Err() == nil {
					t.Fatal("exit path retained a provider request context")
				}
			}
			if m.providers.active || m.providers.pending || m.providers.running() != 0 || len(m.providers.queue) != 0 {
				t.Fatal("exit path retained provider work")
			}
			if m.acceptProviders(read) != nil {
				t.Fatal("late provider read restarted polling after cancellation")
			}
			for _, message := range updates {
				if m.acceptProviderUpdate(message.(providerUpdateResultMsg)) != nil {
					t.Fatal("late provider update scheduled work after cancellation")
				}
			}
		})
	}
}

func TestRulesProvidersSearchPreservesNamesAndInlineIsReadOnly(t *testing.T) {
	m, backend := ruleProvidersModel(t, 4)
	m.providers.search.SetValue("inline")
	m.reconcileProviders()
	list := m.filteredProviders()
	if len(list) != 1 || list[0].Name != backend.providers[3].Name {
		t.Fatal("provider source type search lost its exact provider")
	}
	if m.providersActivate("provider.update."+list[0].Name) != nil || len(m.providers.updates) != 0 {
		t.Fatal("Inline provider exposed an update operation")
	}
	body := m.providersBodyWindow(100, 0, -1)
	for _, target := range body.targets {
		if strings.HasPrefix(target.ID, "provider.update.") {
			t.Fatal("Inline row included a mouse update target")
		}
	}
	m.providers.search.SetValue("provider-01")
	m.reconcileProviders()
	if m.providers.selected != backend.providers[1].Name {
		t.Fatal("filtered provider selection did not reconcile by name")
	}
	m.providers.editing = true
	m.providers.search.Focus()
	m.providersSearchKey(special(tea.KeyEnter))
	if m.providers.editing || m.providers.search.Focused() || m.focus != "provider.row."+backend.providers[1].Name {
		t.Fatal("finishing search did not focus its matching provider")
	}
}

func TestRulesProvidersNarrowRowsKeepNamesWithoutInvisibleUpdateTargets(t *testing.T) {
	m, _ := ruleProvidersModel(t, 3)
	provider := m.providers.snapshot.Providers[0]
	label, _ := m.providerUpdateLabel(provider)
	for _, width := range []int{1, 5, 10, ansi.StringWidth(label) + 7} {
		body := m.providersBodyWindow(width, 0, -1)
		for _, target := range body.targets {
			if strings.HasPrefix(target.ID, "provider.update.") {
				t.Fatalf("width %d retained an action where its button cannot fit", width)
			}
		}
		for _, line := range body.lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflowed: %q", width, ansi.Strip(line))
			}
		}
	}
	m.providersActivate("provider.row." + provider.Name)
	if command := m.startProviderUpdates([]string{provider.Name}); command == nil {
		t.Fatal("hiding narrow mouse actions also disabled the selected provider update")
	}
}

func TestRulesProvidersWindowMatchesCompleteBody(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {80, 24}, {60, 20}, {40, 16}, {40, 9}, {20, 7}, {5, 2}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m, backend := ruleProvidersModel(t, 35)
			m.width, m.height = size[0], size[1]
			m.providers.updates[backend.providers[1].Name] = providerUpdate{state: "failed", err: errors.New("fixture update failure")}
			for _, offset := range []int{0, 1, 2, 3, 9, 87, 10000} {
				m.scroll = offset
				o := m.navigationOptions()
				o.Scroll = offset
				g := dashboardGeometry(o)
				if o.BodyTopRows > 0 {
					o.BodyTop = m.providersToolbar(g.width, o.BodyTopRows)
				}
				full := m.providersBodyWindow(g.width, 0, -1)
				window := m.providersBodyWindow(g.width, offset, g.height)
				o.Body, o.BodyTargets = strings.Join(full.lines, "\n"), full.targets
				want, wantHits, wantTotal := renderDashboard(o)
				o.Body, o.BodyTargets, o.BodyStart, o.BodyTotal = strings.Join(window.lines, "\n"), window.targets, window.start, window.total
				got, gotHits, gotTotal := renderDashboard(o)
				if got != want || !reflect.DeepEqual(gotHits, wantHits) || gotTotal != wantTotal {
					t.Fatalf("provider viewport differs at offset %d", offset)
				}
				assertCanvas(t, got, size[0], size[1])
				assertRegions(t, gotHits, size[0], size[1])
			}
		})
	}
}

func TestRulesProvidersUnavailableDropsRowsAndQueuedUpdates(t *testing.T) {
	m, _ := ruleProvidersModel(t, 6)
	m.providersActivate("providers.all")
	m.providers.pending, m.providers.request = true, 2
	m.acceptProviders(providersResultMsg{generation: m.providers.generation, request: 2, kind: m.providers.kind, source: m.providers.source, snapshot: providerSnapshot{State: "stopped"}})
	if len(m.filteredProviders()) != 0 || len(m.providers.updates) != 0 || m.providers.running() != 0 || len(m.providers.queue) != 0 {
		t.Fatal("stopped core retained old provider data or queued mutations")
	}
	body := m.providersBodyWindow(100, 0, -1)
	plain := ansi.Strip(strings.Join(body.lines, "\n"))
	if strings.Contains(plain, "provider-00") {
		t.Fatal("stopped core displayed stale provider names")
	}
}

func TestRulesProvidersPageOwnsSearchKeysAndExactUpdateActions(t *testing.T) {
	m, backend := ruleProvidersModel(t, 5)
	m.providers.open, m.focus = false, "rules.providers"
	m.providers.active, m.providers.received = false, false
	m.providers.snapshot = providerSnapshot{}
	m.layout()
	var updated []string
	backend.update = func(ctx context.Context, name string) error {
		updated = append(updated, name)
		return ctx.Err()
	}
	var command tea.Cmd
	m, command = update(m, special(tea.KeyEnter))
	for _, result := range providerMessages(command) {
		if _, ok := result.(providersResultMsg); ok {
			m, _ = update(m, result)
		}
	}
	if !m.providers.open || !m.providers.received || !hasAction(m.targets, "providers.search") {
		t.Fatal("header entry did not load and render its provider view")
	}
	m, _ = update(m, key("/"))
	for _, value := range []string{"q", "1", "2", "3"} {
		m, _ = update(m, key(value))
	}
	if m.quitting || m.page != "rules" || m.providers.search.Value() != "q123" {
		t.Fatal("provider search input leaked into global navigation or quit")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m, _ = update(m, tea.PasteMsg{Content: "inline"})
	m, _ = update(m, special(tea.KeyEnter))
	if m.providers.editing || m.providers.selected != backend.providers[4].Name {
		t.Fatal("provider search did not select its matching source")
	}
	m, command = update(m, key("u"))
	if command != nil || len(updated) != 0 {
		t.Fatal("Inline shortcut started an update")
	}
	m, _ = update(m, key("/"))
	m, _ = update(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m, _ = update(m, special(tea.KeyEnter))
	m.layout()
	file := backend.providers[1].Name
	m, command = click(m, findTarget(t, m, "provider.update."+file))
	for _, result := range providerMessages(command) {
		if _, ok := result.(providerUpdateResultMsg); ok {
			m, command = update(m, result)
		}
	}
	if !reflect.DeepEqual(updated, []string{file}) || m.providers.updates[file].state != "success" || !m.providers.open {
		t.Fatalf("mouse update reached the wrong provider: %#v", updated)
	}
	for _, result := range providerMessages(command) {
		if _, ok := result.(providersResultMsg); ok {
			m, _ = update(m, result)
		}
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.providers.open || m.providers.active || m.providers.running() != 0 {
		t.Fatal("provider Back did not return to the current effective rules")
	}
}

func TestRulesProvidersReturningToNavigationReleasesSearchKeys(t *testing.T) {
	for _, navigate := range []struct {
		name string
		goTo func(*Model)
	}{
		{"current page", func(m *Model) { m.browsePage("rules") }},
		{"navigation", func(m *Model) { m.returnToNavigation() }},
	} {
		t.Run(navigate.name, func(t *testing.T) {
			m, _ := ruleProvidersModel(t, 5)
			m.focusProvidersSearch()
			navigate.goTo(&m)
			if m.providers.editing || m.providers.search.Focused() || m.focus != "nav.rules" {
				t.Fatal("navigation retained ownership in the provider search input")
			}
			m, _ = update(m, special(tea.KeyDown))
			if m.page != "logs" || m.focus != "nav.logs" {
				t.Fatal("provider search consumed a navigation arrow after losing focus")
			}
		})
	}
}

func TestRulesProvidersTabSelectionMatchesItsFocusedRow(t *testing.T) {
	m, backend := ruleProvidersModel(t, 5)
	var updated []string
	backend.update = func(ctx context.Context, name string) error {
		updated = append(updated, name)
		return ctx.Err()
	}
	m.providersActivate("provider.row." + backend.providers[3].Name)
	m.focus = "providers.search"
	m.layout()
	for range len(m.focusTargets()) {
		m.cycleFocus(1)
		if !strings.HasPrefix(m.focus, "provider.row.") {
			continue
		}
		if name := strings.TrimPrefix(m.focus, "provider.row."); m.providers.selected != name || m.filteredProviders()[m.providers.selectedIndex].Name != name {
			t.Fatal("Tab focused one provider while row shortcuts still targeted another")
		}
		_, command := update(m, key("u"))
		providerMessages(command)
		if !reflect.DeepEqual(updated, []string{strings.TrimPrefix(m.focus, "provider.row.")}) {
			t.Fatalf("update shortcut did not target the row reached by Tab: %#v", updated)
		}
		return
	}
	t.Fatal("Tab traversal never reached the provider list")
}

func TestRulesProvidersModalKeepsFixedControlsAndClipsEverySize(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {110, 30}, {80, 24}, {60, 20}, {40, 16}, {24, 13}, {20, 7}, {5, 2}, {1, 1}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m, _ := ruleProvidersModel(t, 35)
			m.width, m.height = size[0], size[1]
			m.layout()
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
			close := findTarget(t, m, "providers.back")
			if size[0] >= 24 && size[1] >= 13 {
				search := findTarget(t, m, "providers.search")
				all := findTarget(t, m, "providers.all")
				m.providers.viewport.GotoBottom()
				m.layout()
				if findTarget(t, m, "providers.back") != close || findTarget(t, m, "providers.search") != search || findTarget(t, m, "providers.all") != all {
					t.Fatal("provider scrolling moved the fixed header or footer")
				}
				if m.providers.bounds.W != size[0] && size[0] < 110 {
					t.Fatal("narrow terminal did not use the full screen")
				}
			}
			m.providersKey(special(tea.KeyEscape))
			if m.providers.open || m.focus != "rules.providers" {
				t.Fatal("Escape did not close the provider modal at a supported size")
			}
		})
	}
}

func TestRulesProvidersModalWheelOnlyScrollsItsBody(t *testing.T) {
	m, _ := ruleProvidersModel(t, 35)
	m.layout()
	p := &m.providers
	for _, point := range [][2]int{{0, 0}, {p.bounds.X + 3, p.bounds.Y + 1}, {p.bounds.X + 3, p.bounds.Y + p.bounds.H - 2}} {
		m.providersMouse(tea.MouseWheelMsg{X: point[0], Y: point[1], Button: tea.MouseWheelDown})
		if p.viewport.YOffset() != 0 {
			t.Fatal("wheel outside the provider body moved its viewport")
		}
	}
	m.providersMouse(tea.MouseWheelMsg{X: p.bodyBounds.X + 1, Y: p.bodyBounds.Y + 1, Button: tea.MouseWheelDown})
	if p.viewport.YOffset() == 0 {
		t.Fatal("wheel inside the provider body did not move its viewport")
	}
	m.layout()
	m.providersKey(special(tea.KeyEnd))
	m.layout()
	last := m.filteredProviders()[34]
	if !hasAction(m.targets, "provider.row."+last.Name) {
		t.Fatal("End did not reveal its selected provider inside the modal viewport")
	}
}

func TestRulesProvidersModalSearchEscapeThenClose(t *testing.T) {
	m, _ := ruleProvidersModel(t, 5)
	m.layout()
	m.focusProvidersSearch()
	m.providersKey(key("q"))
	if !m.providers.open || m.providers.search.Value() != "q" {
		t.Fatal("search q closed the provider modal")
	}
	m.providersKey(special(tea.KeyEscape))
	if !m.providers.open || m.providers.editing || m.providers.search.Focused() {
		t.Fatal("first Escape should end provider search editing")
	}
	m.providersKey(key("q"))
	if m.providers.open || m.quitting || m.focus != "rules.providers" {
		t.Fatal("normal q should close the modal without quitting the application")
	}
}

func TestRulesProvidersEndingSearchRevealsItsFocusedProvider(t *testing.T) {
	for _, finish := range []tea.KeyPressMsg{special(tea.KeyEnter), special(tea.KeyEscape)} {
		t.Run(finish.String(), func(t *testing.T) {
			m, backend := ruleProvidersModel(t, 35)
			m.layout()
			m.providersKey(special(tea.KeyEnd))
			m.providersKey(special(tea.KeyUp))
			m.layout()
			selected := m.providers.selected
			m.focusProvidersSearch()
			m.layout()
			if m.providers.viewport.YOffset() != 0 {
				t.Fatal("starting provider search did not reveal its top")
			}
			m.providersKey(finish)
			m.layout()
			if m.focus != "provider.row."+selected || !hasAction(m.targets, m.focus) {
				t.Fatal("ending unchanged search left its focused provider outside the viewport")
			}
			var updated string
			backend.update = func(_ context.Context, name string) error { updated = name; return nil }
			_, command := m.providersKey(key("u"))
			providerMessages(command)
			if updated != selected {
				t.Fatalf("update targeted %q instead of the visible focused provider %q", updated, selected)
			}
		})
	}
}

func TestRulesProvidersResizeRevealsFocusWithoutPinningWheelScrolling(t *testing.T) {
	m, _ := ruleProvidersModel(t, 35)
	m.layout()
	m.providersKey(special(tea.KeyEnd))
	m.layout()
	focused := m.focus
	m.width, m.height = 80, 24
	m.layout()
	if m.focus != focused || !hasAction(m.targets, focused) {
		t.Fatal("shrinking the provider modal hid its focused provider")
	}
	m.providers.viewport.GotoTop()
	m.layout()
	if m.providers.viewport.YOffset() != 0 || hasAction(m.targets, focused) {
		t.Fatal("a normal render overrode independent provider scrolling")
	}
	m.width, m.height = 40, 16
	m.layout()
	if !hasAction(m.targets, focused) {
		t.Fatal("a subsequent provider resize did not restore focus visibility")
	}
}
