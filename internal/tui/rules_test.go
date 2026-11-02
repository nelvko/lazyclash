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

func sampleRules() []mihomo.Rule {
	return []mihomo.Rule{{Index: 0, Type: "DomainSuffix", Payload: "one.example.invalid", Proxy: "PROXY", Size: -1}, {Index: 1, Type: "Domain", Payload: "needle.example.invalid", Proxy: "REJECT", Size: -1}, {Index: 2, Type: "Match", Proxy: "DIRECT", Size: -1}}
}
func rulesModel(t *testing.T, b *fakeBackend) Model {
	t.Helper()
	m := runtimeModel(t, b)
	m.page, m.focus = "rules", "rule.0"
	m.rules.active, m.rules.received = true, true
	m.rules.snapshot = app.RuleSnapshot{State: "ready", Rules: sampleRules(), ObservedAt: time.Now()}
	m.reconcileRules()
	m.layout()
	return m
}

func TestRulesPreserveOrderAndSearchInputOpensDetails(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	// API sequence is authoritative, even if a response uses sparse indexes.
	m.rules.snapshot.Rules = []mihomo.Rule{sampleRules()[2], sampleRules()[0], sampleRules()[1]}
	list := m.filteredRules()
	if len(list) != 3 || list[0].Index != 2 || list[1].Index != 0 || list[2].Index != 1 {
		t.Fatal("rules were reordered")
	}
	m, _ = update(m, key("/"))
	for _, value := range []string{"q", "1", "2", "3", "4", "5", "6"} {
		m, _ = update(m, key(value))
	}
	if m.page != "rules" || m.quitting || m.rules.search.Value() != "q123456" {
		t.Fatal("search characters triggered global commands")
	}
	m.rules.search.Reset()
	m, _ = update(m, tea.PasteMsg{Content: "NEEDLE"})
	list = m.filteredRules()
	if len(list) != 1 || list[0].Index != 1 {
		t.Fatal("case-insensitive payload search failed")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.rules.editing || m.focus != "rule.1" || m.rules.detail != nil {
		t.Fatal("finishing search did not focus its first result")
	}
	m, _ = update(m, special(tea.KeyEnter))
	plain := ansi.Strip(m.screen)
	if m.rules.detail == nil || m.rules.detail.Index != 1 || !strings.Contains(plain, "Rule details") || !strings.Contains(plain, "needle.example.invalid") || !strings.Contains(plain, "REJECT") {
		t.Fatal("details did not show the matched rule")
	}
	if strings.Contains(plain, "Entries:") || strings.Contains(plain, "Enable rule") || strings.Contains(plain, "Disable rule") {
		t.Fatal("inapplicable count or mutation action was displayed")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.rules.detail != nil || m.focus != "rule.1" || m.rules.search.Value() != "NEEDLE" {
		t.Fatal("return lost query or selection")
	}
}

func TestRulesRefreshDoesNotRetargetDetailsWhenIndexIsReused(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m, _ = update(m, special(tea.KeyEnter))
	before := *m.rules.detail
	fresh := sampleRules()
	fresh[0].Payload = "replacement.example.invalid"
	m.rules.pending = true
	m, _ = update(m, rulesResultMsg{generation: m.rules.generation, snapshot: app.RuleSnapshot{State: "ready", Rules: fresh}})
	if m.rules.detail == nil || ruleIdentity(*m.rules.detail) != ruleIdentity(before) || m.ruleCurrent(*m.rules.detail) || !strings.Contains(ansi.Strip(m.screen), "Last observed rule") {
		t.Fatal("index reuse silently replaced the displayed rule")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.rules.detail != nil || m.rules.selected != ruleIdentity(fresh[0]) {
		t.Fatal("return did not reconcile against the fresh list")
	}
}

func TestRulesOptionalObservationsAndZeroSizeRemainReadOnly(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	r := sampleRules()[0]
	r.Size, r.Extra = 0, &mihomo.RuleExtra{Disabled: true, HitCount: 4, MissCount: 9}
	m.rules.snapshot.Rules[0] = r
	m, _ = update(m, special(tea.KeyEnter))
	plain := ansi.Strip(m.screen)
	for _, expected := range []string{"Entries: 0", "State: Disabled", "Hits / misses: 4 / 9", "Read only"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("missing observation %q", expected)
		}
	}
	for _, target := range m.targets {
		if strings.HasPrefix(target.ID, "rules.") && target.ID != "rules.back" && target.ID != "rules.retry" {
			t.Fatalf("unexpected details action %s", target.ID)
		}
	}
}

func TestRulesQueriesRemainIndependentAndRejectLateMessages(t *testing.T) {
	calls := 0
	b := &fakeBackend{rules: func(context.Context) (app.RuleSnapshot, error) {
		calls++
		return app.RuleSnapshot{State: "ready", Rules: sampleRules()}, nil
	}}
	m := rulesModel(t, b)
	m.stopRules()
	first := m.syncRules()
	firstID := m.rules.generation
	if first == nil || m.busy || !m.rules.pending || m.syncRules() != nil {
		t.Fatal("rules query did not have an independent single-task lifecycle")
	}
	m, resized := update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if resized != nil || m.rules.generation != firstID {
		t.Fatal("resize repeated the request")
	}
	second := m.retryRules()
	secondID := m.rules.generation
	m, _ = update(m, first())
	if !m.rules.pending || m.rules.received || m.rules.generation != secondID {
		t.Fatal("late response replaced retry")
	}
	m, next := update(m, second())
	if calls != 2 || m.rules.pending || !m.rules.received || next == nil {
		t.Fatal("rules result did not schedule its refresh")
	}
	m, request := update(m, rulesTickMsg{generation: secondID})
	current := m.rules.generation
	if request == nil || !m.rules.pending || current == secondID {
		t.Fatal("refresh did not start a distinct task")
	}
	m, duplicate := update(m, rulesTickMsg{generation: secondID})
	if duplicate != nil || m.rules.generation != current {
		t.Fatal("duplicate tick created a second query")
	}
	m, _ = update(m, rulesResultMsg{generation: secondID, snapshot: app.RuleSnapshot{State: "stopped"}})
	if !m.rules.pending || m.rules.snapshot.State != "ready" {
		t.Fatal("older message replaced current query")
	}
}

func TestRulesRequestsCancelOnNavigationModalActionAndQuit(t *testing.T) {
	for _, action := range []string{"1", "i", "n", "a", "s", "q", "ctrl+c"} {
		t.Run(action, func(t *testing.T) {
			started := make(chan context.Context, 1)
			done := make(chan tea.Msg, 1)
			b := &fakeBackend{rules: func(ctx context.Context) (app.RuleSnapshot, error) {
				started <- ctx
				<-ctx.Done()
				return app.RuleSnapshot{}, ctx.Err()
			}}
			m := rulesModel(t, b)
			m.stopRules()
			request := m.syncRules()
			go func() { done <- request() }()
			ctx := <-started
			if action == "ctrl+c" {
				m, _ = update(m, ctrlC())
			} else {
				m, _ = update(m, key(action))
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("leaving the rules query did not cancel it")
			}
			cached, stoppedGeneration := m.rules.snapshot, m.rules.generation
			if !m.rules.received || cached.State != "ready" {
				t.Fatal("canceling the rules query discarded its previous observation")
			}
			late := <-done
			m, _ = update(m, late)
			if !m.rules.received || m.rules.generation != stoppedGeneration || !reflect.DeepEqual(m.rules.snapshot, cached) {
				t.Fatal("canceled result changed the cached rules observation")
			}
		})
	}
}

func TestRulesEmptyFailuresAndRecoveryDoNotShowOldRows(t *testing.T) {
	for _, state := range []string{"uninstalled", "unsupported", "stopped", "unavailable", "empty", "error"} {
		t.Run(state, func(t *testing.T) {
			m := rulesModel(t, &fakeBackend{})
			m.rules.pending = true
			result := rulesResultMsg{generation: m.rules.generation, snapshot: app.RuleSnapshot{State: state, Error: "diagnostic fixture"}}
			if state == "empty" {
				result.snapshot = app.RuleSnapshot{State: "ready", Rules: []mihomo.Rule{}}
			}
			if state == "error" {
				result.err = errors.New("query interrupted")
			}
			m, retry := update(m, result)
			if len(m.filteredRules()) != 0 || strings.Contains(ansi.Strip(m.screen), "one.example.invalid") || retry == nil {
				t.Fatal("unavailable/empty query displayed stale data or stopped retrying")
			}
			if state == "empty" && !strings.Contains(ansi.Strip(m.screen), "No effective rules") {
				t.Fatal("valid empty rules did not have a distinct status")
			}
			request := m.retryRules()
			if request == nil || !m.rules.pending {
				t.Fatal("manual retry unavailable")
			}
			m, _ = update(m, rulesResultMsg{generation: m.rules.generation, snapshot: app.RuleSnapshot{State: "ready", Rules: sampleRules()}})
			if !strings.Contains(ansi.Strip(m.screen), "Rules loaded") || len(m.filteredRules()) != 3 {
				t.Fatal("fresh rules did not recover from failure")
			}
		})
	}
}

func TestRulesKeyboardMouseAndResponsiveFocus(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {100, 28}, {100, 20}, {80, 24}, {60, 20}, {120, 18}, {20, 7}, {5, 2}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := rulesModel(t, &fakeBackend{})
			m.rules.snapshot.Rules = nil
			for i := 0; i < 50; i++ {
				m.rules.snapshot.Rules = append(m.rules.snapshot.Rules, mihomo.Rule{Index: i, Type: "DomainSuffix", Payload: strings.Repeat("中文长规则/", 30), Proxy: "PROXY", Size: -1})
			}
			m.reconcileRules()
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for i := 0; i < 30; i++ {
				m, _ = update(m, special(tea.KeyDown))
			}
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
			if size[0] >= 20 && size[1] >= 8 && !hasAction(m.targets, "rule.30") {
				t.Fatal("selected rule scrolled outside the viewport")
			}
			if size == [2]int{100, 20} {
				if !hasAction(m.targets, "nav.rules") || !strings.Contains(ansi.Strip(m.screen), "Rules") {
					t.Fatal("sidebar core summary covered Rules navigation")
				}
			}
			m, _ = update(m, special(tea.KeyEnter))
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
			m, _ = update(m, special(tea.KeyEscape))
			if m.rules.detail != nil {
				t.Fatal("tiny rules detail could not be exited")
			}
		})
	}
	m := rulesModel(t, &fakeBackend{})
	m, _ = click(m, findTarget(t, m, "rule.1"))
	if m.rules.detail == nil || m.rules.detail.Index != 1 {
		t.Fatal("mouse did not open its exact rule")
	}
	m, _ = click(m, findTarget(t, m, "rules.back"))
	if m.rules.detail != nil {
		t.Fatal("mouse Back did not return to list")
	}
}

func TestRulesNavigationMatchesMenuShortcuts(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	for _, item := range []struct{ key, page string }{{"1", "overview"}, {"2", "proxies"}, {"3", "profiles"}, {"4", "connections"}, {"5", "rules"}, {"6", "logs"}, {"7", "settings"}} {
		m, _ = update(m, key(item.key))
		if m.page != item.page {
			t.Fatalf("%s went to %s", item.key, m.page)
		}
	}
}
