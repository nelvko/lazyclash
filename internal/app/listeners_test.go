package app

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReviewedListenersPersistWithPinnedProfileACLAndWriteOnlySecret(t *testing.T) {
	a, _ := installedApp(t)
	ctx := t.Context()
	source := filepath.Join(t.TempDir(), "acl.yaml")
	write(t, source, []byte("rules: [MATCH,DIRECT]\nauthentication: [fixture:original]\nlan-allowed-ips: [192.0.2.0/24]\nlan-disallowed-ips: [192.0.2.1/32]\n"))
	if err := a.AddSubscription(ctx, "acl", source); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "acl"); err != nil {
		t.Fatal(err)
	}
	write(t, source, []byte("rules: [MATCH,DIRECT]\nauthentication: [fixture:new]\n"))
	original, err := a.GetRuntimeSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next := availableSettings(t)
	next.AccessRules = original.AccessRules
	next.HTTPPort, next.MixedPort = next.MixedPort, 0
	next.AllowLAN = true
	next.NewSecret = "rotated-private-test-value"
	next.AllowedOrigins = "https://panel.example\nhttps://*.example"
	next.AllowPrivateNetwork = true
	next.UnixSocket = "control.sock"
	next.DoHPath = "/dns-query"
	plan, err := a.PrepareRuntimeSettingsEdit(ctx, original, next)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(plan)
	if !plan.SecretChanged || plan.Proposed.NewSecret != "" || strings.Contains(string(public), next.NewSecret) || strings.Contains(fmt.Sprintf("%+v", plan), next.NewSecret) {
		t.Fatal("review exposed a replacement credential")
	}
	result, err := a.ApplyRuntimeSettings(ctx, plan)
	if err != nil || result.Settings.NewSecret != "" {
		t.Fatal("failed to apply reviewed listener settings", err)
	}
	if _, err := a.PrepareRuntimeSettingsEdit(ctx, original, original); err == nil {
		t.Fatal("stale listener edit accepted")
	}
	if _, err := a.Port(ctx); err == nil {
		t.Fatal("exported a disabled mixed endpoint")
	}
	m, _, err := a.load()
	if err != nil || m.Secret != next.NewSecret {
		t.Fatal("replacement credential not persisted")
	}
	check := func(want string) {
		t.Helper()
		_, config, _ := a.paths()
		var cfg map[string]any
		if err := yaml.Unmarshal(read(t, config), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg["bind-address"] != "*" || cfg["mixed-port"] != 0 || cfg["port"] != next.HTTPPort || cfg["external-controller-unix"] != next.UnixSocket || cfg["external-doh-server"] != next.DoHPath || cfg["secret"] != next.NewSecret {
			t.Fatal("persisted listeners changed during generation rebuild")
		}
		if want != "" && (fmt.Sprint(cfg["authentication"]) != want || fmt.Sprint(cfg["lan-allowed-ips"]) != "[192.0.2.0/24]" || fmt.Sprint(cfg["lan-disallowed-ips"]) != "[192.0.2.1/32]") {
			t.Fatal("enabling LAN discarded the applied profile access policy")
		}
	}
	check("[fixture:original]")
	snapshot, err := a.Snapshot(ctx)
	raw, _ := json.Marshal(snapshot)
	if err != nil || !snapshot.RuntimeSettings.AllowLAN || strings.Contains(string(raw), next.NewSecret) || strings.Contains(string(raw), "fixture:original") {
		t.Fatal("snapshot missing settings or exposed credentials", err)
	}
	if err := a.DeactivateProfile(ctx, ""); err != nil {
		t.Fatal(err)
	}
	check("")
}

func TestReviewRejectsForeignSocketsAndUDPConflicts(t *testing.T) {
	a, _ := installedApp(t)
	next := availableSettings(t)
	file := filepath.Join(t.TempDir(), "control.sock")
	write(t, file, []byte("keep"))
	next.UnixSocket = file
	if _, err := a.PrepareRuntimeSettings(t.Context(), next); err == nil {
		t.Fatal("foreign file accepted as a Unix socket")
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "keep" {
		t.Fatal("foreign socket candidate modified")
	}
	next.UnixSocket = ""
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	next.SOCKSPort = udp.LocalAddr().(*net.UDPAddr).Port
	if _, err := a.PrepareRuntimeSettings(t.Context(), next); err == nil || !strings.Contains(err.Error(), "udp") {
		t.Fatal("SOCKS UDP conflict not identified", err)
	}
}

func TestSecretOnlyRotationInvalidatesOtherOpenDrafts(t *testing.T) {
	a, _ := installedApp(t)
	ports, err := a.PrepareRuntimeSettings(t.Context(), availableSettings(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.ApplyRuntimeSettings(t.Context(), ports); err != nil {
		t.Fatal(err)
	}
	original, err := a.GetRuntimeSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	next := original
	next.NewSecret = "replacement-test-value"
	plan, err := a.PrepareRuntimeSettingsEdit(t.Context(), original, next)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyRuntimeSettings(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	next.NewSecret = "another-test-value"
	if _, err := a.PrepareRuntimeSettingsEdit(t.Context(), original, next); err == nil {
		t.Fatal("concurrent secret rotation accepted a stale draft")
	}
}
