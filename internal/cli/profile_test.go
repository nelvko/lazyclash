package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
)

func runProfileCLI(t *testing.T, backend *app.App, args ...string) (string, error) {
	t.Helper()
	cmd := newProfileCommand(func() *app.App { return backend })
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return output.String(), err
}

func writeCLIConfig(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.yaml")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func configDigest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestProfileRememberSourceAndExplicitRefreshPreserveStoppedCore(t *testing.T) {
	a, service := installBackend(t)
	initial := "mode: rule\nproxies: []\nproxy-groups: []\nrules: ['MATCH,DIRECT']\n"
	source := writeCLIConfig(t, initial)
	if _, err := runProfileCLI(t, a, "add", "saved", source, "--remember-source"); err != nil {
		t.Fatal(err)
	}
	sources, err := a.ListSources(t.Context())
	if err != nil || len(sources) != 1 || !sources[0].Refreshable || sources[0].Revision != configDigest(initial) {
		t.Fatalf("remembered source: %+v %v", sources, err)
	}
	updated := "mode: direct\nproxies: []\nproxy-groups: []\nrules: ['MATCH,DIRECT']\n"
	if err := os.WriteFile(source, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runProfileCLI(t, a, "refresh", "saved"); err != nil {
		t.Fatal(err)
	}
	sources, err = a.ListSources(t.Context())
	if err != nil || len(sources) != 1 || sources[0].Revision != configDigest(updated) {
		t.Fatalf("refresh did not publish the new source revision: %+v %v", sources, err)
	}
	if service.installs != 0 || service.actions != 0 {
		t.Fatal("subscription command installed or started a core")
	}
}

func TestProfileImportDoesNotRememberSourceByDefault(t *testing.T) {
	a, service := installBackend(t)
	source := writeCLIConfig(t, "proxies: []\nrules: ['MATCH,DIRECT']\n")
	if _, err := runProfileCLI(t, a, "add", "copied", source); err != nil {
		t.Fatal(err)
	}
	sources, err := a.ListSources(t.Context())
	if err != nil || len(sources) != 1 || sources[0].Refreshable {
		t.Fatalf("ordinary import remembered its source: %+v %v", sources, err)
	}
	if _, err := runProfileCLI(t, a, "refresh", "copied"); err == nil {
		t.Fatal("refresh accepted a source that was not remembered")
	}
	if service.actions != 0 || service.installs != 0 {
		t.Fatal("import touched the core service")
	}
}

func TestProfileRecipesOverridesAndReferenceSafeDeletionWithoutInstallation(t *testing.T) {
	a, service := installBackend(t)
	for _, name := range []string{"first", "second"} {
		raw := "proxies:\n  - name: " + name + "\n    type: ss\n    server: example.com\n    port: 443\n    cipher: aes-128-gcm\n    password: private-node-password\n"
		if _, err := runSubscriptionCLI(t, a, "add", name, writeCLIConfig(t, raw)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runProfileCLI(t, a, "create", "full", "--source", "first"); err != nil {
		t.Fatal(err)
	}
	template := writeCLIConfig(t, "proxy-groups:\n  - name: Proxy\n    type: select\n    include-all: true\nrules: ['MATCH,Proxy']\n")
	if _, err := runProfileCLI(t, a, "create", "combined", "--managed", "--sources", "first,second", "--template", template); err != nil {
		t.Fatal(err)
	}
	override := writeCLIConfig(t, "set:\n  dns:\n    enable: true\n    nameserver: ['https://dns.example/dns-query']\nprepend-rules: ['DOMAIN,local.example,DIRECT']\n")
	if _, err := runProfileCLI(t, a, "override", "combined", override); err != nil {
		t.Fatal(err)
	}
	output, err := runProfileCLI(t, a, "show", "combined")
	var detail app.ProfileInfo
	if err != nil || json.Unmarshal([]byte(output), &detail) != nil || detail.Name != "combined" || detail.Mode != "managed" || detail.Core != "mihomo" || len(detail.Sources) != 2 {
		t.Fatalf("profile details: %s %v", output, err)
	}
	if strings.Contains(output, "private-node-password") || strings.Contains(output, "dns.example") {
		t.Fatal("show exposed configuration content")
	}
	output, err = runProfileCLI(t, a, "list")
	var profiles []app.ProfileInfo
	if err != nil || json.Unmarshal([]byte(output), &profiles) != nil || len(profiles) != 3 {
		t.Fatalf("profile list without installation: %s %v", output, err)
	}
	if _, err := runSubscriptionCLI(t, a, "delete", "first"); err == nil {
		t.Fatal("deleted a referenced subscription")
	}
	for _, name := range []string{"full", "combined"} {
		if _, err := runProfileCLI(t, a, "delete", name); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"first", "second"} {
		if _, err := runSubscriptionCLI(t, a, "delete", name); err != nil {
			t.Fatal(err)
		}
	}
	if service.actions != 0 || service.installs != 0 {
		t.Fatal("recipe editing touched the core service")
	}
}

func TestProfileCreateRejectsAmbiguousFlagsAndUnsafeInputFiles(t *testing.T) {
	a, _ := installBackend(t)
	for _, args := range [][]string{
		{"create", "full"},
		{"create", "full", "--source", "one", "--sources", "two"},
		{"create", "managed", "--managed", "--source", "one", "--sources", "two", "--template", "/unused"},
		{"create", "managed", "--managed", "--sources", "one"},
		{"create", "managed", "--managed", "--sources", "one", "--template", "relative.yaml"},
		{"override", "full", "relative.yaml"},
	} {
		if _, err := runProfileCLI(t, a, args...); err == nil {
			t.Fatalf("accepted invalid arguments %v", args)
		}
	}
	file := writeCLIConfig(t, "set: {}\n")
	link := filepath.Join(t.TempDir(), "link.yaml")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProfileInput(link); err == nil {
		t.Fatal("accepted symlink input")
	}
	large := writeCLIConfig(t, strings.Repeat("a", (8<<20)+1))
	if _, err := readProfileInput(large); err == nil {
		t.Fatal("accepted oversized input")
	}
}
