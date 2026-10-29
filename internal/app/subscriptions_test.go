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

	"github.com/nelvko/lazyclash/internal/store"
)

func TestRememberedSubscriptionRefreshPreservesRuntimeAndPrivateSource(t *testing.T) {
	a, svc := installedApp(t)
	a.HTTP = &http.Client{}
	content := "mode: rule\nrules: [MATCH,DIRECT]\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "private-subscription-token" {
			t.Error("remembered source lost its credentials")
		}
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()
	source := server.URL + "/profile?token=private-subscription-token"
	if err := a.AddSubscription(t.Context(), "remote", source); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "remote"); err != nil {
		t.Fatal(err)
	}
	svc.active = true
	before, _ := installationTree(filepath.Join(a.Home, "installation"))
	content = "mode: direct\nrules: [MATCH,REJECT]\n"
	if err := a.Refresh(t.Context(), "remote"); err != nil {
		t.Fatal(err)
	}
	after, _ := installationTree(filepath.Join(a.Home, "installation"))
	if !maps.Equal(before, after) || !svc.active || len(svc.actions) != 0 {
		t.Fatal("refresh applied a profile or affected the running service")
	}
	profile := filepath.Join(a.Home, "profiles", "remote.yaml")
	metadataPath := filepath.Join(a.Home, "profiles", "remote.source.json")
	if string(read(t, profile)) != content {
		t.Fatal("new subscription bytes were not saved")
	}
	var metadata subscriptionSource
	if json.Unmarshal(read(t, metadataPath), &metadata) != nil || metadata.Source != source || metadata.SHA != digest([]byte(content)) {
		t.Fatal("private source metadata does not match the refreshed profile")
	}
	for _, path := range []string{profile, metadataPath} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("subscription files are not private")
		}
	}
}

func TestUnrememberedSourceIsNotStored(t *testing.T) {
	a, _ := installedApp(t)
	source := filepath.Join(t.TempDir(), "source.yaml")
	write(t, source, []byte("rules: [MATCH,DIRECT]\n"))
	if err := a.Add(t.Context(), "local", source); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, filepath.Join(a.Home, "profiles", "local.source.json"))
	if err := a.Refresh(t.Context(), "local"); err == nil || !strings.Contains(err.Error(), "remember") {
		t.Fatal("refresh silently assumed a source", err)
	}
}

func TestSubscriptionRefreshProtectsLocalEditsAndInterruptedMetadata(t *testing.T) {
	for _, scenario := range []string{"local-edit", "interrupted-metadata", "validation-failure", "changed-during-validation", "cancel-validation"} {
		t.Run(scenario, func(t *testing.T) {
			a, svc := installedApp(t)
			source := filepath.Join(t.TempDir(), "source.yaml")
			original := []byte("rules: [MATCH,DIRECT]\n")
			write(t, source, original)
			if err := a.AddSubscription(t.Context(), "saved", source); err != nil {
				t.Fatal(err)
			}
			profile := filepath.Join(a.Home, "profiles", "saved.yaml")
			metadataPath := filepath.Join(a.Home, "profiles", "saved.source.json")
			metadataBefore := read(t, metadataPath)
			write(t, source, []byte("rules: [MATCH,REJECT]\n"))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "local-edit":
				write(t, profile, []byte("# user edit\nrules: [MATCH,DIRECT]\n"))
			case "interrupted-metadata":
				var metadata subscriptionSource
				_ = json.Unmarshal(metadataBefore, &metadata)
				metadata.SHA = digest([]byte("future-profile"))
				updated, _ := json.Marshal(metadata)
				write(t, metadataPath, updated)
			case "validation-failure":
				a.Validate = func(context.Context, string, string, string) error { return errors.New("engine rejected subscription") }
			case "changed-during-validation":
				a.Validate = func(context.Context, string, string, string) error {
					write(t, profile, []byte("# concurrent user edit\nrules: [MATCH,DIRECT]\n"))
					return nil
				}
			case "cancel-validation":
				a.Validate = func(context.Context, string, string, string) error { cancel(); return context.Canceled }
			}
			before := read(t, profile)
			metaExpected := read(t, metadataPath)
			if err := a.Refresh(ctx, "saved"); err == nil {
				t.Fatal("unsafe refresh reported success")
			}
			if scenario != "changed-during-validation" && string(read(t, profile)) != string(before) {
				t.Fatal("failed refresh changed the saved profile")
			}
			if scenario == "changed-during-validation" && !strings.Contains(string(read(t, profile)), "concurrent user edit") {
				t.Fatal("refresh overwrote a concurrent local edit")
			}
			if string(read(t, metadataPath)) != string(metaExpected) || len(svc.actions) != 0 {
				t.Fatal("failed refresh changed remembered metadata or controlled the service")
			}
		})
	}
}

func TestSubscriptionRefreshBeforeInstallationRemainsUnvalidated(t *testing.T) {
	a, _ := testApp(t)
	source := filepath.Join(t.TempDir(), "source.yaml")
	write(t, source, []byte("rules: [MATCH,DIRECT]\n"))
	if err := a.AddSubscription(t.Context(), "saved", source); err != nil {
		t.Fatal(err)
	}
	next := []byte("rules: [MATCH,REJECT]\n")
	write(t, source, next)
	if err := a.Refresh(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(a.Home, "profiles", "saved.unvalidated")
	if string(read(t, marker)) != digest(next) {
		t.Fatal("structurally validated refresh lost its full-validation marker")
	}
	if err := a.Install(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.Refresh(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, marker)
}

func TestSubscriptionErrorsWithholdSourceCredentials(t *testing.T) {
	a, _ := installedApp(t)
	a.HTTP = &http.Client{}
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("rules: [MATCH,DIRECT]\n"))
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

func TestProfileWriteRollbackPreservesChangedFiles(t *testing.T) {
	a, _ := testApp(t)
	if err := store.Dir(filepath.Join(a.Home, "profiles")); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(a.Home, "profiles", "saved.yaml")
	write(t, profile, []byte("user-profile"))
	metadata := subscriptionSource{Source: "/private/source", SHA: digest([]byte("new"))}
	if err := a.writeProfileLocked("saved", []byte("new"), nil, &metadata, nil, true); err == nil {
		t.Fatal("existing profile was overwritten")
	}
	if string(read(t, profile)) != "user-profile" {
		t.Fatal("existing profile was changed")
	}
	assertMissing(t, filepath.Join(a.Home, "profiles", "saved.source.json"))
	assertMissing(t, filepath.Join(a.Home, "profiles", "saved.unvalidated"))
}
