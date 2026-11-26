package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func openNetworkAccessForm(t *testing.T, id string, current app.RuntimeSettings) Model {
	t.Helper()
	b := &fakeBackend{snapshot: settingsTestSnapshot()}
	b.snapshot.RuntimeSettings = current
	b.getSettings = func(context.Context) (app.RuntimeSettings, error) { return current, nil }
	m := focusedPortsEntry(b, 160, 45)
	m.locale = i18n.New(i18n.Chinese)
	m.layout()
	m, cmd := click(m, findTarget(t, m, id))
	m, _ = update(m, actionCmd(t, cmd)())
	return m
}

func TestAccessRulesReviewOnlyChangedNetworksAndKeepDraftOnResize(t *testing.T) {
	s := app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}
	s.LANAllowedIPs, s.SkipAuthPrefixes = mihomo.DefaultLANAllowedIPs, "::1/128"
	m := openNetworkAccessForm(t, "settings.accessRules", s)
	if m.ports.step != "edit" || m.ports.focus != allowedNetworksID || slices.Contains(m.ports.controls, "ports.review") {
		t.Fatalf("unexpected initial rule form: %s %s %v", m.ports.step, m.ports.focus, m.ports.controls)
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.ports.focus != deniedNetworksID {
		t.Fatal("Tab did not move to the deny list")
	}
	m, _ = update(m, tea.PasteMsg{Content: "192.168.1.7\ninvalid"})
	if slices.Contains(m.ports.controls, "ports.review") || !strings.Contains(ansi.Strip(m.screen), m.tr("diagnostic.settings.invalidPrefix", 2)) {
		t.Fatal("invalid pasted list did not report its line and disable review")
	}
	m, _ = click(m, findTarget(t, m, "ports.rulesReset"))
	if m.ports.textarea(deniedNetworksID).Value() != "" || m.ports.textarea(allowedNetworksID).Value() != mihomo.DefaultLANAllowedIPs {
		t.Fatal("restore defaults did not reset both drafts")
	}
	m, _ = update(m, special(tea.KeyTab))
	m, _ = update(m, tea.PasteMsg{Content: "192.168.1.7"})
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, tea.PasteMsg{Content: "fd00::1"})
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 16})
	assertCanvas(t, m.screen, 40, 16)
	findTarget(t, m, deniedNetworksID)
	m, cmd := click(m, findTarget(t, m, "ports.review"))
	m, _ = update(m, actionCmd(t, cmd)())
	next := m.ports.plan.Proposed
	want := s
	want.LANDisallowedIPs = "192.168.1.7/32\nfd00::1/128"
	if m.ports.step != "review" || next != want {
		t.Fatalf("unexpected change: step %s, proposed %#v", m.ports.step, next)
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 45})
	body, _, _ := m.portsDocument(60)
	review := ansi.Strip(strings.Join(body.lines, "\n"))
	if !strings.Contains(review, m.tr("access.deniedNetworks")) || strings.Contains(review, m.tr("access.allowedNetworks")) {
		t.Fatalf("review must contain only the changed list: %s", review)
	}
}

func TestAccessRulesDistinguishDenyAllFromDefaultAndPreserveLongLists(t *testing.T) {
	s := app.RuntimeSettings{MixedPort: 7897}
	locale := i18n.New(i18n.Chinese)
	if got := accessRulesSummary(s, locale); got != "禁止全部" {
		t.Fatalf("empty allow list summary: %q", got)
	}
	s.LANAllowedIPs = mihomo.DefaultLANAllowedIPs
	s.LANDisallowedIPs = "192.168.1.7/32"
	if got := accessRulesSummary(s, locale); got != "允许全部 · 禁止 1" {
		t.Fatalf("default plus exception summary: %q", got)
	}
	m := openNetworkAccessForm(t, "settings.accessRules", s)
	m.ports.textarea(allowedNetworksID).SetValue("::/0\n0.0.0.0/0\n::/0")
	m.layout()
	if slices.Contains(m.ports.controls, "ports.review") {
		t.Fatal("merely reordering or repeating equivalent rules should preserve the inherited list")
	}
	m.ports.textarea(allowedNetworksID).Reset()
	m.layout()
	if !strings.Contains(ansi.Strip(m.screen), m.tr("access.emptyAllowWarning")) {
		t.Fatal("removing all allowed networks must warn about local clients")
	}
	long := strings.Repeat("192.168.10.0/24\n", 40) + "fd00::/8"
	if got := newAccessTextarea(long, "", 3).Value(); got != long {
		t.Fatal("multi-line editor silently truncated its saved value")
	}
	before := m.ports.textarea(allowedNetworksID).Value()
	m, _ = update(m, tea.PasteMsg{Content: strings.Repeat("192.0.2.0/24\n", 10000)})
	if m.ports.textarea(allowedNetworksID).Value() != before || m.ports.err != m.tr("access.listTooLong") {
		t.Fatal("oversized paste must leave the whole draft intact")
	}
}

func TestBindAddressCanBePreconfiguredWithLANOff(t *testing.T) {
	s := app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}
	s.BindAddress = "10.0.0.8"
	m := openNetworkAccessForm(t, "settings.bindAddress", s)
	if got := m.ports.field(bindAddressID).Value(); got != s.BindAddress || bindAddressSummary(s, m.locale) != got {
		t.Fatalf("off-state form failed to load the configured bind address: %q", got)
	}
	m.ports.field(bindAddressID).SetValue("bad host")
	m.layout()
	if slices.Contains(m.ports.controls, "ports.review") || m.ports.field(bindAddressID).Err == nil {
		t.Fatal("invalid host must disable review")
	}
	m, _ = click(m, findTarget(t, m, "ports.bindSample.[fd00::20]"))
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 16})
	assertCanvas(t, m.screen, 40, 16)
	m, cmd := click(m, findTarget(t, m, "ports.review"))
	m, _ = update(m, actionCmd(t, cmd)())
	want := s
	want.BindAddress = "[fd00::20]"
	if m.ports.step != "review" || m.ports.plan.Proposed != want {
		t.Fatalf("bind edit must preserve allow-lan and all other settings: %#v", m.ports.plan)
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.ports.open || m.snapshot.RuntimeSettings != s {
		t.Fatal("cancelling the review committed a draft")
	}
}
