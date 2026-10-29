package app

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/configuration"
	"gopkg.in/yaml.v3"
)

func addNodeSource(t *testing.T, a *App, name, node string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".yaml")
	write(t, path, []byte("proxies:\n - {name: "+node+", type: socks5, server: example.invalid, port: 1080}\n"))
	if err := a.AddSource(t.Context(), name, path); err != nil {
		t.Fatal(err)
	}
	return path
}

func generatedDocument(t *testing.T, a *App) map[string]any {
	t.Helper()
	_, path, _ := a.paths()
	var cfg map[string]any
	if err := yaml.Unmarshal(read(t, path), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestManagedProfileReusesIndependentSourcesAndPinsAppliedInputs(t *testing.T) {
	a, svc := installedApp(t)
	alpha := addNodeSource(t, a, "alpha", "node-one")
	addNodeSource(t, a, "beta", "node-two")
	spec := configuration.Profile{
		Core: "mihomo", Mode: "managed", Sources: []string{"beta", "alpha"},
		Template:  "proxy-groups: [{name: PROXY, type: select, include-all-proxies: true}]\nrules: ['MATCH,PROXY']\n",
		Overrides: "prepend-rules: ['DOMAIN,custom.invalid,REJECT']\n",
	}
	if err := a.CreateProfile(t.Context(), "combined", spec); err != nil {
		t.Fatal(err)
	}
	info, err := a.ProfileDetails(t.Context(), "combined")
	if err != nil || info.Mode != "managed" || !info.Validated || info.Pending || !reflect.DeepEqual(info.Sources, []string{"beta", "alpha"}) {
		t.Fatalf("unexpected saved recipe: %+v %v", info, err)
	}
	m, old, err := a.load()
	if err != nil || m.Applied != "default" || len(svc.actions) != 0 {
		t.Fatal("creating subscriptions or a recipe selected or started it", err)
	}
	if err := a.Apply(t.Context(), "combined"); err != nil {
		t.Fatal(err)
	}
	m, applied, err := a.load()
	if err != nil || applied == old || m.Build == nil || m.Build.Recipe == nil || m.Build.Recipe.Mode != "managed" || len(m.Build.Sources) != 2 {
		t.Fatal("applied generation did not pin its recipe and source revisions", err)
	}
	cfg := generatedDocument(t, a)
	nodes := cfg["proxies"].([]any)
	if len(nodes) != 2 || nodes[0].(map[string]any)["name"] != "node-two" || nodes[1].(map[string]any)["name"] != "node-one" {
		t.Fatal("managed profile did not preserve its explicit source order")
	}
	rules := cfg["rules"].([]any)
	if !reflect.DeepEqual(rules, []any{"DOMAIN,custom.invalid,REJECT", "MATCH,PROXY"}) {
		t.Fatal("managed template and custom rules were not composed")
	}
	pinned := m.Build.fingerprint()
	_, appliedPath, _ := a.paths()
	before := read(t, appliedPath)
	write(t, alpha, []byte("proxies: [{name: node-one, type: socks5, server: updated.invalid, port: 2080}]\n"))
	if err := a.RefreshSource(t.Context(), "alpha"); err != nil {
		t.Fatal(err)
	}
	info, err = a.ProfileDetails(t.Context(), "combined")
	if err != nil || !info.Pending {
		t.Fatal("source refresh did not expose the unapplied revision", err)
	}
	lib, _, err := a.library().Load()
	if err != nil || lib.Profiles["combined"].Spec.Overrides != spec.Overrides {
		t.Fatal("source refresh replaced the user's rules", err)
	}
	nextManifest, current, err := a.load()
	if err != nil || current != applied || nextManifest.Build.fingerprint() != pinned || !bytes.Equal(read(t, appliedPath), before) || len(svc.actions) != 0 {
		t.Fatal("source refresh changed the committed generation or service", err)
	}
	// A load failure restores the previous applied inputs while leaving the
	// independently saved source revision available for an explicit retry.
	svc.active = true
	failed := false
	svc.actionHook = func(context.Context, string) error {
		if !failed {
			failed = true
			return errors.New("new managed configuration failed to start")
		}
		return nil
	}
	if err := a.Apply(t.Context(), "combined"); err == nil {
		t.Fatal("failed managed apply was reported as successful")
	}
	restored, restoredGeneration, err := a.load()
	if err != nil || restoredGeneration != applied || restored.Build.fingerprint() != pinned || !bytes.Equal(read(t, appliedPath), before) {
		t.Fatal("failed apply did not restore the old pinned configuration", err)
	}
	info, err = a.ProfileDetails(t.Context(), "combined")
	if err != nil || !info.Pending {
		t.Fatal("rollback discarded the saved source update", err)
	}
	svc.active = false
	svc.actionHook = nil
	if err := a.Apply(t.Context(), "combined"); err != nil {
		t.Fatal(err)
	}
	info, err = a.ProfileDetails(t.Context(), "combined")
	if err != nil || info.Pending || !strings.Contains(string(read(t, appliedPath)), "updated.invalid") {
		t.Fatal("explicit retry did not commit the pending source revision", err)
	}
}

func TestSourceRoutingIsValidatedOnlyWhenAFullProfileConsumesIt(t *testing.T) {
	a, svc := testApp(t)
	path := filepath.Join(t.TempDir(), "partial.yaml")
	write(t, path, []byte("proxies: [{name: node, type: direct}]\nproxy-groups: [{name: supplier, type: select, proxies: [absent]}]\nrules: ['MATCH,supplier']\n"))
	if err := a.AddSource(t.Context(), "nodes", path); err != nil {
		t.Fatal("unused source routing prevented node-source storage", err)
	}
	if err := a.CreateProfile(t.Context(), "full", configuration.Profile{Mode: "full", Source: "nodes"}); err == nil {
		t.Fatal("full profile accepted a dangling outbound")
	}
	assertLibraryEntryMissing(t, a, "full")
	if err := a.CreateProfile(t.Context(), "managed", configuration.Profile{Mode: "managed", Sources: []string{"nodes"}, Template: "proxy-groups: [{name: PROXY, type: select, include-all-proxies: true}]\nrules: ['MATCH,PROXY']"}); err != nil {
		t.Fatal("managed profile consumed unused supplier routing", err)
	}
	info, err := a.ProfileDetails(t.Context(), "managed")
	if err != nil || info.Validated || info.Pending || svc.installs != 0 || len(svc.actions) != 0 {
		t.Fatal("pre-install recipe invented engine validation or changed service state", err)
	}
}

func TestProfileOverrideEditsStayDraftUntilExplicitApply(t *testing.T) {
	a, svc := installedApp(t)
	addFixtureProfile(t, a, "custom")
	if err := a.Apply(t.Context(), "custom"); err != nil {
		t.Fatal(err)
	}
	_, applied, _ := a.load()
	_, appliedPath, _ := a.paths()
	before := read(t, appliedPath)
	override := []byte("prepend-rules: ['DOMAIN,user.invalid,REJECT']\nset: {log-level: info}\n")
	if err := a.SetProfileOverrides(t.Context(), "custom", override); err != nil {
		t.Fatal(err)
	}
	info, err := a.ProfileDetails(t.Context(), "custom")
	if err != nil || !info.Pending || !bytes.Equal(before, read(t, appliedPath)) || len(svc.actions) != 0 {
		t.Fatal("editing overrides implicitly applied its draft", err)
	}
	libraryPath := filepath.Join(a.Home, "configuration", "library.json")
	beforeLibrary := read(t, libraryPath)
	for _, invalid := range [][]byte{[]byte("set: {rules: ['MATCH,DIRECT']}\nappend-rules: ['DOMAIN,hidden.invalid,DIRECT']"), []byte("set: {mixed-port: 20000}"), []byte("set: {proxy-groups: []}")} {
		if err := a.SetProfileOverrides(t.Context(), "custom", invalid); err == nil {
			t.Fatal("invalid override was saved")
		}
		if !bytes.Equal(beforeLibrary, read(t, libraryPath)) {
			t.Fatal("failed override edit changed the saved recipe")
		}
	}
	a.Validate = func(context.Context, string, string, string) error { return errors.New("core validation failed") }
	if err := a.SetProfileOverrides(t.Context(), "custom", []byte("set: {log-level: debug}")); err == nil || !bytes.Equal(beforeLibrary, read(t, libraryPath)) {
		t.Fatal("engine-rejected override changed the library", err)
	}
	a.Validate = func(ctx context.Context, _, _, _ string) error { return ctx.Err() }
	if err := a.Apply(t.Context(), "custom"); err != nil {
		t.Fatal(err)
	}
	_, current, _ := a.load()
	info, err = a.ProfileDetails(t.Context(), "custom")
	if err != nil || info.Pending || current == applied || !strings.Contains(string(read(t, appliedPath)), "DOMAIN,user.invalid,REJECT") {
		t.Fatal("explicit apply did not commit custom rules", err)
	}
}

func TestProfileAndSourceDeletionProtectsSelectionDependenciesAndHistoricalInputs(t *testing.T) {
	a, svc := installedApp(t)
	addNodeSource(t, a, "shared", "node")
	for _, name := range []string{"primary", "secondary"} {
		if err := a.CreateProfile(t.Context(), name, configuration.Profile{Mode: "full", Source: "shared"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Apply(t.Context(), "primary"); err != nil {
		t.Fatal(err)
	}
	snapshotPath := savedSourcePath(t, a, "shared")
	snapshot := read(t, snapshotPath)
	if err := a.DeleteProfile(t.Context(), "primary"); err == nil {
		t.Fatal("deleted selected profile")
	}
	if err := a.DeleteSource(t.Context(), "shared"); err == nil {
		t.Fatal("deleted a referenced source")
	}
	if err := a.Apply(t.Context(), "default"); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteProfile(t.Context(), "primary"); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteSource(t.Context(), "shared"); err == nil {
		t.Fatal("ignored a second profile's dependency")
	}
	if err := a.DeleteProfile(t.Context(), "secondary"); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteSource(t.Context(), "shared"); err != nil {
		t.Fatal(err)
	}
	lib, _, err := a.library().Load()
	if err != nil || len(lib.Profiles) != 0 || len(lib.Subscriptions) != 0 || !bytes.Equal(snapshot, read(t, snapshotPath)) || len(svc.actions) != 0 {
		t.Fatal("deletion changed historical input snapshots or service state", err)
	}
	if err := a.DeleteProfile(t.Context(), "default"); err == nil {
		t.Fatal("deleted the reserved default profile")
	}
}
