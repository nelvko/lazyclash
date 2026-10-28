package mihomo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestEffectiveOwnsListenersAndPreservesRouting(t *testing.T) {
	raw := []byte(`proxies:
  - name: private-node
    type: socks5
    server: example.org
    port: 1080
proxy-groups:
  - name: PROXY
    type: select
    proxies: [private-node, DIRECT]
rules: ["MATCH,PROXY"]
external-controller: 0.0.0.0:9999
secret: subscription-secret
mixed-port: 9998
allow-lan: true
ss-config: ss://private-token
listeners: [{name: extra, type: http, port: 8080}]
tunnels: [{network: tcp, address: 0.0.0.0:2000, target: example.org:80}]
tun: {enable: true, auto-route: true}
dns: {enable: true, listen: 0.0.0.0:53}
iptables: {enable: true}
`)
	out, err := Effective(raw, "127.0.0.1:19090", "managed-secret", 17890)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err = yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["external-controller"] != "127.0.0.1:19090" || cfg["secret"] != "managed-secret" || cfg["mixed-port"] != 17890 || cfg["allow-lan"] != false {
		t.Fatal("managed listener fields lost")
	}
	if !strings.Contains(string(out), "private-node") || !strings.Contains(string(out), "MATCH,PROXY") {
		t.Fatal("routing definitions lost")
	}
	for _, key := range []string{"port", "socks-port", "redir-port", "tproxy-port"} {
		if cfg[key] != 0 {
			t.Fatalf("%s remains enabled", key)
		}
	}
	for _, key := range []string{"tun", "dns", "iptables", "ntp", "tuic-server"} {
		if cfg[key].(map[string]any)["enable"] != false {
			t.Fatalf("%s remains enabled", key)
		}
	}
	for _, key := range []string{"listeners", "tunnels"} {
		if len(cfg[key].([]any)) != 0 {
			t.Fatalf("%s remains enabled", key)
		}
	}
	if cfg["ss-config"] != "" {
		t.Fatal("SS inbound remains enabled")
	}
}

func TestDefaultContainsOneValidMatchRule(t *testing.T) {
	out, err := Default("127.0.0.1:19090", "managed-secret", 17890)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Rules  []string `yaml:"rules"`
		Groups []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}
	if err = yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0] != "MATCH,PROXY" || len(cfg.Groups) != 1 || len(cfg.Groups[0].Proxies) != 2 {
		t.Fatalf("invalid default: %+v", cfg)
	}
}

func TestEffectiveRejectsInvalidInputWithoutEchoingSecrets(t *testing.T) {
	for _, raw := range []string{"", "# comment", "[]", "private-token: [", "a: 1\na: 2", "a: 1\n---\nb: 2", "null"} {
		_, err := Effective([]byte(raw), "127.0.0.1:19090", "managed-secret", 17890)
		if err == nil {
			t.Fatalf("accepted %q", raw)
		}
		if strings.Contains(err.Error(), "private-token") {
			t.Fatal("parser error leaked input")
		}
	}
	for _, address := range []string{"0.0.0.0:19090", "localhost:19090", "192.168.1.1:19090", "127.0.0.1:80", "127.0.0.1:17890"} {
		if _, err := Default(address, "managed-secret", 17890); err == nil {
			t.Fatalf("accepted %q", address)
		}
	}
}

func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBinaryAndValidationWithholdOutput(t *testing.T) {
	if version, err := BinaryVersion(t.Context(), script(t, "echo 'Mihomo Meta v1.19.33 linux arm64'\n")); err != nil || version != Version {
		t.Fatalf("version=%q err=%v", version, err)
	}
	bad := script(t, "echo 'secret-private-token' >&2\nexit 1\n")
	if _, err := BinaryVersion(t.Context(), bad); err == nil || strings.Contains(err.Error(), "secret-private-token") {
		t.Fatalf("unsafe version error: %v", err)
	}
	if err := Validate(t.Context(), bad, "config", "data"); err == nil || strings.Contains(err.Error(), "secret-private-token") {
		t.Fatalf("unsafe validation error: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Validate(ctx, bad, "config", "data"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	// exec replaces the shell so CommandContext can terminate the actual process.
	ctx, cancel = context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := BinaryVersion(ctx, script(t, "exec sleep 10\n")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline lost: %v", err)
	}
}
