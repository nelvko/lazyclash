package app

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nelvko/lazyclash/internal/configuration"
)

func TestCreateHTTPSProfileKeepsMetadataAndRunningGenerationSeparate(t *testing.T) {
	for _, tc := range []struct{ name, explicit, want, source string }{
		{name: "automatic", want: "机场 HTTPS", source: configuration.NameProvider},
		{name: "custom", explicit: "我的配置", want: "我的配置", source: configuration.NameCustom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, svc := installedApp(t)
			svc.active = true
			before, err := installationTree(filepath.Join(a.Home, "installation"))
			if err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int64
			body := []byte("rules: ['MATCH,DIRECT']\n")
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("profile-title", "机场 HTTPS")
				_, _ = w.Write(body)
			}))
			defer server.Close()
			a.HTTP = server.Client()
			address := server.URL + "/download.yaml?token=private-fixture-token"
			if err := a.CreateProfile(t.Context(), ProfileDraft{
				Name: tc.explicit, Description: "  日常\n路由\t说明  ", Address: address, RememberSource: true,
			}); err != nil {
				t.Fatal(err)
			}
			profile := namedProfile(t, a, tc.want)
			requireProfileName(t, a, profile.ID, tc.want, tc.source, tc.want)
			if profile.Description != "日常 路由 说明" || profile.Kind != "remote" || !profile.Refreshable || !profile.Validated || profile.Pending {
				t.Fatalf("remote profile metadata is incomplete: %+v", profile)
			}
			lib, _, err := a.library().Load()
			if err != nil || lib.Profiles[profile.ID].Address != address || lib.Profiles[profile.ID].Spec.Overrides != "" {
				t.Fatalf("remote source or runtime recipe is incorrect: %+v %v", lib.Profiles[profile.ID], err)
			}
			if !bytes.Equal(read(t, savedSourcePath(t, a, profile.ID)), body) || requests.Load() != 1 {
				t.Fatal("creating metadata changed the source or downloaded it more than once")
			}
			after, err := installationTree(filepath.Join(a.Home, "installation"))
			if err != nil || !maps.Equal(before, after) || !svc.active || len(svc.actions) != 0 || svc.installs != 1 {
				t.Fatalf("creating a profile changed the running installation: %v", err)
			}
		})
	}
}

func TestCreateLocalProfileAutomaticallyNamesLiteralFileBeforeInstallation(t *testing.T) {
	a, svc := testApp(t)
	path := filepath.Join(t.TempDir(), "配置 #1? literal%20.yaml")
	body := []byte("rules: ['MATCH,DIRECT']\n")
	write(t, path, body)
	if err := a.CreateProfile(t.Context(), ProfileDraft{
		Description: "  本地\t配置  ", Address: path, RememberSource: true,
	}); err != nil {
		t.Fatal(err)
	}
	profile := namedProfile(t, a, "配置 #1? literal%20")
	if profile.NameSource != configuration.NameURL || profile.NameBase != profile.Name || profile.Description != "本地 配置" || profile.Kind != "local" || !profile.Refreshable || profile.Validated {
		t.Fatalf("local profile metadata is incorrect: %+v", profile)
	}
	lib, _, err := a.library().Load()
	if err != nil || lib.Profiles[profile.ID].Address != path || len(lib.Profiles) != 1 {
		t.Fatalf("local creation did not publish one remembered profile: %+v %v", lib, err)
	}
	snapshotPath := savedSourcePath(t, a, profile.ID)
	if filepath.Base(snapshotPath) != configuration.Hash(body)+".yaml" || !bytes.Equal(read(t, snapshotPath), body) || !bytes.Equal(read(t, path), body) {
		t.Fatal("local creation used the display name as storage or altered its source")
	}
	assertMissing(t, filepath.Join(a.Home, "installation"))
	if svc.installs != 0 || len(svc.actions) != 0 {
		t.Fatal("creating a local profile installed or started the core")
	}
}

func TestFailedProfileCreationPublishesNeitherMetadataNorRuntime(t *testing.T) {
	for _, scenario := range []string{"download failure", "invalid configuration", "core rejection", "cancelled validation"} {
		t.Run(scenario, func(t *testing.T) {
			a, svc := installedApp(t)
			original := filepath.Join(t.TempDir(), "original.yaml")
			write(t, original, []byte("rules: ['MATCH,DIRECT']\n"))
			if err := a.CreateProfile(t.Context(), ProfileDraft{Name: "Original", Description: "Keep this note", Address: original}); err != nil {
				t.Fatal(err)
			}
			svc.active = true
			indexPath := filepath.Join(a.Home, "configuration", "library.json")
			beforeIndex := read(t, indexPath)
			beforeRuntime, err := installationTree(filepath.Join(a.Home, "installation"))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var requests atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("profile-title", "Unpublished candidate")
				if scenario == "download failure" {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
				body := "rules: ['MATCH,REJECT']\n"
				if scenario == "invalid configuration" {
					body = "rules: ["
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			a.HTTP = server.Client()
			switch scenario {
			case "core rejection":
				a.Validate = func(context.Context, string, string, string) error { return errors.New("core rejected candidate") }
			case "cancelled validation":
				a.Validate = func(context.Context, string, string, string) error { cancel(); return context.Canceled }
			}
			err = a.CreateProfile(ctx, ProfileDraft{
				Description: "Unpublished description", Address: server.URL + "/config?token=private-fixture-token", RememberSource: true,
			})
			if err == nil || strings.Contains(err.Error(), "private-fixture-token") || strings.Contains(err.Error(), server.URL) {
				t.Fatalf("failed creation succeeded or exposed its address: %v", err)
			}
			if scenario == "cancelled validation" && !errors.Is(err, context.Canceled) {
				t.Fatalf("creation lost cancellation: %v", err)
			}
			afterRuntime, treeErr := installationTree(filepath.Join(a.Home, "installation"))
			if treeErr != nil || !bytes.Equal(beforeIndex, read(t, indexPath)) || !maps.Equal(beforeRuntime, afterRuntime) || !svc.active || len(svc.actions) != 0 || requests.Load() != 1 {
				t.Fatalf("failed creation changed published inputs or the running generation: %v", treeErr)
			}
		})
	}
}

func TestClearingProfileDescriptionPreservesIdentityRecipeAndRunningGeneration(t *testing.T) {
	a, svc := installedApp(t)
	path := filepath.Join(t.TempDir(), "work.yaml")
	write(t, path, []byte("rules: ['MATCH,DIRECT']\n"))
	if err := a.CreateProfile(t.Context(), ProfileDraft{Name: "Work", Description: "Office routing", Address: path, RememberSource: true}); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "Work"); err != nil {
		t.Fatal(err)
	}
	svc.active = true
	beforeLibrary, _, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	beforeRuntime, err := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil {
		t.Fatal(err)
	}
	a.Validate = func(context.Context, string, string, string) error {
		t.Fatal("clearing description regenerated or validated the runtime configuration")
		return nil
	}
	if err := a.UpdateProfileDescription(t.Context(), "Work", " \t\n "); err != nil {
		t.Fatal(err)
	}
	afterLibrary, _, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	expected := beforeLibrary.Profiles["Work"]
	expected.Description = ""
	if len(afterLibrary.Profiles) != 1 || !reflect.DeepEqual(afterLibrary.Profiles["Work"], expected) {
		t.Fatalf("clearing description changed other metadata or the recipe: %+v", afterLibrary.Profiles)
	}
	profile, err := a.ProfileDetails(t.Context(), "Work")
	if err != nil || profile.ID != "Work" || profile.Description != "" || profile.Pending {
		t.Fatalf("cleared description changed the profile identity or pending state: %+v %v", profile, err)
	}
	afterRuntime, err := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil || !maps.Equal(beforeRuntime, afterRuntime) || !svc.active || len(svc.actions) != 0 {
		t.Fatalf("clearing description changed the running generation: %v", err)
	}
}
