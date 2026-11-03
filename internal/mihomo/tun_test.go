package mihomo

import (
	"testing"
)

func TestTUNRequiresManagedAuthorizationAndUsesPrivateResources(t *testing.T) {
	raw, err := Default("127.0.0.1:9097", "test-secret", 7897)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ManagedTUN("lazyclash-first")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := ManagedTUN("lazyclash-second")
	if first == second || len(first.Device) > 15 || first.Rule+10 >= 32766 {
		t.Fatalf("invalid resource allocation: %+v %+v", first, second)
	}
	enabled, err := WithTUN(raw, true, "lazyclash-first")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := decodeConfig(enabled)
	if err != nil {
		t.Fatal(err)
	}
	tun := cfg["tun"].(map[string]any)
	if tun["device"] != first.Device || tun["iproute2-table-index"] != first.Table || tun["iproute2-rule-index"] != first.Rule || tun["auto-redirect"] != false || tun["dns-mode"] != "disabled" {
		t.Fatalf("unexpected TUN policy: %#v", tun)
	}
	if _, err := Effective(enabled, "127.0.0.1:9097", "test-secret", 7897); err == nil {
		t.Fatal("subscription TUN was accepted as authorization")
	}
	disabled, err := WithTUN(enabled, false, "lazyclash-first")
	if err != nil {
		t.Fatal(err)
	}
	if on, err := ConfigTUN(disabled); err != nil || on {
		t.Fatalf("disable TUN: %v %v", on, err)
	}
	if _, err := WithTUN(raw, true, "../../foreign"); err == nil {
		t.Fatal("invalid service identity accepted")
	}
}

func TestBuildPreservesExplicitTUNPolicyAcrossProfileBuild(t *testing.T) {
	raw, err := Build(BuildOptions{Base: []byte("rules: [\"MATCH,DIRECT\"]\n"), Controller: "127.0.0.1:9097", Secret: "test-secret", MixedPort: 7897, Mode: "direct", TUN: true, ServiceName: "lazyclash-test"})
	if err != nil {
		t.Fatal(err)
	}
	if on, err := ConfigTUN(raw); err != nil || !on {
		t.Fatalf("TUN lost during build: %v %v", on, err)
	}
	if mode, err := ConfigMode(raw); err != nil || mode != "direct" {
		t.Fatalf("mode lost during build: %q %v", mode, err)
	}
}
