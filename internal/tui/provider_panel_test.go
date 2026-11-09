package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type panelProvidersBackend struct {
	*fakeBackend
	proxies       []mihomo.ProxyProvider
	rules         []mihomo.RuleProvider
	updates       map[string]int
	snapshotReads int
	update        func(context.Context, string) error
}

func (b *panelProvidersBackend) ProxyProviders(ctx context.Context) (app.ProxyProviderSnapshot, error) {
	return app.ProxyProviderSnapshot{State: "ready", Providers: b.proxies, ObservedAt: time.Now()}, ctx.Err()
}
func (b *panelProvidersBackend) UpdateProxyProvider(ctx context.Context, name string) error {
	b.updates[name]++
	if b.update != nil {
		return b.update(ctx, name)
	}
	return ctx.Err()
}
func (b *panelProvidersBackend) RuleProviders(ctx context.Context) (app.RuleProviderSnapshot, error) {
	return app.RuleProviderSnapshot{State: "ready", Providers: b.rules, ObservedAt: time.Now()}, ctx.Err()
}
func (b *panelProvidersBackend) UpdateRuleProvider(ctx context.Context, name string) error {
	b.updates["rule:"+name]++
	return ctx.Err()
}
func (b *panelProvidersBackend) Snapshot(ctx context.Context) (app.Snapshot, error) {
	b.snapshotReads++
	return b.fakeBackend.Snapshot(ctx)
}

func proxyProviderPanelModel(t *testing.T) (Model, *panelProvidersBackend) {
	t.Helper()
	m, original := proxiesModel(t, 160, 45)
	b := &panelProvidersBackend{fakeBackend: original.fakeBackend, updates: make(map[string]int),
		rules: []mihomo.RuleProvider{{Name: "rule-source", Type: "Rule", VehicleType: "HTTP", Behavior: "Domain", Format: "yaml", RuleCount: 99}}}
	for index, vehicle := range []string{"HTTP", "HTTP", "HTTP", "HTTP", "File", "Inline", "Compatible", "Future"} {
		b.proxies = append(b.proxies, mihomo.ProxyProvider{Name: fmt.Sprintf("proxy-%d-中文", index), Type: "Proxy", VehicleType: vehicle, ProxyCount: index + 1})
	}
	m.backend = b
	request := m.openProviders(providerProxies)
	if request == nil {
		t.Fatal("opening proxy providers did not request metadata")
	}
	m.acceptProviders(request().(providersResultMsg))
	m.layout()
	t.Cleanup(m.stopProviders)
	t.Cleanup(m.stopRuntime)
	return m, b
}

func TestProviderPanelUsesTheSameViewForRuleAndProxyMetadata(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, kind := range []providerKind{providerRules, providerProxies} {
			for _, size := range [][2]int{{160, 45}, {80, 24}, {40, 16}, {20, 7}} {
				t.Run(fmt.Sprintf("%s/%s/%dx%d", language, kind, size[0], size[1]), func(t *testing.T) {
					m, _ := proxyProviderPanelModel(t)
					m.locale = i18n.New(language)
					if kind == providerRules {
						m.page = "rules"
						request := m.openProviders(providerRules)
						m.acceptProviders(request().(providersResultMsg))
					}
					m.width, m.height = size[0], size[1]
					m.layout()
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
					findTarget(t, m, "providers.back")
					if size[0] < 24 {
						return
					}
					if !strings.Contains(ansi.Strip(m.screen), m.providerTitle()) {
						t.Fatal("shared panel lost its kind-specific title")
					}
					row := m.providers.snapshot.Providers[0]
					text := ansi.Strip(strings.Join(m.providerRows(row, 100), "\n"))
					if !strings.Contains(text, m.tr("providers.entries."+string(kind), row.Count)) {
						t.Fatalf("shared row lost kind-specific count: %s", text)
					}
					if kind == providerProxies && (row.Behavior != "" || row.Format != "" || !strings.Contains(text, m.tr("providers.unknown_time"))) {
						t.Fatalf("proxy metadata gained invented rule fields or time: %s", text)
					}
				})
			}
		}
	}
}

func TestProxyProviderPanelUpdatesOnlyExternalSourcesAndSynchronizesMainList(t *testing.T) {
	m, b := proxyProviderPanelModel(t)
	failed := b.proxies[1].Name
	b.update = func(ctx context.Context, name string) error {
		if name == failed {
			return errors.New("fixture download failed")
		}
		b.snapshot.Groups = []mihomo.Group{{Name: "UPDATED", Type: "Selector", Now: "fresh-node", All: []string{"fresh-node"}}}
		b.snapshot.Proxies = map[string]mihomo.Proxy{"fresh-node": {Name: "fresh-node", Type: "Shadowsocks"}}
		for index := range b.proxies {
			if b.proxies[index].Name == name {
				b.proxies[index].ProxyCount++
			}
		}
		return ctx.Err()
	}
	commands := m.providersActivate("providers.all")
	if m.providers.running() != 3 || len(m.providers.queue) != 2 {
		t.Fatalf("proxy updates did not share the three-slot queue: running=%d queued=%d", m.providers.running(), len(m.providers.queue))
	}
	if duplicate := m.providersActivate("providers.all"); duplicate != nil {
		t.Fatal("duplicate update-all enqueued more work")
	}
	messages := providerMessages(commands)
	for len(messages) > 0 {
		message := messages[0]
		messages = messages[1:]
		switch result := message.(type) {
		case providerUpdateResultMsg:
			messages = append(messages, providerMessages(m.acceptProviderUpdate(result))...)
		case providersResultMsg:
			m.acceptProviders(result)
		case coreStatusMsg:
			m, _ = update(m, result)
		}
		if m.providers.running() > 3 {
			t.Fatal("completion exceeded the shared worker limit")
		}
	}
	if len(b.updates) != 5 || b.snapshotReads != 5 || m.providers.updates[failed].state != "failed" {
		t.Fatalf("wrong provider mutations or missing main-list synchronization: updates=%v reads=%d", b.updates, b.snapshotReads)
	}
	for _, provider := range b.proxies[5:] {
		if b.updates[provider.Name] != 0 || hasAction(m.targets, "provider.update."+provider.Name) {
			t.Fatalf("non-external provider remained updateable: %+v", provider)
		}
	}
	if len(m.snapshot.Groups) != 1 || m.snapshot.Groups[0].Name != "UPDATED" || m.snapshot.Proxies["fresh-node"].Name != "fresh-node" {
		t.Fatal("provider updates did not automatically replace the underlying proxy list")
	}
}

func TestProviderPanelKindAndCoreChangesRejectOldWork(t *testing.T) {
	for _, change := range []string{"kind", "profile", "generation", "pid"} {
		t.Run(change, func(t *testing.T) {
			m, b := proxyProviderPanelModel(t)
			read := m.retryProviders()().(providersResultMsg)
			updates := providerMessages(m.startProviderUpdates([]string{b.proxies[0].Name}))
			if len(updates) != 1 {
				t.Fatal("fixture did not start one provider update")
			}
			switch change {
			case "kind":
				m.closeProviders()
				m.page = "rules"
				m.openProviders(providerRules)
			case "profile":
				m.snapshot.Applied = "another-profile"
			case "generation":
				m.snapshot.Generation = "another-generation"
			case "pid":
				m.snapshot.Service.PID++
			}
			if m.acceptProviders(read) != nil {
				t.Fatal("old provider rows survived a kind or core transition")
			}
			observation := m.acceptProviderUpdate(updates[0].(providerUpdateResultMsg))
			if change != "kind" && observation != nil {
				t.Fatal("a completion from an old core scheduled an observation")
			}
			if change == "kind" && (m.providers.kind != providerRules || len(m.providers.updates) != 0 || m.providers.received) {
				t.Fatal("old proxy completion polluted the rule panel")
			}
			m.stopCoreStatus()
			before := len(b.updates)
			if m.startProviderUpdates([]string{b.proxies[1].Name}) != nil || len(b.updates) != before {
				t.Fatal("an old displayed row became a mutation target in the new context")
			}
			if change != "kind" {
				if current := m.syncProviders(); current == nil || m.providers.received || m.providers.source != m.providerSource() {
					t.Fatal("new core context did not cancel and replace the old observation")
				}
			}
		})
	}
}

func TestProxyProviderPanelOwnsInputAndRestoresUnderlyingPage(t *testing.T) {
	m, _ := proxyProviderPanelModel(t)
	focus, page, scroll := m.focus, m.page, m.scroll
	m.providers.editing = false
	for _, input := range []tea.KeyPressMsg{key("1"), key("2"), key("r"), key("?"), special(tea.KeyLeft)} {
		m, _ = update(m, input)
	}
	if m.page != page || m.scroll != scroll || m.focus != focus || !m.providers.open || m.helpOpen || m.busy {
		t.Fatal("provider modal leaked navigation or service controls to its page")
	}
	m.focusProvidersSearch()
	m, _ = update(m, tea.PasteMsg{Content: "Compatible"})
	if list := m.filteredProviders(); len(list) != 1 || list[0].VehicleType != "Compatible" {
		t.Fatal("shared provider search failed to match proxy metadata")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if !m.providers.open || m.providers.editing {
		t.Fatal("Escape while editing did not remain in the provider panel")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.providers.open || m.focus != "proxies.providers" || m.page != page || m.scroll != scroll {
		t.Fatal("closing proxy providers did not restore its page opener")
	}
}

func TestProxyProviderCompletionObservesCoreAfterPanelCloses(t *testing.T) {
	for _, outcome := range []string{"success", "cancelled after write", "error after write"} {
		t.Run(outcome, func(t *testing.T) {
			m, b := proxyProviderPanelModel(t)
			b.update = func(ctx context.Context, _ string) error {
				// A network failure or cancellation does not prove PUT was rolled
				// back. Model an accepted mutation with an uncertain response.
				b.snapshot.Groups = []mihomo.Group{{Name: "AFTER-CLOSE", Type: "Selector", All: []string{"DIRECT"}, Now: "DIRECT"}}
				switch outcome {
				case "cancelled after write":
					return context.Canceled
				case "error after write":
					return errors.New("connection closed after update")
				}
				return nil
			}
			messages := providerMessages(m.startProviderUpdates([]string{b.proxies[0].Name}))
			m.closeProviders()
			command := m.acceptProviderUpdate(messages[0].(providerUpdateResultMsg))
			if command == nil || m.providers.open || len(m.providers.updates) != 0 {
				t.Fatal("closed panel dropped the core observation or restored its rows")
			}
			m, _ = update(m, command())
			if b.snapshotReads != 1 || len(m.snapshot.Groups) != 1 || m.snapshot.Groups[0].Name != "AFTER-CLOSE" {
				t.Fatal("accepted provider mutation was not observed after closing")
			}
			m.stopCoreStatus()
		})
	}
}

func TestProxyProviderPartialBatchObservesSuccessBeforeDeparture(t *testing.T) {
	m, b := proxyProviderPanelModel(t)
	b.update = func(context.Context, string) error {
		b.snapshot.Groups = []mihomo.Group{{Name: "PARTIAL", Type: "Selector", All: []string{"DIRECT"}, Now: "DIRECT"}}
		return nil
	}
	messages := providerMessages(m.startProviderUpdates([]string{b.proxies[0].Name, b.proxies[1].Name}))
	if len(messages) != 2 {
		t.Fatal("fixture did not start two independent sources")
	}
	observations := providerMessages(m.acceptProviderUpdate(messages[0].(providerUpdateResultMsg)))
	if m.providers.running() != 1 || b.snapshotReads != 1 {
		t.Fatal("first success waited for the entire batch before observing the core")
	}
	m.closeProviders()
	for _, message := range observations {
		if result, ok := message.(coreStatusMsg); ok {
			m, _ = update(m, result)
		}
	}
	if len(m.snapshot.Groups) != 1 || m.snapshot.Groups[0].Name != "PARTIAL" || m.providers.open {
		t.Fatal("departing during a partial batch lost the successful provider's changes")
	}
	m.stopCoreStatus()
}
