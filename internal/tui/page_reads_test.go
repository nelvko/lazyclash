package tui

import (
	"context"
	"fmt"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestPageRuntimeDepartureKeepsCacheAndRejectsCanceledResult(t *testing.T) {
	m := runtimeModel(t, &fakeBackend{})
	m.runtime.snapshot, m.runtime.received = liveRuntime(), true
	m.runtime.active, m.runtime.pending, m.runtime.generation = true, true, 10
	ctx, cancel := context.WithCancel(t.Context())
	m.runtime.cancel = cancel
	m.connections.search.SetValue("keep query")
	m.stopRuntime()
	if ctx.Err() == nil || m.runtime.active || m.runtime.pending || !m.runtime.received || m.runtime.snapshot.State != "ready" {
		t.Fatal("leaving runtime did not cancel its request while retaining the observation")
	}
	m.acceptRuntime(runtimeResultMsg{generation: 10, snapshot: app.RuntimeSnapshot{State: "stopped"}})
	if m.runtime.snapshot.State != "ready" || m.connections.search.Value() != "keep query" {
		t.Fatal("late canceled runtime result replaced cached state")
	}
	m.page, m.width = "connections", 80
	request := m.syncRuntime()
	if request == nil || !m.runtime.pending || !m.runtime.received || m.runtime.snapshot.State != "ready" {
		t.Fatal("returning runtime did not refresh independently of its usable cache")
	}
	m.stopRuntime()
}

func TestPageRulesDepartureKeepsCacheAndRejectsCanceledResult(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m.rules.pending, m.rules.generation = true, 10
	ctx, cancel := context.WithCancel(t.Context())
	m.rules.cancel = cancel
	m.rules.search.SetValue("keep query")
	m.stopRules()
	if ctx.Err() == nil || m.rules.active || m.rules.pending || !m.rules.received || m.rules.snapshot.State != "ready" {
		t.Fatal("leaving rules did not cancel its request while retaining the observation")
	}
	m.acceptRules(rulesResultMsg{generation: 10, snapshot: app.RuleSnapshot{State: "stopped"}})
	if m.rules.snapshot.State != "ready" || m.rules.search.Value() != "keep query" {
		t.Fatal("late canceled rules result replaced cached state")
	}
	request := m.syncRules()
	if request == nil || !m.rules.pending || !m.rules.received {
		t.Fatal("returning rules hid the cache while refreshing")
	}
	m.retryRules()
	if m.rules.received {
		t.Fatal("explicit retry did not reset its unavailable observation")
	}
	m.stopRules()
}

func TestPageConnectionRefreshKeepsStableFocusAndScroll(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m.connections.selected, m.focus, m.scroll = "second", "connection.second", 20
	m.connections.selectedIndex = 1
	m.reconcileConnections()
	if m.focus != "connection.second" || m.scroll != 20 {
		t.Fatal("unchanged connection refresh moved the viewport")
	}
	m.scroll = 0
	m.runtime.snapshot.Connections.Connections = append(m.runtime.snapshot.Connections.Connections, fixtureConnection("newest", "new.example.invalid", 2))
	m.reconcileConnections()
	if m.focus != "connection.second" || m.scroll != 0 || m.connections.selectedIndex != 2 {
		t.Fatal("connection insertion stole selection or scrolled content")
	}
	m.runtime.snapshot.Connections.Connections = m.runtime.snapshot.Connections.Connections[:1]
	m.reconcileConnections()
	if m.connections.selected != "first" || m.focus != "connection.first" {
		t.Fatal("removed connection did not choose the closest surviving position")
	}
	m.page, m.focus, m.scroll = "settings", "connection.second", 30
	m.reconcileConnections()
	if m.focus != "connection.second" || m.scroll != 30 {
		t.Fatal("off-page connection refresh changed another page's focus or scroll")
	}
}

func TestPageRuleRefreshKeepsStableFocusAndScroll(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m.rules.selected, m.focus, m.scroll = ruleIdentity(sampleRules()[2]), "rule.2", 20
	m.rules.selectedIndex = 2
	m.reconcileRules()
	if m.focus != "rule.2" || m.scroll != 20 || m.rules.selectedIndex != 2 {
		t.Fatal("unchanged rule refresh moved the viewport")
	}
	m.rules.snapshot.Rules = m.rules.snapshot.Rules[:2]
	m.reconcileRules()
	if m.rules.selected != ruleIdentity(sampleRules()[1]) || m.focus != "rule.1" {
		t.Fatal("removed rule did not choose the closest surviving position")
	}
	m.page, m.focus, m.scroll = "settings", "rule.2", 30
	m.reconcileRules()
	if m.focus != "rule.2" || m.scroll != 30 {
		t.Fatal("off-page rule refresh changed another page's focus or scroll")
	}
}

func TestPageRuleReorderingKeepsKeyboardFocusVisible(t *testing.T) {
	m := rulesModel(t, &fakeBackend{})
	m.width, m.height = 80, 24
	var fresh []mihomo.Rule
	for index := 0; index < 35; index++ {
		fresh = append(fresh, mihomo.Rule{Index: 100 + index, Type: "Domain", Payload: fmt.Sprintf("new-%d.example.invalid", index), Proxy: "DIRECT"})
	}
	fresh = append(fresh, sampleRules()[0])
	m.rules.pending = true
	m.acceptRules(rulesResultMsg{generation: m.rules.generation, snapshot: app.RuleSnapshot{State: "ready", Rules: fresh}})
	m.layout()
	if m.focus != "rule.0" || !hasAction(m.targets, "rule.0") {
		t.Fatal("inserting rules before the selected item hid keyboard focus")
	}
	scroll := m.scroll
	m.rules.pending = true
	m.acceptRules(rulesResultMsg{generation: m.rules.generation, snapshot: app.RuleSnapshot{State: "ready", Rules: fresh}})
	if m.scroll != scroll {
		t.Fatal("unchanged rule refresh moved an already visible focus")
	}
}

func TestPageDetailsReturnToRememberedListScroll(t *testing.T) {
	t.Run("connections", func(t *testing.T) {
		m := connectionsModel(t, &fakeBackend{})
		// Use a scrollable list whose selected row is actually visible at the
		// remembered offset; the two-row fixture cannot have a scroll of eight.
		m.runtime.snapshot.Connections.Connections = nil
		for index := range 30 {
			m.runtime.snapshot.Connections.Connections = append(m.runtime.snapshot.Connections.Connections, fixtureConnection(fmt.Sprintf("connection-%02d", index), "example.invalid", 30-index))
		}
		m.scroll = 8
		m.connectionActivate("connection.connection-08")
		if m.connections.detail == nil || m.scroll != 0 {
			t.Fatal("connection details did not open")
		}
		m.connectionActivate("conn.back")
		if m.scroll != 8 || m.focus != "connection.connection-08" || m.connections.detail != nil {
			t.Fatal("returning from connection details lost the list position")
		}
	})
	t.Run("rules", func(t *testing.T) {
		m := rulesModel(t, &fakeBackend{})
		for index := 3; index < 35; index++ {
			m.rules.snapshot.Rules = append(m.rules.snapshot.Rules, mihomo.Rule{Index: index, Type: "Domain", Payload: "example.invalid", Proxy: "DIRECT", Size: -1})
		}
		m.selectRule("rule.8")
		m.scroll = 8
		m.ruleActivate("rule.8")
		if m.rules.expanded != ruleIdentity(m.rules.snapshot.Rules[8]) || m.focus != "rule.8" || m.scroll != 8 {
			t.Fatal("inline expansion lost the list position")
		}
		m.ruleActivate("rule.8")
		if m.scroll != 8 || m.focus != "rule.8" || m.rules.expanded != "" {
			t.Fatal("collapsing an inline rule lost the list position")
		}
	})
}

func TestPageEmptyListsRecoverToSafeContentFocus(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	m.runtime.snapshot.Connections.Connections = nil
	m.reconcileConnections()
	if m.focus != "content" {
		t.Fatal("empty connections focused a search or mutation control")
	}
	r := rulesModel(t, &fakeBackend{})
	r.rules.snapshot.Rules = nil
	r.reconcileRules()
	if r.focus != "content" {
		t.Fatal("empty rules focused a search or mutation control")
	}
}

func TestPageLogsDepartureKeepsPauseQueryAndHistory(t *testing.T) {
	m := logsModel()
	m.logs.entries = []mihomo.LogEntry{{Level: "info", Message: "retained"}}
	m.logs.search.SetValue("retained")
	m.logs.paused, m.logs.anchor.row = true, 12
	m.logs.active, m.logs.pending, m.logs.generation = true, true, 7
	ctx, cancel := context.WithCancel(t.Context())
	m.logs.cancel = cancel
	m.page = "settings"
	if m.syncLogs() != nil || ctx.Err() == nil {
		t.Fatal("leaving logs did not cancel the request")
	}
	m.acceptLogs(logsResultMsg{generation: 7, entries: []mihomo.LogEntry{{Message: "late"}}})
	m.page = "logs"
	if m.syncLogs() != nil || !m.logs.paused || m.logs.anchor.row != 12 || m.logs.search.Value() != "retained" || len(m.logs.entries) != 1 {
		t.Fatal("returning logs resumed a paused capture or lost retained state")
	}
}
