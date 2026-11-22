package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func proxyProviderHintHasKey(hint, key string) bool {
	for _, item := range strings.Split(hint, " · ") {
		if strings.HasPrefix(item, key+" ") {
			return true
		}
	}
	return false
}

func TestProxyHintsTrackSelectedNodesAndRunningGroup(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m, b := proxiesModel(t, 160, 40)
		m.locale = i18n.New(language)
		t.Cleanup(m.stopProxyTests)
		m.focus = "node.0.0"
		if strings.Contains(m.proxiesContextHelp(), "Enter") {
			t.Fatal("already selected node advertised an activation")
		}
		if cmd := m.proxyActivate(m.focus); cmd != nil || b.selections != 0 {
			t.Fatal("selected node should have no selection command")
		}
		m.focus = "node.0.1"
		if !strings.Contains(m.proxiesContextHelp(), m.tr("navigation.proxies.action.select")) {
			t.Fatal("unselected selector member lost its selection hint")
		}
		m.focus = "proxies.test.0"
		if m.testProxyGroup() == nil || !m.proxies.testing {
			t.Fatal("group test did not start")
		}
		if !strings.Contains(m.proxiesContextHelp(), m.tr("navigation.proxies.action.cancelTest")) {
			t.Fatal("same group did not advertise cancellation")
		}
		m.focus = "proxies.test.1"
		if hint := m.proxiesContextHelp(); !strings.Contains(hint, m.tr("navigation.proxies.action.testGroup")) || strings.Contains(hint, m.tr("navigation.proxies.action.cancelTest")) {
			t.Fatalf("different group advertised cancellation instead of testing: %s", hint)
		}
		if m.testProxyGroup() == nil || m.proxies.testGroup != "AUTO" || !m.proxies.testing {
			t.Fatal("different group's test did not replace the running group")
		}
		m.focus = "node.1.0"
		if hint := m.proxiesContextHelp(); !strings.Contains(hint, m.tr("navigation.proxies.cancelTestKey")) ||
			proxyProviderHintHasKey(hint, "Enter") || proxyProviderHintHasKey(hint, "i") {
			t.Fatalf("automatic node advertised an unavailable action or lost cancellation: %s", hint)
		}
		m.focus = "proxies.delay.1.0"
		if strings.Contains(m.proxiesContextHelp(), "Enter") || m.proxyActivate(m.focus) != nil {
			t.Fatal("in-flight node advertised or started a duplicate test")
		}
	}
}

func TestProxyHintsOmitUnavailableListActions(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.proxies.history = []proxyLocation{{group: "AUTO"}}
	m.focus = "proxies.reference.0.0"
	if strings.Contains(m.proxiesContextHelp(), "Enter") {
		t.Fatal("cyclic group reference advertised an activation that is rejected")
	}
	m.proxies.history = nil
	m.focus = "group.7" // The empty group has nothing to test.
	if proxyProviderHintHasKey(m.proxiesContextHelp(), "t") {
		t.Fatal("empty group advertised a test")
	}
	m.proxies.search.SetValue("no-such-node")
	m.focus = "content"
	if hint := m.proxiesContextHelp(); strings.Contains(hint, "Enter") || strings.Contains(hint, "↑↓") || proxyProviderHintHasKey(hint, "t") || proxyProviderHintHasKey(hint, "s") {
		t.Fatalf("empty search advertised list actions: %s", hint)
	}
	m.snapshot.Mode.Actual = "direct"
	if hint := m.proxiesContextHelp(); strings.Contains(hint, "Enter") || strings.Contains(hint, "/ ") {
		t.Fatalf("Direct content advertised an unavailable action: %s", hint)
	}
}

func TestProviderHintsFollowSearchFocusAndUpdateEligibility(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, kind := range []providerKind{providerProxies, providerRules} {
			m, _ := proxyProviderPanelModel(t)
			m.locale = i18n.New(language)
			if kind == providerRules {
				m.page = "rules"
				request := m.openProviders(kind)
				m.acceptProviders(request().(providersResultMsg))
			}
			row := m.providers.snapshot.Providers[0]
			m.providers.selected, m.focus = row.Name, "provider.row."+row.Name
			if hint := m.providersContextHelp(); !strings.Contains(hint, "u ") || strings.Contains(hint, "Enter") {
				t.Fatalf("provider row advertised the wrong activation: %s", hint)
			}
			m.focus = "provider.update." + row.Name
			if !strings.Contains(m.providersContextHelp(), m.tr("navigation.providers.activate", m.tr("providers.update"))) {
				t.Fatal("update button lost its Enter action")
			}
			_, cmd := m.providersKey(key("u"))
			if cmd == nil || m.providers.updates[row.Name].state != "updating" {
				t.Fatal("advertised provider update did not begin")
			}
			if hint := m.providersContextHelp(); strings.Contains(hint, "u ") || strings.Contains(hint, "Enter") {
				t.Fatalf("running provider advertised a duplicate update: %s", hint)
			}
			m.stopProviders()
			m.providers.active, m.providers.open = true, true
			m.providers.snapshot.Providers[0].updateable = false
			m.focus = "provider.row." + row.Name
			if strings.Contains(m.providersContextHelp(), "u ") {
				t.Fatal("read-only provider advertised an update")
			}
			m.focusProvidersSearch()
			if hint := m.providersContextHelp(); strings.Contains(hint, "q ") || strings.Contains(hint, "u ") || !strings.Contains(hint, m.tr("navigation.providers.searching")) {
				t.Fatalf("provider search advertised keys owned by text input: %s", hint)
			}
			m.providersKey(key("u"))
			if m.providers.search.Value() != "u" {
				t.Fatal("provider search did not own the update key")
			}
			m.providersKey(special(tea.KeyEscape))
			if m.providers.editing || !m.providers.open {
				t.Fatal("search Escape should end input without closing providers")
			}
			for width := 1; width <= 100; width++ {
				footer := m.providersFooterHelp(width)
				if ansi.StringWidth(footer) > width || strings.Contains(footer, "…") {
					t.Fatal("provider footer clipped a shortcut")
				}
			}
		}
	}
}
