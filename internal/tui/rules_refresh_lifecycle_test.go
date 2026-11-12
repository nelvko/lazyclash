package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nelvko/lazyclash/internal/app"
)

func TestRulesReadOnPageEntryAndSettleWithoutPolling(t *testing.T) {
	reads := 0
	b := &fakeBackend{rules: func(ctx context.Context) (app.RuleSnapshot, error) {
		reads++
		return app.RuleSnapshot{State: "ready", Rules: sampleRules()}, ctx.Err()
	}}
	m := runtimeModel(t, b)
	m.page = "overview"
	if m.syncRules() != nil || reads != 0 {
		t.Fatal("starting outside the Rules page prefetched the rule list")
	}
	m.page = "rules"
	request := m.syncRules()
	if request == nil || m.syncRules() != nil {
		t.Fatal("entering Rules did not start exactly one finite read")
	}
	if m.acceptRules(request().(rulesResultMsg)) != nil {
		t.Fatal("a completed rule observation scheduled background polling")
	}
	for range 20 {
		if m.syncRules() != nil {
			t.Fatal("remaining on Rules scheduled another read without a change")
		}
	}
	if reads != 1 {
		t.Fatalf("unchanged page read rules %d times", reads)
	}
	m.rules.search.SetValue("needle")
	m.reconcileRules()
	m.rules.expanded = m.rules.selected
	selected, expanded := m.rules.selected, m.rules.expanded
	m.page = "connections"
	if m.syncRules() != nil || !m.rules.received {
		t.Fatal("leaving Rules discarded its usable cache or started a read")
	}
	m.page = "rules"
	request = m.syncRules()
	if request == nil {
		t.Fatal("returning to Rules did not request its current contents")
	}
	m.acceptRules(request().(rulesResultMsg))
	if reads != 2 || m.rules.search.Value() != "needle" || m.rules.selected != selected || m.rules.expanded != expanded {
		t.Fatal("returning to Rules lost its query or current expansion")
	}
	m.stopRules()
}

func TestRulesRefreshWhenAppliedConfigurationOrProcessChanges(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*app.Snapshot)
	}{
		{"configuration applied", func(s *app.Snapshot) { s.Generation = "generation-2" }},
		{"applied profile switched", func(s *app.Snapshot) { s.Applied = "other" }},
		{"core restarted", func(s *app.Snapshot) { s.Service.PID++ }},
		{"process ID reused", func(s *app.Snapshot) { s.Service.StartTimestampMonotonic++ }},
	} {
		t.Run(change.name, func(t *testing.T) {
			reads := 0
			b := &fakeBackend{rules: func(ctx context.Context) (app.RuleSnapshot, error) {
				reads++
				return app.RuleSnapshot{State: "ready", Rules: sampleRules()}, ctx.Err()
			}}
			m := runtimeModel(t, b)
			m.page = "rules"
			m.snapshot.Generation, m.snapshot.Applied = "generation-1", "work"
			m.snapshot.Service.PID, m.snapshot.Service.StartTimestampMonotonic = 123, 1000
			m.acceptRules(m.syncRules()().(rulesResultMsg))
			change.mutate(&m.snapshot)
			request := m.syncRules()
			if request == nil || m.syncRules() != nil {
				t.Fatal("changed source did not start exactly one replacement read")
			}
			m.acceptRules(request().(rulesResultMsg))
			if reads != 2 || m.syncRules() != nil {
				t.Fatalf("source transition did not settle after one read: reads=%d", reads)
			}
			m.stopRules()
		})
	}
}

func TestRulesMetadataChangesDoNotRefreshTheRunningRuleList(t *testing.T) {
	b := &fakeBackend{rules: func(ctx context.Context) (app.RuleSnapshot, error) {
		return app.RuleSnapshot{State: "ready", Rules: sampleRules()}, ctx.Err()
	}}
	m := runtimeModel(t, b)
	m.page = "rules"
	m.snapshot.Generation, m.snapshot.Applied = "generation-1", "work"
	m.snapshot.Service.PID, m.snapshot.Service.StartTimestampMonotonic = 123, 1000
	m.snapshot.Service.StartedAt = time.Now().Add(-time.Minute)
	m.acceptRules(m.syncRules()().(rulesResultMsg))
	// Downloading a newer subscription changes its draft metadata and pending
	// flag, while the published generation and running process stay the same.
	m.snapshot.ProfilePending = true
	m.snapshot.AppliedName = "renamed profile"
	m.snapshot.ProfileDetails = []app.ProfileInfo{{ID: "work", Name: "renamed profile"}}
	m.snapshot.Service.StartedAt = m.snapshot.Service.StartedAt.Add(time.Millisecond)
	m.snapshot.ServiceObservedAt = time.Now()
	if m.syncRules() != nil {
		t.Fatal("draft/profile metadata or a reconstructed uptime anchor reloaded running rules")
	}
	m.stopRules()
}

func TestRulesSourceChangeCancelsInflightReadAndRejectsItsResult(t *testing.T) {
	var requests []context.Context
	b := &fakeBackend{rules: func(ctx context.Context) (app.RuleSnapshot, error) {
		requests = append(requests, ctx)
		return app.RuleSnapshot{State: "ready", Rules: sampleRules()}, nil
	}}
	m := runtimeModel(t, b)
	m.page = "rules"
	m.snapshot.Generation = "generation-1"
	oldRequest := m.syncRules()
	old := oldRequest().(rulesResultMsg)
	m.snapshot.Generation = "generation-2"
	m.acceptRules(old)
	if m.rules.received || !m.rules.pending {
		t.Fatal("a stale result was accepted before the new source read was scheduled")
	}
	currentRequest := m.syncRules()
	if currentRequest == nil || requests[0].Err() == nil {
		t.Fatal("a new applied configuration did not cancel the old rule request")
	}
	m.acceptRules(old)
	if m.rules.received || !m.rules.pending {
		t.Fatal("a result from the previous applied configuration was published")
	}
	m.acceptRules(currentRequest().(rulesResultMsg))
	if !m.rules.received || m.rules.pending || len(m.filteredRules()) != len(sampleRules()) {
		t.Fatal("the current configuration could not publish its rule observation")
	}
	m.stopRules()
}

func TestRulesManualReloadKeepsTheCurrentListWhileReading(t *testing.T) {
	for _, trigger := range []tea.KeyPressMsg{key("g"), {Code: 'r', Mod: tea.ModCtrl}} {
		t.Run(trigger.String(), func(t *testing.T) {
			m := rulesMouseModel(t, 60, 160, 45)
			m.syncRuntime()
			b := m.backend.(*fakeBackend)
			fresh := m.rules.snapshot
			b.rules = func(ctx context.Context) (app.RuleSnapshot, error) { return fresh, ctx.Err() }
			m.rules.search.SetValue("中文")
			m.rules.selected = ruleIdentity(fresh.Rules[40])
			m.rules.expanded, m.focus = m.rules.selected, "rule.40"
			m.revealRule()
			m.layout()
			scroll, selected, expanded := m.scroll, m.rules.selected, m.rules.expanded
			m, request := update(m, trigger)
			if request == nil || !m.rules.pending || !m.rules.received || len(m.filteredRules()) != 60 || m.scroll != scroll {
				t.Fatal("manual reload hid or moved its usable rule observation")
			}
			m, _ = update(m, request())
			if m.rules.pending || m.rules.search.Value() != "中文" || m.rules.selected != selected || m.rules.expanded != expanded || m.scroll != scroll {
				t.Fatal("manual reload changed the query, selected rule, expansion or scroll")
			}
			m.stopRules()
		})
	}
}

func TestRulesProviderCompletionAfterPanelCloseRefreshesOnlyItsCurrentCore(t *testing.T) {
	for _, current := range []bool{false, true} {
		t.Run(map[bool]string{false: "old core", true: "current core"}[current], func(t *testing.T) {
			reads := 0
			b := &fakeBackend{rules: func(ctx context.Context) (app.RuleSnapshot, error) {
				reads++
				return app.RuleSnapshot{State: "ready", Rules: sampleRules()}, ctx.Err()
			}}
			m := rulesModel(t, b)
			m.stopRules()
			m.acceptRules(m.syncRules()().(rulesResultMsg))
			// An update may have reached Mihomo before closing the panel canceled
			// its client request. Its row completion is stale, but this core's
			// effective rule observation must still be invalidated.
			source := m.providerSource()
			if !current {
				source = "another applied configuration or process"
			}
			m.providers.open = false
			m.acceptProviderUpdate(providerUpdateResultMsg{kind: providerRules, source: source, generation: 99, name: "closed-panel-source", update: 1})
			request := m.syncRules()
			if !current {
				if request != nil || !m.rules.active || reads != 1 {
					t.Fatal("an old core's provider completion invalidated current effective rules")
				}
				return
			}
			if request == nil || m.syncRules() != nil {
				t.Fatal("provider completion after closing its panel did not start exactly one rule read")
			}
			m.acceptRules(request().(rulesResultMsg))
			if reads != 2 || m.syncRules() != nil {
				t.Fatal("provider-triggered rule refresh did not settle after completion")
			}
			m.stopRules()
		})
	}
}

func TestRulesRuntimeAvailabilityTransitionsInvalidateOnce(t *testing.T) {
	reads := 0
	observed := app.RuleSnapshot{State: "ready", Rules: sampleRules()}
	b := &fakeBackend{rules: func(ctx context.Context) (app.RuleSnapshot, error) {
		reads++
		return observed, ctx.Err()
	}}
	m := runtimeModel(t, b)
	m.page = "rules"
	m.acceptRules(m.syncRules()().(rulesResultMsg))
	m.snapshot.ServiceError = "fixture owner inspection failed"
	observed = app.RuleSnapshot{State: "unavailable", Error: "fixture owner inspection failed"}
	request := m.syncRules()
	if request == nil || m.rules.received || len(m.filteredRules()) != 0 {
		t.Fatal("lost process observation kept a ready rule list")
	}
	m.acceptRules(request().(rulesResultMsg))
	m.snapshot.ServiceError = "fixture owner inspection still failing with another message"
	m.snapshot.APIError = "an independent proxy query failed"
	if m.syncRules() != nil || reads != 2 || m.rules.snapshot.State != "unavailable" {
		t.Fatal("diagnostic wording or an independent API failure retriggered rule reads")
	}
	m.snapshot.ServiceError = ""
	observed = app.RuleSnapshot{State: "ready", Rules: sampleRules()}
	request = m.syncRules()
	if request == nil {
		t.Fatal("restored process observation did not recover its rules")
	}
	m.acceptRules(request().(rulesResultMsg))
	m.snapshot.APIError = "another independent proxy query failure"
	if reads != 3 || m.syncRules() != nil || len(m.filteredRules()) != len(sampleRules()) {
		t.Fatal("recovered rules failed to settle independently of proxy API observations")
	}
	m.stopRules()
}
