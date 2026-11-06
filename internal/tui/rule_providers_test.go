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
	m.backend, m.rules.view = backend, "providers"
	m.width, m.height, m.scroll = 160, 45, 0
	m.logoIntro.used, m.message = true, ""
	m.notification.items = nil
	m.notification.source = m.notificationSource()
	m.ruleProviders = newRuleProvidersState(m.locale)
	m.ruleProviders.active, m.ruleProviders.received = true, true
	m.ruleProviders.generation = 7
	m.ruleProviders.snapshot = app.RuleProviderSnapshot{State: "ready", Providers: backend.providers}
	m.reconcileRuleProviders()
	m.focus = "rules.provider." + m.ruleProviders.selected
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
	m.ruleProviders.active, m.ruleProviders.received = false, false
	var requests []context.Context
	backend.read = func(ctx context.Context) (app.RuleProviderSnapshot, error) {
		requests = append(requests, ctx)
		return app.RuleProviderSnapshot{State: "ready", Providers: backend.providers}, nil
	}
	first := m.syncRuleProviders()
	if first == nil || !m.ruleProviders.pending || m.syncRuleProviders() != nil {
		t.Fatal("provider reader did not start exactly one request")
	}
	old := first().(ruleProvidersResultMsg)
	second := m.retryRuleProviders()
	if requests[0].Err() == nil || second == nil {
		t.Fatal("explicit reload did not cancel its prior request")
	}
	m.acceptRuleProviders(old)
	if m.ruleProviders.received || !m.ruleProviders.pending {
		t.Fatal("old read result replaced the fresh request")
	}
	fresh := second().(ruleProvidersResultMsg)
	if m.acceptRuleProviders(fresh) == nil || m.ruleProviders.pending || !m.ruleProviders.received || len(m.filteredRuleProviders()) != 4 {
		t.Fatal("fresh provider observation did not publish or schedule polling")
	}
	third := m.ruleProvidersCmd()
	late := third().(ruleProvidersResultMsg)
	m.stopRuleProviders()
	generation := m.ruleProviders.generation
	m.acceptRuleProviders(late)
	if m.ruleProviders.active || m.ruleProviders.pending || generation != m.ruleProviders.generation {
		t.Fatal("response after leaving reactivated the provider reader")
	}
}

func TestRulesProvidersReopeningWaitsForCurrentCoreObservation(t *testing.T) {
	m, backend := ruleProvidersModel(t, 3)
	oldName := backend.providers[0].Name
	m.ruleProviders.search.SetValue("provider")
	m.ruleProvidersActivate("rules.providers-back")
	backend.providers = []mihomo.RuleProvider{{Name: "current-provider", VehicleType: "HTTP", Behavior: "Domain"}}
	command := m.openRuleProviders()
	if command == nil || m.ruleProviders.received || len(m.filteredRuleProviders()) != 0 || m.ruleProvidersCanUpdateAll() {
		t.Fatal("reopened panel exposed cached mutation targets before reading the current core")
	}
	if m.ruleProviders.search.Value() != "provider" || m.ruleProviders.selected != oldName {
		t.Fatal("reopening unexpectedly discarded the prior query or selection")
	}
	if m.ruleProvidersActivate("rules.provider-update."+oldName) != nil {
		t.Fatal("cached source remained mutable before a current observation")
	}
	m.acceptRuleProviders(command().(ruleProvidersResultMsg))
	if list := m.filteredRuleProviders(); len(list) != 1 || list[0].Name != "current-provider" || m.ruleProviders.selected != "current-provider" {
		t.Fatal("fresh observation did not replace cached sources and reconcile selection")
	}
}

func TestRulesProvidersReloadDiscardsPriorPollingTimer(t *testing.T) {
	m, _ := ruleProvidersModel(t, 3)
	first := m.ruleProvidersCmd()().(ruleProvidersResultMsg)
	m.acceptRuleProviders(first)
	second := m.retryRuleProviders()().(ruleProvidersResultMsg)
	m.acceptRuleProviders(second)
	m, _ = update(m, ruleProvidersTickMsg{generation: first.generation, request: first.request})
	if m.ruleProviders.pending {
		t.Fatal("a superseded read timer started another polling chain")
	}
	m, command := update(m, ruleProvidersTickMsg{generation: second.generation, request: second.request})
	if command == nil || !m.ruleProviders.pending {
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
	command := m.ruleProvidersActivate("rules.providers-all")
	if m.ruleProviders.running() != 3 || len(m.ruleProviders.queue) != 4 {
		t.Fatalf("bulk update did not reserve three slots: running=%d queued=%d", m.ruleProviders.running(), len(m.ruleProviders.queue))
	}
	if duplicate := m.ruleProvidersActivate("rules.providers-all"); duplicate != nil || len(m.ruleProviders.queue) != 4 {
		t.Fatal("duplicate Update All created more work")
	}
	messages := providerMessages(command)
	if len(messages) != 3 || len(calls) != 3 {
		t.Fatal("the initial batch exceeded its three-worker budget")
	}
	for len(messages) > 0 {
		message := messages[0]
		messages = messages[1:]
		if result, ok := message.(ruleProviderUpdateResultMsg); ok {
			messages = append(messages, providerMessages(m.acceptRuleProviderUpdate(result))...)
		}
		if m.ruleProviders.running() > 3 {
			t.Fatal("a completion overfilled the update worker budget")
		}
	}
	if len(calls) != 7 || calls[backend.providers[7].Name] != 0 || m.ruleProviders.running() != 0 || len(m.ruleProviders.queue) != 0 {
		t.Fatalf("updates omitted a source or included Inline: %#v", calls)
	}
	if m.ruleProviders.updates[fail].state != "failed" || m.ruleProviders.updates[backend.providers[0].Name].state != "success" {
		t.Fatal("individual failure erased another provider's success")
	}
	for _, result := range providerMessages(m.ruleProvidersActivate("rules.provider-update." + fail)) {
		m.acceptRuleProviderUpdate(result.(ruleProviderUpdateResultMsg))
	}
	if calls[fail] != 2 || m.ruleProviders.updates[fail].state != "success" {
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
	results := providerMessages(m.ruleProvidersActivate("rules.providers-all"))
	if len(contexts) != 3 {
		t.Fatal("fixture did not start a full batch")
	}
	m.ruleProvidersActivate("rules.providers-back")
	for _, ctx := range contexts {
		if ctx.Err() == nil {
			t.Fatal("leaving provider view did not cancel a running update")
		}
	}
	for _, message := range results {
		if m.acceptRuleProviderUpdate(message.(ruleProviderUpdateResultMsg)) != nil {
			t.Fatal("late update scheduled work after leaving")
		}
	}
	if m.rules.view != "" || m.focus != "rules.providers" || m.ruleProviders.running() != 0 || len(m.ruleProviders.queue) != 0 || len(m.ruleProviders.updates) != 0 {
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
			read := m.ruleProvidersCmd()().(ruleProvidersResultMsg)
			updates := providerMessages(m.ruleProvidersActivate("rules.providers-all"))
			if len(contexts) != 4 || len(m.ruleProviders.queue) != 3 {
				t.Fatal("fixture did not begin one read, three updates and queued work")
			}
			trigger.stop(&m)
			for _, ctx := range contexts {
				if ctx.Err() == nil {
					t.Fatal("exit path retained a provider request context")
				}
			}
			if m.ruleProviders.active || m.ruleProviders.pending || m.ruleProviders.running() != 0 || len(m.ruleProviders.queue) != 0 {
				t.Fatal("exit path retained provider work")
			}
			if m.acceptRuleProviders(read) != nil {
				t.Fatal("late provider read restarted polling after cancellation")
			}
			for _, message := range updates {
				if m.acceptRuleProviderUpdate(message.(ruleProviderUpdateResultMsg)) != nil {
					t.Fatal("late provider update scheduled work after cancellation")
				}
			}
		})
	}
}

func TestRulesProvidersSearchPreservesNamesAndInlineIsReadOnly(t *testing.T) {
	m, backend := ruleProvidersModel(t, 4)
	m.ruleProviders.search.SetValue("inline")
	m.reconcileRuleProviders()
	list := m.filteredRuleProviders()
	if len(list) != 1 || list[0].Name != backend.providers[3].Name {
		t.Fatal("provider source type search lost its exact provider")
	}
	if m.ruleProvidersActivate("rules.provider-update."+list[0].Name) != nil || len(m.ruleProviders.updates) != 0 {
		t.Fatal("Inline provider exposed an update operation")
	}
	body := m.ruleProvidersBodyWindow(100, 0, -1)
	for _, target := range body.targets {
		if strings.HasPrefix(target.ID, "rules.provider-update.") {
			t.Fatal("Inline row included a mouse update target")
		}
	}
	m.ruleProviders.search.SetValue("provider-01")
	m.reconcileRuleProviders()
	if m.ruleProviders.selected != backend.providers[1].Name {
		t.Fatal("filtered provider selection did not reconcile by name")
	}
	m.ruleProviders.editing = true
	m.ruleProviders.search.Focus()
	m.ruleProvidersSearchKey(special(tea.KeyEnter))
	if m.ruleProviders.editing || m.ruleProviders.search.Focused() || m.focus != "rules.provider."+backend.providers[1].Name {
		t.Fatal("finishing search did not focus its matching provider")
	}
}

func TestRulesProvidersNarrowRowsKeepNamesWithoutInvisibleUpdateTargets(t *testing.T) {
	m, backend := ruleProvidersModel(t, 3)
	provider := backend.providers[0]
	label, _ := m.ruleProviderUpdateLabel(provider)
	for _, width := range []int{1, 5, 10, ansi.StringWidth(label) + 7} {
		body := m.ruleProvidersBodyWindow(width, 0, -1)
		for _, target := range body.targets {
			if strings.HasPrefix(target.ID, "rules.provider-update.") {
				t.Fatalf("width %d retained an action where its button cannot fit", width)
			}
		}
		for _, line := range body.lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflowed: %q", width, ansi.Strip(line))
			}
		}
	}
	m.ruleProvidersActivate("rules.provider." + provider.Name)
	if command := m.startRuleProviderUpdates([]string{provider.Name}); command == nil {
		t.Fatal("hiding narrow mouse actions also disabled the selected provider update")
	}
}

func TestRulesProvidersWindowMatchesCompleteBody(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {80, 24}, {60, 20}, {40, 16}, {40, 9}, {20, 7}, {5, 2}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m, backend := ruleProvidersModel(t, 35)
			m.width, m.height = size[0], size[1]
			m.ruleProviders.updates[backend.providers[1].Name] = ruleProviderUpdate{state: "failed", err: errors.New("fixture update failure")}
			for _, offset := range []int{0, 1, 2, 3, 9, 87, 10000} {
				m.scroll = offset
				o := m.navigationOptions()
				o.Scroll = offset
				g := dashboardGeometry(o)
				if o.BodyTopRows > 0 {
					o.BodyTop = m.ruleProvidersToolbar(g.width, o.BodyTopRows)
				}
				full := m.ruleProvidersBodyWindow(g.width, 0, -1)
				window := m.ruleProvidersBodyWindow(g.width, offset, g.height)
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
	m.ruleProvidersActivate("rules.providers-all")
	m.ruleProviders.pending, m.ruleProviders.request = true, 2
	m.acceptRuleProviders(ruleProvidersResultMsg{generation: m.ruleProviders.generation, request: 2, snapshot: app.RuleProviderSnapshot{State: "stopped"}})
	if len(m.filteredRuleProviders()) != 0 || len(m.ruleProviders.updates) != 0 || m.ruleProviders.running() != 0 || len(m.ruleProviders.queue) != 0 {
		t.Fatal("stopped core retained old provider data or queued mutations")
	}
	body := m.ruleProvidersBodyWindow(100, 0, -1)
	plain := ansi.Strip(strings.Join(body.lines, "\n"))
	if strings.Contains(plain, "provider-00") {
		t.Fatal("stopped core displayed stale provider names")
	}
}

func TestRulesProvidersPageOwnsSearchKeysAndExactUpdateActions(t *testing.T) {
	m, backend := ruleProvidersModel(t, 5)
	m.rules.view, m.focus = "", "rules.providers"
	m.ruleProviders.active, m.ruleProviders.received = false, false
	m.ruleProviders.snapshot = app.RuleProviderSnapshot{}
	m.layout()
	var updated []string
	backend.update = func(ctx context.Context, name string) error {
		updated = append(updated, name)
		return ctx.Err()
	}
	var command tea.Cmd
	m, command = update(m, special(tea.KeyEnter))
	for _, result := range providerMessages(command) {
		if _, ok := result.(ruleProvidersResultMsg); ok {
			m, _ = update(m, result)
		}
	}
	if m.rules.view != "providers" || !m.ruleProviders.received || !hasAction(m.targets, "rules.providers-search") {
		t.Fatal("header entry did not load and render its provider view")
	}
	m, _ = update(m, key("/"))
	for _, value := range []string{"q", "1", "2", "3"} {
		m, _ = update(m, key(value))
	}
	if m.quitting || m.page != "rules" || m.ruleProviders.search.Value() != "q123" {
		t.Fatal("provider search input leaked into global navigation or quit")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m, _ = update(m, tea.PasteMsg{Content: "inline"})
	m, _ = update(m, special(tea.KeyEnter))
	if m.ruleProviders.editing || m.ruleProviders.selected != backend.providers[4].Name {
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
	m, command = click(m, findTarget(t, m, "rules.provider-update."+file))
	for _, result := range providerMessages(command) {
		if _, ok := result.(ruleProviderUpdateResultMsg); ok {
			m, command = update(m, result)
		}
	}
	if !reflect.DeepEqual(updated, []string{file}) || m.ruleProviders.updates[file].state != "success" || m.rules.view != "providers" {
		t.Fatalf("mouse update reached the wrong provider: %#v", updated)
	}
	for _, result := range providerMessages(command) {
		if _, ok := result.(ruleProvidersResultMsg); ok {
			m, _ = update(m, result)
		}
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.rules.view != "" || m.ruleProviders.active || m.ruleProviders.running() != 0 {
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
			m.focusRuleProvidersSearch()
			navigate.goTo(&m)
			if m.ruleProviders.editing || m.ruleProviders.search.Focused() || m.focus != "nav.rules" {
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
	m.ruleProvidersActivate("rules.provider." + backend.providers[3].Name)
	m.focus = "rules.providers-search"
	m.layout()
	for range len(m.focusTargets()) {
		m.cycleFocus(1)
		if !strings.HasPrefix(m.focus, "rules.provider.") {
			continue
		}
		if name := strings.TrimPrefix(m.focus, "rules.provider."); m.ruleProviders.selected != name || m.filteredRuleProviders()[m.ruleProviders.selectedIndex].Name != name {
			t.Fatal("Tab focused one provider while row shortcuts still targeted another")
		}
		_, command := update(m, key("u"))
		providerMessages(command)
		if !reflect.DeepEqual(updated, []string{strings.TrimPrefix(m.focus, "rules.provider.")}) {
			t.Fatalf("update shortcut did not target the row reached by Tab: %#v", updated)
		}
		return
	}
	t.Fatal("Tab traversal never reached the provider list")
}
