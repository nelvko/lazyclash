package app

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func generatedDocument(t *testing.T, a *App) map[string]any {
	t.Helper()
	_, path, _ := a.paths()
	var cfg map[string]any
	if err := yaml.Unmarshal(read(t, path), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestCreateProfileStoresDescriptionWithoutChangingRuntimeRecipe(t *testing.T) {
	a, _ := installedApp(t)
	path := filepath.Join(t.TempDir(), "remote-like.yaml")
	write(t, path, []byte("rules: ['MATCH,DIRECT']\n"))
	if err := a.CreateProfile(t.Context(), ProfileDraft{Name: "Work", Description: "  office\nroute  ", Address: path, RememberSource: true}); err != nil {
		t.Fatal(err)
	}
	info, err := a.ProfileDetails(t.Context(), "Work")
	if err != nil {
		t.Fatal(err)
	}
	if info.Description != "office route" || !info.Refreshable {
		t.Fatalf("unexpected profile metadata: %+v", info)
	}
	lib, _, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := lib.Profiles[info.ID].Spec.Overrides; got != "" {
		t.Fatalf("description leaked into runtime recipe: %q", got)
	}
}

func TestUpdateProfileDescriptionOnlyChangesMetadata(t *testing.T) {
	a, _ := installedApp(t)
	path := filepath.Join(t.TempDir(), "profile.yaml")
	write(t, path, []byte("rules: ['MATCH,DIRECT']\n"))
	if err := a.AddSubscription(t.Context(), "Work", path); err != nil {
		t.Fatal(err)
	}
	lib, _, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	revision := lib.Profiles["Work"].Spec.Revision
	if err := a.UpdateProfileDescription(t.Context(), "Work", "  office\nroute  "); err != nil {
		t.Fatal(err)
	}
	updated, _, err := a.library().Load()
	if err != nil || updated.Profiles["Work"].Spec.Revision != revision || updated.Profiles["Work"].Description != "office route" {
		t.Fatalf("description update changed recipe or was not saved: %+v %v", updated.Profiles["Work"], err)
	}
}

func TestFullProfilePinsAppliedContentAndCustomRules(t *testing.T) {
	a, svc := installedApp(t)
	path := filepath.Join(t.TempDir(), "profile.yaml")
	write(t, path, []byte("proxies: [{name: node-one, type: socks5, server: example.invalid, port: 1080}]\nproxy-groups: [{name: PROXY, type: select, proxies: [node-one, DIRECT]}]\nrules: ['MATCH,PROXY']\n"))
	if err := a.AddSubscription(t.Context(), "saved", path); err != nil {
		t.Fatal(err)
	}
	overrides := "prepend-rules: ['DOMAIN,custom.invalid,REJECT']\n"
	if err := a.SetProfileOverrides(t.Context(), "saved", []byte(overrides)); err != nil {
		t.Fatal(err)
	}
	info, err := a.ProfileDetails(t.Context(), "saved")
	if err != nil || !info.Refreshable || !info.Validated || info.Pending {
		t.Fatalf("unexpected saved profile: %+v %v", info, err)
	}
	m, old, err := a.load()
	if err != nil || m.Applied != "default" || len(svc.actions) != 0 {
		t.Fatal("adding or editing a profile selected or started it", err)
	}
	if err := a.Apply(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	m, applied, err := a.load()
	if err != nil || applied == old || m.Build == nil || m.Build.Recipe == nil || m.Build.Recipe.Revision == "" {
		t.Fatal("applied generation did not pin its profile and content revision", err)
	}
	cfg := generatedDocument(t, a)
	nodes := cfg["proxies"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["name"] != "node-one" {
		t.Fatal("full profile did not retain its nodes")
	}
	if !reflect.DeepEqual(cfg["rules"], []any{"DOMAIN,custom.invalid,REJECT", "MATCH,PROXY"}) {
		t.Fatal("full profile and custom rules were not composed")
	}
	pinned := m.Build.fingerprint()
	_, appliedPath, _ := a.paths()
	before := read(t, appliedPath)
	write(t, path, []byte("proxies: [{name: node-one, type: socks5, server: updated.invalid, port: 2080}]\nproxy-groups: [{name: PROXY, type: select, proxies: [node-one, DIRECT]}]\nrules: ['MATCH,PROXY']\n"))
	if err := a.Refresh(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	info, err = a.ProfileDetails(t.Context(), "saved")
	if err != nil || !info.Pending {
		t.Fatal("refresh did not expose the unapplied revision", err)
	}
	lib, _, err := a.library().Load()
	if err != nil || lib.Profiles["saved"].Spec.Overrides != overrides {
		t.Fatal("refresh replaced the user's rules", err)
	}
	nextManifest, current, err := a.load()
	if err != nil || current != applied || nextManifest.Build.fingerprint() != pinned || !bytes.Equal(read(t, appliedPath), before) || len(svc.actions) != 0 {
		t.Fatal("refresh changed the committed generation or service", err)
	}
	svc.active = true
	failed := false
	svc.actionHook = func(context.Context, string) error {
		if !failed {
			failed = true
			return errors.New("new configuration failed to start")
		}
		return nil
	}
	if err := a.Apply(t.Context(), "saved"); err == nil {
		t.Fatal("failed apply was reported as successful")
	}
	restored, restoredGeneration, err := a.load()
	if err != nil || restoredGeneration != applied || restored.Build.fingerprint() != pinned || !bytes.Equal(read(t, appliedPath), before) {
		t.Fatal("failed apply did not restore the old pinned configuration", err)
	}
	info, err = a.ProfileDetails(t.Context(), "saved")
	if err != nil || !info.Pending {
		t.Fatal("rollback discarded the saved update", err)
	}
	svc.active = false
	svc.actionHook = nil
	if err := a.Apply(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	info, err = a.ProfileDetails(t.Context(), "saved")
	if err != nil || info.Pending || !strings.Contains(string(read(t, appliedPath)), "updated.invalid") {
		t.Fatal("explicit retry did not commit the pending revision", err)
	}
}

func TestProfileImportRejectsDanglingRoutingBeforeSaving(t *testing.T) {
	a, svc := testApp(t)
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	write(t, path, []byte("proxies: [{name: node, type: direct}]\nproxy-groups: [{name: supplier, type: select, proxies: [absent]}]\nrules: ['MATCH,supplier']\n"))
	if err := a.AddSubscription(t.Context(), "invalid", path); err == nil {
		t.Fatal("profile accepted a dangling outbound")
	}
	assertLibraryEntryMissing(t, a, "invalid")
	if svc.installs != 0 || len(svc.actions) != 0 {
		t.Fatal("rejected import affected the service")
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

func TestProfileDeletionProtectsSelectionAndHistoricalInputs(t *testing.T) {
	a, svc := installedApp(t)
	for _, name := range []string{"primary", "secondary"} {
		addFixtureProfile(t, a, name)
	}
	if err := a.Apply(t.Context(), "primary"); err != nil {
		t.Fatal(err)
	}
	snapshotPath := savedSourcePath(t, a, "primary")
	snapshot := read(t, snapshotPath)
	if err := a.DeleteProfile(t.Context(), "primary"); err == nil {
		t.Fatal("deleted selected profile")
	}
	if err := a.DeleteProfileWithReplacement(t.Context(), "primary", "secondary"); err != nil {
		t.Fatal(err)
	}
	lib, _, err := a.library().Load()
	if err != nil || len(lib.Profiles) != 1 || lib.Profiles["secondary"].Name != "secondary" {
		t.Fatal("deleting one profile changed another profile", err)
	}
	if err := a.DeleteProfileWithReplacement(t.Context(), "secondary", "default"); err != nil {
		t.Fatal(err)
	}
	m, _, err := a.load()
	if err != nil || m.selectedProfile() != "" {
		t.Fatal("deleting the last selected profile left a dangling selection", err)
	}
	lib, raw, err := a.library().Load()
	if err != nil || len(lib.Profiles) != 0 || bytes.Contains(raw, []byte("\"subscriptions\"")) || !bytes.Equal(snapshot, read(t, snapshotPath)) || len(svc.actions) != 0 {
		t.Fatal("deletion left source metadata or changed historical snapshots or service state", err)
	}
	if err := a.DeleteProfile(t.Context(), "default"); err == nil {
		t.Fatal("deleted the reserved default profile")
	}
}

func TestDeleteSelectedProfileUsesReviewedTargetAndReplacement(t *testing.T) {
	a, svc := installedApp(t)
	for _, id := range []string{"current", "next", "other"} {
		addFixtureProfile(t, a, id)
	}
	if err := a.Apply(t.Context(), "current"); err != nil {
		t.Fatal(err)
	}
	// A stale card ID must not resolve to another profile's display name.
	if err := a.RenameProfile(t.Context(), "other", "vanished"); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"current", "current"}, {"current", "vanished"}, {"vanished", ""}, {"next", "default"}, {"current", ""}} {
		if err := a.DeleteProfileWithReplacement(t.Context(), pair[0], pair[1]); err == nil {
			t.Fatalf("accepted stale or incomplete delete %q", pair)
		}
	}
	if err := a.SetProfileOverrides(t.Context(), "next", []byte("prepend-rules: ['DOMAIN,next.example,DIRECT']\n")); err != nil {
		t.Fatal(err)
	}
	svc.active = true
	svc.actionHook = func(_ context.Context, action string) error {
		if action != "restart" {
			t.Fatalf("unexpected service action %s", action)
		}
		lib, _, err := a.library().Load()
		if _, ok := lib.Profiles["current"]; err != nil || !ok {
			t.Fatal("deleted current profile before the replacement was ready", err)
		}
		return nil
	}
	if err := a.DeleteProfileWithReplacement(t.Context(), "current", "next"); err != nil {
		t.Fatal(err)
	}
	status, err := a.Snapshot(t.Context())
	if err != nil || status.Applied != "next" || !status.Service.Active || len(status.Profiles) != 2 || len(svc.actions) != 1 {
		t.Fatalf("unexpected state after replacement: %+v, %v", status, err)
	}
	_, cfg, _ := a.paths()
	if !strings.Contains(string(read(t, cfg)), "next.example") {
		t.Fatal("replacement did not use the saved rules")
	}
}

func TestDeleteSelectedProfileFailurePreservesRecord(t *testing.T) {
	for _, failure := range []string{"validation", "restart", "external-edit"} {
		t.Run(failure, func(t *testing.T) {
			a, svc := installedApp(t)
			addFixtureProfile(t, a, "current")
			if err := a.Apply(t.Context(), "current"); err != nil {
				t.Fatal(err)
			}
			svc.active = true
			switch failure {
			case "validation":
				a.Validate = func(context.Context, string, string, string) error { return errors.New("validation failed") }
			case "restart":
				svc.actionHook = func(context.Context, string) error {
					if len(svc.actions) == 1 {
						return errors.New("restart failed")
					}
					return nil
				}
			case "external-edit":
				a.Validate = func(context.Context, string, string, string) error {
					lib, previous, err := a.library().Load()
					if err != nil {
						return err
					}
					profile := lib.Profiles["current"]
					profile.Description = "external change"
					lib.Profiles["current"] = profile
					return a.library().Save(lib, previous)
				}
			}
			err := a.DeleteProfileWithReplacement(t.Context(), "current", "default")
			if err == nil {
				t.Fatal("unexpected deletion")
			}
			status, readErr := a.Snapshot(t.Context())
			if readErr != nil || !status.Service.Active || len(status.Profiles) != 1 || status.Profiles[0] != "current" {
				t.Fatalf("failed delete lost the profile or stopped the service: %+v, %v", status, readErr)
			}
			if failure == "external-edit" {
				if status.Applied != "" || status.ProfileDetails[0].Description != "external change" || !strings.Contains(err.Error(), "selection changed") {
					t.Fatal("partial success hid the new selection or discarded the updated record", status, err)
				}
			} else if status.Applied != "current" {
				t.Fatal("failed replacement changed the selected profile")
			}
		})
	}
}

func TestDeactivateProfileOnlyChangesTheExpectedSelection(t *testing.T) {
	a, svc := installedApp(t)
	addFixtureProfile(t, a, "selected")
	if err := a.Apply(t.Context(), "selected"); err != nil {
		t.Fatal(err)
	}
	_, selectedGeneration, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DeactivateProfile(t.Context(), "old-selection"); err == nil {
		t.Fatal("a stale card deactivated another profile")
	}
	m, target, err := a.load()
	if err != nil || m.Applied != "selected" || target != selectedGeneration {
		t.Fatal("stale deactivation changed the generation", err)
	}
	if err := a.DeactivateProfile(t.Context(), "selected"); err != nil {
		t.Fatal(err)
	}
	m, target, err = a.load()
	if err != nil || m.selectedProfile() != "" || selectedGeneration == target || len(svc.actions) != 0 {
		t.Fatal("deactivation did not preserve the stopped instance", err)
	}
	if err := a.DeactivateProfile(t.Context(), "selected"); err != nil {
		t.Fatal(err)
	}
	_, retriedTarget, err := a.load()
	if err != nil || retriedTarget != target {
		t.Fatal("retry regenerated the already deactivated profile", err)
	}
}
