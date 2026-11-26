package app

import (
	"net"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

func TestAccessRulesOverrideOnlyEditedNetworksAcrossProfileUpdates(t *testing.T) {
	a, _ := installedApp(t)
	ctx := t.Context()
	source := filepath.Join(t.TempDir(), "access.yaml")
	subscription := func(allowed, denied, exempt, account string) {
		t.Helper()
		write(t, source, []byte("rules: ['MATCH,DIRECT']\nlan-allowed-ips: ["+allowed+"]\nlan-disallowed-ips: ["+denied+"]\nskip-auth-prefixes: ["+exempt+"]\nauthentication: ["+account+"]\n"))
	}
	subscription("192.0.2.0/24", "192.0.2.7/32", "192.0.2.9/32", "one:fixture")
	if err := a.AddSubscription(ctx, "acl", source); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "acl"); err != nil {
		t.Fatal(err)
	}

	get := func() RuntimeSettings {
		t.Helper()
		s, err := a.GetRuntimeSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	apply := func(current, proposed RuntimeSettings) {
		t.Helper()
		plan, err := a.PrepareRuntimeSettingsEdit(ctx, current, proposed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.ApplyRuntimeSettings(ctx, plan); err != nil {
			t.Fatal(err)
		}
	}
	current := get()
	want := mihomo.AccessRules{LANAllowedIPs: "192.0.2.0/24", LANDisallowedIPs: "192.0.2.7/32", SkipAuthPrefixes: "192.0.2.9/32"}
	if current.AccessRules != want || current.ProxyUserCount != 1 || current.AllowLAN {
		t.Fatalf("inherited ACL = %+v", current.AccessRules)
	}
	next := current
	ports := availableSettings(t)
	next.MixedPort, next.ControllerPort = ports.MixedPort, ports.ControllerPort
	next.LANAllowedIPs = ""
	next.BindAddress = "::1"
	apply(current, next)
	current = get()
	if current.LANAllowedIPs != "" || current.ConfiguredBindAddress() != "[::1]" || current.ProxyListenHost() != "127.0.0.1" {
		t.Fatalf("inactive bind/empty allow = %+v", current)
	}
	next = current
	next.AllowLAN = true
	apply(current, next)
	current = get()
	if current.ProxyListenHost() != "::1" || current.ProxyUserCount != 1 {
		t.Fatal("saved bind or inherited account not restored")
	}
	editable, err := a.GetProxyAuth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := a.PrepareProxyAuthRules(ctx, editable, []ProxyAuthUser{{Username: "one", OriginalUsername: "one", Password: "changed-fixture"}}, editable.SkipAuthPrefixes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyRuntimeSettings(ctx, plan); err != nil {
		t.Fatal(err)
	}
	subscription("203.0.113.0/24", "203.0.113.3/32", "203.0.113.4/32", "two:new-fixture")
	if err := a.Refresh(ctx, "acl"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "acl"); err != nil {
		t.Fatal(err)
	}
	current = get()
	if current.LANAllowedIPs != "" || current.LANDisallowedIPs != "203.0.113.3/32" || current.SkipAuthPrefixes != "203.0.113.4/32" {
		t.Fatalf("unexpected ACL after refresh: %+v", current.AccessRules)
	}
	next = current
	next.AllowLAN = false
	apply(current, next)
	m, _, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	if m.Listeners.ConfiguredBindAddress() != "[::1]" || m.Listeners.ProxyListenHost() != "127.0.0.1" || m.AccessOverrides.LANAllowedIPs == nil || *m.AccessOverrides.LANAllowedIPs != "" || m.AccessOverrides.LANDisallowedIPs != nil || m.AccessOverrides.SkipAuthPrefixes != nil || len(m.proxyUsers) != 1 || m.proxyUsers[0] != "one:changed-fixture" {
		t.Fatal("toggling LAN or editing accounts changed unrelated overrides")
	}
}

func TestExemptionOnlyEditPreservesInheritedAccounts(t *testing.T) {
	a, _ := installedApp(t)
	ctx := t.Context()
	source := filepath.Join(t.TempDir(), "users.yaml")
	write(t, source, []byte("rules: ['MATCH,DIRECT']\nauthentication: [one:fixture]\n"))
	if err := a.AddSubscription(ctx, "users", source); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "users"); err != nil {
		t.Fatal(err)
	}
	ports := availableSettings(t)
	ports.AllowLAN = true
	plan, err := a.PrepareRuntimeSettings(ctx, ports)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyRuntimeSettings(ctx, plan); err != nil {
		t.Fatal(err)
	}
	original, err := a.GetProxyAuth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = a.PrepareProxyAuthRules(ctx, original, []ProxyAuthUser{{OriginalUsername: "one", Username: "one"}}, " ::1 \n::1/128\n")
	if err != nil || !plan.Changed {
		t.Fatal("exemption edit missing", err)
	}
	if _, err := a.ApplyRuntimeSettings(ctx, plan); err != nil {
		t.Fatal(err)
	}
	write(t, source, []byte("rules: ['MATCH,DIRECT']\nauthentication: [two:updated]\nskip-auth-prefixes: [10.0.0.0/8]\n"))
	if err := a.Refresh(ctx, "users"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "users"); err != nil {
		t.Fatal(err)
	}
	m, _, err := a.load()
	if err != nil || m.ManagedProxyAuth || len(m.proxyUsers) != 1 || m.proxyUsers[0] != "two:updated" || m.accessRules.SkipAuthPrefixes != "::1/128" {
		t.Fatal("source account was pinned during exemption-only edit", err)
	}
	if _, err := a.PrepareProxyAuthRules(ctx, original, nil, ""); err == nil {
		t.Fatal("stale exemption edit accepted")
	}
}

func TestActiveWildcardDoesNotMaskInvalidNewBindAddress(t *testing.T) {
	current := manifest{Controller: "127.0.0.1:9091", Port: 8888, Listeners: mihomo.ListenerOptions{AllowLAN: true}}
	next := manifestSettings(current)
	next.BindAddress = "192.0.2.253"
	if ln, err := net.Listen("tcp", net.JoinHostPort(next.BindAddress, "0")); err == nil {
		ln.Close()
		t.Skip("reserved documentation IP assigned to this machine")
	}
	if err := checkRuntimePorts(t.Context(), current, next, true); err == nil || !strings.Contains(err.Error(), "cannot bind") {
		t.Fatalf("invalid active wildcard-to-specific bind accepted: %v", err)
	}
}

func TestAppliedProfileDistinguishesMissingAndEmptyAllowedIPs(t *testing.T) {
	a, _ := installedApp(t)
	for _, tc := range []struct{ name, field, want string }{
		{"default", "", mihomo.DefaultLANAllowedIPs},
		{"empty", "lan-allowed-ips: []", ""},
		{"null", "lan-allowed-ips: null", ""},
	} {
		path := filepath.Join(t.TempDir(), tc.name+".yaml")
		write(t, path, []byte("rules: ['MATCH,DIRECT']\n"+tc.field+"\n"))
		if err := a.AddSubscription(t.Context(), tc.name, path); err != nil {
			t.Fatal(err)
		}
		if err := a.Apply(t.Context(), tc.name); err != nil {
			t.Fatal(err)
		}
		settings, err := a.GetRuntimeSettings(t.Context())
		if err != nil || settings.LANAllowedIPs != tc.want {
			t.Fatalf("%s allow list = %q, want %q: %v", tc.name, settings.LANAllowedIPs, tc.want, err)
		}
	}
	if err := a.Apply(t.Context(), "default"); err != nil {
		t.Fatal(err)
	}
	settings, err := a.GetRuntimeSettings(t.Context())
	if err != nil || settings.LANAllowedIPs != mihomo.DefaultLANAllowedIPs {
		t.Fatalf("restored allow list = %q: %v", settings.LANAllowedIPs, err)
	}
}

func TestLANToggleUsesDeployedAuthenticationUntilProfileRefresh(t *testing.T) {
	a, _ := installedApp(t)
	ctx := t.Context()
	source := filepath.Join(t.TempDir(), "accounts.yaml")
	write(t, source, []byte("rules: ['MATCH,DIRECT']\nauthentication: [old:fixture]\n"))
	if err := a.AddSubscription(ctx, "accounts", source); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "accounts"); err != nil {
		t.Fatal(err)
	}
	// An older loopback generation may already have stripped inherited accounts.
	// Simulate it without altering the pinned or current subscription bytes.
	m, original, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	_, config, _ := a.paths()
	stripped, err := mihomo.EffectiveListeners(read(t, config), m.Controller, m.Secret, m.Port, m.Listeners, []string{}, m.AccessOverrides)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(a.Home, "installation")
	target, err := a.writeGeneration(base, m, stripped)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Switch(filepath.Join(base, "current"), target, original); err != nil {
		t.Fatal(err)
	}
	write(t, source, []byte("rules: ['MATCH,DIRECT']\nauthentication: [fresh:fixture]\n"))

	ports := availableSettings(t)
	for _, enabled := range []bool{true, false} {
		current, err := a.GetRuntimeSettings(ctx)
		if err != nil || current.ProxyUserCount != 0 {
			t.Fatal("legacy generation had unexpected accounts", err)
		}
		next := current
		next.MixedPort, next.ControllerPort = ports.MixedPort, ports.ControllerPort
		next.AllowLAN = enabled
		plan, err := a.PrepareRuntimeSettingsEdit(ctx, current, next)
		if err != nil || plan.Proposed.ProxyUserCount != 0 {
			t.Fatal("LAN switch restored unreviewed accounts", err)
		}
		if _, err := a.ApplyRuntimeSettings(ctx, plan); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Refresh(ctx, "accounts"); err != nil {
		t.Fatal(err)
	}
	current, err := a.GetProxyAuth(ctx)
	if err != nil || !slices.Equal(current.Usernames, []string{"fresh"}) {
		t.Fatal("refreshed profile did not restore newly applied accounts while LAN was off", err)
	}
	for _, enabled := range []bool{true, false} {
		ports.AllowLAN = enabled
		plan, err := a.PrepareRuntimeSettings(ctx, ports)
		if err != nil || plan.Proposed.ProxyUserCount != 1 {
			t.Fatal("LAN toggle altered inherited account count", err)
		}
		if _, err := a.ApplyRuntimeSettings(ctx, plan); err != nil {
			t.Fatal(err)
		}
		observed, err := a.GetProxyAuth(ctx)
		if err != nil || !slices.Equal(observed.Usernames, current.Usernames) {
			t.Fatal("LAN toggle altered refreshed authentication", err)
		}
	}
}
