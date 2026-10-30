package app

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRememberedSubscriptionRefreshPreservesRuntimeAndOverrides(t *testing.T) {
	a, svc := installedApp(t)
	a.HTTP = &http.Client{}
	content := "rules: ['MATCH,DIRECT']\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "private-subscription-token" {
			t.Error("source lost its credentials")
		}
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()
	source := server.URL + "/profile?token=private-subscription-token"
	if err := a.AddSubscription(t.Context(), "remote", source); err != nil {
		t.Fatal(err)
	}
	custom := []byte("prepend-rules: ['DOMAIN,custom.example,REJECT']\n")
	if err := a.SetProfileOverrides(t.Context(), "remote", custom); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "remote"); err != nil {
		t.Fatal(err)
	}
	svc.active = true
	before, _ := installationTree(filepath.Join(a.Home, "installation"))
	previous, _, _ := a.library().Load()
	content = "mode: direct\nrules: ['MATCH,REJECT']\n"
	if err := a.Refresh(t.Context(), "remote"); err != nil {
		t.Fatal(err)
	}
	after, _ := installationTree(filepath.Join(a.Home, "installation"))
	if !maps.Equal(before, after) || !svc.active || len(svc.actions) != 0 {
		t.Fatal("refresh changed running generation or service")
	}
	lib, _, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	metadata := lib.Profiles["remote"]
	raw, err := a.library().Read(metadata.Spec.Revision)
	if err != nil || string(raw) != content || metadata.Address != source || metadata.Spec.Revision != digest(raw) {
		t.Fatal("snapshot metadata mismatch", err)
	}
	if lib.Profiles["remote"].Spec.Overrides != string(custom) {
		t.Fatal("refresh overwrote user rules")
	}
	old, err := a.library().Read(previous.Profiles["remote"].Spec.Revision)
	if err != nil || string(old) != "rules: ['MATCH,DIRECT']\n" {
		t.Fatal("previous snapshot lost", err)
	}
	info, err := a.ProfileDetails(t.Context(), "remote")
	if err != nil || !info.Pending {
		t.Fatal("source change not shown as pending", info, err)
	}
	for _, path := range []string{filepath.Join(a.Home, "configuration", "library.json"), filepath.Join(a.Home, "configuration", "snapshots", metadata.Spec.Revision+".yaml")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("subscription files are not private", err)
		}
	}
	public, err := a.ListProfiles(t.Context())
	encoded, _ := json.Marshal(public)
	if err != nil || strings.Contains(string(encoded), "private-subscription-token") || strings.Contains(string(encoded), server.URL) {
		t.Fatal("public source listing exposed credentials", err)
	}
}

func TestOneTimeImportDoesNotRememberAddress(t *testing.T) {
	a, _ := installedApp(t)
	source := filepath.Join(t.TempDir(), "source.yaml")
	write(t, source, []byte("rules: ['MATCH,DIRECT']\n"))
	if err := a.Add(t.Context(), "local", source); err != nil {
		t.Fatal(err)
	}
	lib, _, _ := a.library().Load()
	if lib.Profiles["local"].Address != "" {
		t.Fatal("address remembered without opt-in")
	}
	if err := a.Refresh(t.Context(), "local"); err == nil || !strings.Contains(err.Error(), "remembered") {
		t.Fatal("refresh assumed a source", err)
	}
}

func TestRefreshFailureNeverCommitsCandidateIndexOrControlsService(t *testing.T) {
	for _, scenario := range []string{"corrupt-snapshot", "invalid-yaml", "validation-failure", "concurrent-index-edit", "cancel-validation", "override-reference"} {
		t.Run(scenario, func(t *testing.T) {
			a, svc := installedApp(t)
			source := filepath.Join(t.TempDir(), "source.yaml")
			original := []byte("proxy-groups: [{name: ROUTE, type: select, proxies: [DIRECT]}]\nrules: ['MATCH,ROUTE']\n")
			write(t, source, original)
			if err := a.AddSubscription(t.Context(), "saved", source); err != nil {
				t.Fatal(err)
			}
			index := filepath.Join(a.Home, "configuration", "library.json")
			before := read(t, index)
			write(t, source, []byte("rules: ['MATCH,REJECT']\n"))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "corrupt-snapshot":
				lib, _, _ := a.library().Load()
				write(t, filepath.Join(a.Home, "configuration", "snapshots", lib.Profiles["saved"].Spec.Revision+".yaml"), []byte("# manual corruption\nrules: ['MATCH,DIRECT']\n"))
			case "invalid-yaml":
				write(t, source, []byte("sensitive-token: ["))
			case "validation-failure":
				a.Validate = func(context.Context, string, string, string) error { return errors.New("engine rejected candidate") }
			case "concurrent-index-edit":
				a.Validate = func(context.Context, string, string, string) error { write(t, index, append(before, ' ')); return nil }
			case "cancel-validation":
				a.Validate = func(context.Context, string, string, string) error { cancel(); return context.Canceled }
			case "override-reference":
				if err := a.SetProfileOverrides(t.Context(), "saved", []byte("prepend-rules: ['DOMAIN,custom.example,ROUTE']")); err != nil {
					t.Fatal(err)
				}
				before = read(t, index)
			}
			if err := a.Refresh(ctx, "saved"); err == nil {
				t.Fatal("unsafe refresh succeeded")
			}
			want := string(before)
			if scenario == "concurrent-index-edit" {
				want += " "
			}
			if string(read(t, index)) != want || len(svc.actions) != 0 {
				t.Fatal("failed refresh changed published index or service")
			}
			assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
		})
	}
}

func TestRefreshBeforeInstallationDoesNotClaimEngineValidation(t *testing.T) {
	a, _ := testApp(t)
	source := filepath.Join(t.TempDir(), "source.yaml")
	write(t, source, []byte("rules: ['MATCH,DIRECT']\n"))
	if err := a.AddSubscription(t.Context(), "saved", source); err != nil {
		t.Fatal(err)
	}
	write(t, source, []byte("rules: ['MATCH,REJECT']\n"))
	if err := a.Refresh(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	lib, _, _ := a.library().Load()
	if lib.Profiles["saved"].Validated {
		t.Fatal("claimed engine validation without an installed core")
	}
	if err := a.Install(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.Refresh(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	lib, _, _ = a.library().Load()
	if !lib.Profiles["saved"].Validated {
		t.Fatal("successful engine validation was not recorded")
	}
}

func TestSubscriptionErrorsWithholdSourceCredentials(t *testing.T) {
	a, _ := installedApp(t)
	a.HTTP = &http.Client{}
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("rules: ['MATCH,DIRECT']\n"))
	}))
	defer server.Close()
	if err := a.AddSubscription(t.Context(), "saved", server.URL+"/?secret=private-token"); err != nil {
		t.Fatal(err)
	}
	status = 500
	if err := a.Refresh(t.Context(), "saved"); err == nil || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), server.URL) {
		t.Fatal("unsafe subscription error", err)
	}
}

func TestProfileRefreshValidatesItsOverridesWithoutChangingOtherProfiles(t *testing.T) {
	a, _ := installedApp(t)
	path := filepath.Join(t.TempDir(), "profile.yaml")
	write(t, path, []byte("proxies: [{name: NODE, type: direct}]\nrules: ['MATCH,DIRECT']"))
	for _, name := range []string{"custom", "other"} {
		if err := a.AddSubscription(t.Context(), name, path); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.SetProfileOverrides(t.Context(), "custom", []byte("prepend-rules: ['DOMAIN,custom.example,NODE']")); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(a.Home, "configuration", "library.json")
	before := read(t, index)
	write(t, path, []byte("proxies: [{name: OTHER, type: direct}]\nrules: ['MATCH,DIRECT']"))
	if err := a.Refresh(t.Context(), "custom"); err == nil {
		t.Fatal("refresh invalidated custom rules")
	}
	if string(read(t, index)) != string(before) {
		t.Fatal("failed refresh changed a saved profile")
	}
	if err := a.Refresh(t.Context(), "other"); err != nil {
		t.Fatal("another profile's overrides blocked this independent update", err)
	}
	lib, _, err := a.library().Load()
	if err != nil || lib.Profiles["custom"].Spec.Revision == lib.Profiles["other"].Spec.Revision {
		t.Fatal("independent profile refresh changed both revisions", err)
	}
}
