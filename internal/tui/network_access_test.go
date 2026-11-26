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

func openNetworkAccessForm(t *testing.T, id string, current app.RuntimeSettings, addresses ...app.BindAddressOption) Model {
	t.Helper()
	b := &fakeBackend{snapshot: settingsTestSnapshot(), bindAddresses: addresses}
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

func TestAccessRulesSummary(t *testing.T) {
	for _, tc := range []struct{ config, zh, en string }{
		{"{}", "❯", "❯"},
		{"lan-allowed-ips: ['::/0', '0.0.0.0/0', '::/0']", "❯", "❯"},
		{"skip-auth-prefixes: ['192.0.2.1']", "❯", "❯"},
		{"lan-allowed-ips: []", "自定义 ❯", "Custom ❯"},
		{"lan-allowed-ips: ['0.0.0.0/0']", "自定义 ❯", "Custom ❯"},
		{"lan-disallowed-ips: ['192.0.2.1']", "自定义 ❯", "Custom ❯"},
	} {
		_, rules, err := mihomo.ConfigProxyAccess([]byte(tc.config))
		if err != nil {
			t.Fatal(err)
		}
		for lang, value := range map[string]string{i18n.Chinese: tc.zh, i18n.English: tc.en} {
			o := dashboardOptions{Locale: i18n.New(lang), Snapshot: settingsTestSnapshot()}
			o.Snapshot.RuntimeSettings.AccessRules = rules
			want := o.Locale.T("settings.accessRules") + " " + value
			for _, width := range []int{40, 160} {
				body, hits := settingsContent(o, width)
				row := settingsPaintedTarget(t, body, hits, "settings.accessRules")
				if got := strings.Join(strings.Fields(row), " "); got != want {
					t.Errorf("%s/%d columns, %s: got %q, want %q", lang, width, tc.config, got, want)
				}
			}
		}
	}
}

func TestAccessRulesDistinguishDenyAllFromDefaultAndPreserveLongLists(t *testing.T) {
	s := app.RuntimeSettings{MixedPort: 7897}
	s.LANAllowedIPs = mihomo.DefaultLANAllowedIPs
	s.LANDisallowedIPs = "192.168.1.7/32"
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
	m := openNetworkAccessForm(t, "settings.bindAddress", s, app.BindAddressOption{Address: "[fd00::20]", Interface: "eth0"})
	if got := m.ports.field(bindAddressID).Value(); got != s.BindAddress || bindAddressSummary(s, m.locale) != got {
		t.Fatalf("off-state form failed to load the configured bind address: %q", got)
	}
	m.ports.field(bindAddressID).SetValue("bad host")
	m.layout()
	if slices.Contains(m.ports.controls, "ports.review") || m.ports.field(bindAddressID).Err == nil {
		t.Fatal("invalid host must disable review")
	}
	m, _ = click(m, findTarget(t, m, bindOptionPrefix+"[fd00::20]"))
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

func TestBindAddressPickerUsesDraftAndKeepsManualEntry(t *testing.T) {
	s := app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}
	s.BindAddress = "*"
	m := openNetworkAccessForm(t, "settings.bindAddress", s, app.BindAddressOption{Address: "192.0.2.9", Interface: "eth0"})
	m, _ = update(m, special(tea.KeyDown))
	m, _ = update(m, special(tea.KeyEnter))
	if m.ports.field(bindAddressID).Value() != "192.0.2.9" || m.ports.step != "edit" || m.ports.access.bindMenu.open {
		t.Fatal("selecting a candidate must only fill the draft")
	}
	m, _ = click(m, findTarget(t, m, bindMenuID))
	m, _ = update(m, special(tea.KeyEscape))
	if !m.ports.open || m.ports.access.bindMenu.open {
		t.Fatal("Escape must dismiss the open list before the dialog")
	}
	m = openNetworkAccessForm(t, "settings.bindAddress", s)
	if slices.Contains(m.ports.controls, bindMenuID) {
		t.Fatal("unknown host must not offer interface candidates")
	}
	m, _ = update(m, tea.PasteMsg{Content: "fd00::7"})
	want, issues := m.networkAccessProposal()
	if len(issues) != 0 || want.BindAddress != "[fd00::7]" {
		t.Fatalf("manual IPv6 address rejected: %#v %v", want, issues)
	}
	m, _ = click(m, findTarget(t, m, bindAllID))
	if m.ports.field(bindAddressID).Value() != "*" || slices.Contains(m.ports.controls, "ports.review") {
		t.Fatal("restoring all interfaces should restore the original draft")
	}
}

func TestBindPickerWrapsCompleteIPv6Neighbors(t *testing.T) {
	const first = "[2001:db8:1234:5678:abcd:1234:5678:1979]"
	const second = "[2001:db8:1234:5678:abcd:1234:5678:1980]"
	m := openNetworkAccessForm(t, "settings.bindAddress", app.RuntimeSettings{MixedPort: 7897},
		app.BindAddressOption{Address: first, Interface: "eth0"}, app.BindAddressOption{Address: second, Interface: "eth0"})
	for _, address := range []string{first, second} {
		row := settingsPaintedTarget(t, m.screen, m.targets, bindOptionPrefix+address)
		if !strings.Contains(row, address) || !strings.Contains(row, "eth0") {
			t.Fatalf("wide option must show the address and interface together: %q", row)
		}
	}
	for _, tc := range []struct {
		address string
		height  int
	}{{first, 24}, {second, 12}} {
		address := tc.address
		m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: tc.height})
		m, _ = update(m, special(tea.KeyDown))
		assertCanvas(t, m.screen, 40, tc.height)
		lines := strings.Split(m.screen, "\n")
		var painted strings.Builder
		for _, hit := range m.targets {
			if hit.ID == bindOptionPrefix+address {
				for y := hit.Y; y < hit.Y+hit.H; y++ {
					part := ansi.Strip(ansi.Cut(lines[y], hit.X, hit.X+hit.W))
					painted.WriteString(strings.Trim(part, " │›✓"))
				}
			}
		}
		label := painted.String()
		if !strings.Contains(label, address) || !strings.Contains(label, "eth0") || strings.Contains(label, "…") {
			t.Fatalf("narrow option must wrap the full address %q and interface, got %q", address, label)
		}
	}
	if got := m.ports.field(bindAddressID).Value(); got != "*" {
		t.Fatalf("resizing or browsing options changed the draft to %q", got)
	}
	hit := findTarget(t, m, bindOptionPrefix+second)
	m, _ = update(m, tea.MouseClickMsg{X: hit.X + hit.W/2, Y: hit.Y + 1, Button: tea.MouseLeft})
	if got := m.ports.field(bindAddressID).Value(); got != second || m.ports.access.bindMenu.open {
		t.Fatalf("clicking the continuation line of the second option filled %q", got)
	}
}
