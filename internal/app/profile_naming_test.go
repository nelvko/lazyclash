package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/nelvko/lazyclash/internal/configuration"
)

type namingResponse struct {
	title, disposition, body string
	status                   int
}

func namingServer(t *testing.T, initial namingResponse) (*httptest.Server, func(namingResponse), *atomic.Int64) {
	t.Helper()
	var state atomic.Value
	state.Store(initial)
	requests := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		response := state.Load().(namingResponse)
		if response.title != "" {
			w.Header().Set("profile-title", response.title)
		}
		if response.disposition != "" {
			w.Header().Set("Content-Disposition", response.disposition)
		}
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response.body))
	}))
	t.Cleanup(server.Close)
	return server, func(next namingResponse) { state.Store(next) }, requests
}

func namedProfile(t *testing.T, a *App, name string) ProfileInfo {
	t.Helper()
	profiles, err := a.ListProfiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range profiles {
		if profile.Name == name {
			if profile.ID == "" {
				t.Fatal("profile listing omitted its stable ID")
			}
			return profile
		}
	}
	t.Fatalf("profile %q not found in %+v", name, profiles)
	return ProfileInfo{}
}

func requireProfileName(t *testing.T, a *App, id, name, source, base string) ProfileInfo {
	t.Helper()
	profile, err := a.ProfileDetails(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID != id || profile.Name != name || profile.NameSource != source || profile.NameBase != base {
		t.Fatalf("unexpected name metadata for %s: %+v; want name=%q source=%q base=%q", id, profile, name, source, base)
	}
	return profile
}

func TestAutomaticProfileNamesShareTheDownloadResponse(t *testing.T) {
	for _, tc := range []struct {
		name, explicit, title, disposition, path, expected, source string
	}{
		{name: "custom wins", explicit: "我的机场", title: "Provider", disposition: `attachment; filename="download.yaml"`, path: "/url.yaml", expected: "我的机场", source: "custom"},
		{name: "UTF-8 title", title: "机场 A · 香港", disposition: `attachment; filename="download.yaml"`, path: "/url.yaml", expected: "机场 A · 香港", source: "provider"},
		{name: "base64 title", title: "base64:" + base64.StdEncoding.EncodeToString([]byte("机场 B")), path: "/url.yaml", expected: "机场 B", source: "provider"},
		{name: "extended filename", disposition: `attachment; filename="old.yaml"; filename*=UTF-8''%E6%9C%BA%E5%9C%BA%20C.yaml`, path: "/url.yaml", expected: "机场 C.yaml", source: "provider"},
		{name: "legacy filename", disposition: `attachment; filename="Airport D.yaml"`, path: "/url.yaml", expected: "Airport D.yaml", source: "provider"},
		{name: "invalid title falls back", title: "base64:not-base64!", disposition: `attachment; filename="Working.yaml"`, path: "/url.yaml", expected: "Working.yaml", source: "provider"},
		{name: "empty headers use URL", title: "   ", disposition: `attachment; filename=" "`, path: "/folder/%E8%8A%82%E7%82%B9.yaml///?token=not-a-name#not-a-name", expected: "节点", source: "url"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, svc := testApp(t)
			body := "rules: ['MATCH,DIRECT']\n"
			server, _, requests := namingServer(t, namingResponse{title: tc.title, disposition: tc.disposition, body: body})
			a.HTTP = server.Client()
			if err := a.AddSubscription(t.Context(), tc.explicit, server.URL+tc.path); err != nil {
				t.Fatal("name metadata prevented a valid subscription import", err)
			}
			profile := namedProfile(t, a, tc.expected)
			requireProfileName(t, a, profile.ID, tc.expected, tc.source, tc.expected)
			if requests.Load() != 1 || len(svc.actions) != 0 || svc.installs != 0 {
				t.Fatal("name discovery downloaded twice or changed core lifecycle")
			}
			if !profile.Refreshable {
				t.Fatal("remembered profile is not refreshable")
			}
			path := savedSourcePath(t, a, profile.ID)
			if filepath.Base(path) != configuration.Hash([]byte(body))+".yaml" || !bytes.Equal(read(t, path), []byte(body)) {
				t.Fatal("provider filename changed the content-addressed source path or bytes")
			}
		})
	}
}

func TestAutomaticProfileNameLifecycleAndCustomProtection(t *testing.T) {
	a, _ := testApp(t)
	body := "rules: ['MATCH,DIRECT']\n"
	server, update, requests := namingServer(t, namingResponse{body: body})
	a.HTTP = server.Client()
	address := server.URL + "/old-name.yaml?token=private#fragment"
	if err := a.AddSubscription(t.Context(), "", address); err != nil {
		t.Fatal(err)
	}
	profile := namedProfile(t, a, "old-name")
	id := profile.ID
	requireProfileName(t, a, id, "old-name", "url", "old-name")
	for _, name := range []string{"机场 A", "机场 B"} {
		update(namingResponse{title: name, body: body})
		if err := a.Refresh(t.Context(), profile.ID); err != nil {
			t.Fatal(err)
		}
		requireProfileName(t, a, id, name, "provider", name)
	}
	update(namingResponse{body: body})
	if err := a.Refresh(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	requireProfileName(t, a, id, "机场 B", "provider", "机场 B")
	if err := a.RenameProfile(t.Context(), id, "我的专用配置"); err != nil {
		t.Fatal(err)
	}
	update(namingResponse{title: "服务端新名称", body: body})
	if err := a.Refresh(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	requireProfileName(t, a, id, "我的专用配置", "custom", "我的专用配置")
	if requests.Load() != 5 {
		t.Fatalf("add and four refreshes should issue five downloads, got %d", requests.Load())
	}
	byName, err := a.ProfileDetails(t.Context(), "我的专用配置")
	if err != nil || byName.ID != id {
		t.Fatal("unique display name did not resolve to the stable profile", err)
	}
}

func TestDuplicateAutomaticProfileNamesDoNotAccumulateSuffixes(t *testing.T) {
	a, _ := testApp(t)
	server, update, _ := namingServer(t, namingResponse{title: "机场 A", body: "rules: ['MATCH,DIRECT']\n"})
	a.HTTP = server.Client()
	for range 2 {
		if err := a.AddSubscription(t.Context(), "", server.URL+"/subscription"); err != nil {
			t.Fatal(err)
		}
	}
	first, second := namedProfile(t, a, "机场 A"), namedProfile(t, a, "机场 A (1)")
	if first.ID == second.ID {
		t.Fatal("duplicate labels reused the same profile ID")
	}
	for range 3 {
		if err := a.Refresh(t.Context(), second.ID); err != nil {
			t.Fatal(err)
		}
		requireProfileName(t, a, second.ID, "机场 A (1)", "provider", "机场 A")
	}
	update(namingResponse{title: "机场 B", body: "rules: ['MATCH,DIRECT']\n"})
	if err := a.Refresh(t.Context(), second.ID); err != nil {
		t.Fatal(err)
	}
	requireProfileName(t, a, second.ID, "机场 B", "provider", "机场 B")
	if err := a.RenameProfile(t.Context(), second.ID, "机场 A"); err != nil {
		t.Fatal(err)
	}
	requireProfileName(t, a, second.ID, "机场 A (1)", "custom", "机场 A")
	if err := a.DeleteProfile(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	update(namingResponse{title: "机场 Z", body: "rules: ['MATCH,DIRECT']\n"})
	if err := a.Refresh(t.Context(), second.ID); err != nil {
		t.Fatal(err)
	}
	requireProfileName(t, a, second.ID, "机场 A (1)", "custom", "机场 A")
}

func TestProfileRenameKeepsAppliedIdentityAndFilePaths(t *testing.T) {
	a, svc := installedApp(t)
	server, _, _ := namingServer(t, namingResponse{title: "原始机场", body: "rules: ['MATCH,DIRECT']\n"})
	a.HTTP = server.Client()
	if err := a.AddSubscription(t.Context(), "", server.URL+"/profile.yaml"); err != nil {
		t.Fatal(err)
	}
	profile := namedProfile(t, a, "原始机场")
	if err := a.Apply(t.Context(), profile.ID); err != nil {
		t.Fatal(err)
	}
	svc.active = true
	beforeManifest, beforeGeneration, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	beforeFiles, err := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := savedSourcePath(t, a, profile.ID)
	sourceBytes := read(t, sourcePath)
	a.Validate = func(context.Context, string, string, string) error {
		t.Fatal("metadata rename unnecessarily validated or regenerated a core configuration")
		return nil
	}
	if err := a.RenameProfile(t.Context(), profile.ID, "重命名后的中文配置"); err != nil {
		t.Fatal(err)
	}
	afterManifest, afterGeneration, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	afterFiles, err := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil || !maps.Equal(beforeFiles, afterFiles) || beforeGeneration != afterGeneration || beforeManifest.Build.fingerprint() != afterManifest.Build.fingerprint() {
		t.Fatal("display rename changed runtime generation files, paths, or pinned inputs", err)
	}
	after := requireProfileName(t, a, profile.ID, "重命名后的中文配置", "custom", "重命名后的中文配置")
	if after.Pending || savedSourcePath(t, a, profile.ID) != sourcePath || !bytes.Equal(read(t, sourcePath), sourceBytes) || len(svc.actions) != 0 {
		t.Fatal("display rename affected the applied state or immutable source path")
	}
	snapshot, err := a.Snapshot(t.Context())
	if err != nil || snapshot.Applied != profile.ID || snapshot.AppliedName != after.Name {
		t.Fatal("snapshot confused applied ID and display name", err)
	}
}

func TestProfileNameAndSourceRefreshFailuresCommitNeither(t *testing.T) {
	for _, scenario := range []string{"HTTP failure", "invalid YAML", "engine rejection", "cancelled validation"} {
		t.Run(scenario, func(t *testing.T) {
			a, svc := installedApp(t)
			server, update, _ := namingServer(t, namingResponse{title: "可用名称", body: "rules: ['MATCH,DIRECT']\n"})
			a.HTTP = server.Client()
			if err := a.AddSubscription(t.Context(), "", server.URL+"/profile.yaml"); err != nil {
				t.Fatal(err)
			}
			profile := namedProfile(t, a, "可用名称")
			if err := a.Apply(t.Context(), profile.ID); err != nil {
				t.Fatal(err)
			}
			before := read(t, filepath.Join(a.Home, "configuration", "library.json"))
			beforeFiles, _ := installationTree(filepath.Join(a.Home, "installation"))
			response := namingResponse{title: "不应发布的名称", body: "rules: ['MATCH,REJECT']\n"}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "HTTP failure":
				response.status = http.StatusServiceUnavailable
			case "invalid YAML":
				response.body = "rules: ["
			case "engine rejection":
				a.Validate = func(context.Context, string, string, string) error { return errors.New("rejected fixture") }
			case "cancelled validation":
				a.Validate = func(context.Context, string, string, string) error { cancel(); return context.Canceled }
			}
			update(response)
			if err := a.Refresh(ctx, profile.ID); err == nil {
				t.Fatal("failed candidate was reported as refreshed")
			}
			afterFiles, _ := installationTree(filepath.Join(a.Home, "installation"))
			if !bytes.Equal(before, read(t, filepath.Join(a.Home, "configuration", "library.json"))) || !maps.Equal(beforeFiles, afterFiles) || len(svc.actions) != 0 {
				t.Fatal("failed refresh partially committed its name, source revision or runtime")
			}
			requireProfileName(t, a, profile.ID, "可用名称", "provider", "可用名称")
		})
	}
}

func TestProfileAddressChangesRespectNameOwnershipAndOtherProfiles(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "custom"}[custom], func(t *testing.T) {
			a, svc := testApp(t)
			server, update, requests := namingServer(t, namingResponse{title: "旧机场", body: "proxies: [{name: NODE, type: direct}]\nrules: ['MATCH,DIRECT']\n"})
			a.HTTP = server.Client()
			explicit := ""
			if custom {
				explicit = "保留的自定义名称"
			}
			if err := a.AddSubscription(t.Context(), explicit, server.URL+"/old.yaml"); err != nil {
				t.Fatal(err)
			}
			oldName := "旧机场"
			if custom {
				oldName = explicit
			}
			profile := namedProfile(t, a, oldName)
			if err := a.AddSubscription(t.Context(), "other", server.URL+"/old.yaml"); err != nil {
				t.Fatal(err)
			}
			before, _, err := a.library().Load()
			if err != nil {
				t.Fatal(err)
			}
			update(namingResponse{body: "rules: ['MATCH,REJECT']\n"})
			address := server.URL + "/folder/" + url.PathEscape("新机场.yaml") + "?token=withheld#ignored"
			if err := a.UpdateProfileAddress(t.Context(), profile.ID, address); err != nil {
				t.Fatal(err)
			}
			name, source := "新机场", "url"
			if custom {
				name, source = explicit, "custom"
			}
			afterProfile := requireProfileName(t, a, profile.ID, name, source, name)
			after, _, err := a.library().Load()
			if err != nil {
				t.Fatal(err)
			}
			if afterProfile.ID != profile.ID || !reflect.DeepEqual(after.Profiles["other"], before.Profiles["other"]) {
				t.Fatal("editing one profile's address changed its ID or another profile")
			}
			if after.Profiles[profile.ID].Address != address || requests.Load() != 3 || svc.installs != 0 || len(svc.actions) != 0 {
				t.Fatal("address edit downloaded twice, lost its address, or affected core lifecycle")
			}
			index := filepath.Join(a.Home, "configuration", "library.json")
			beforeFailure := read(t, index)
			update(namingResponse{title: "不应覆盖", status: http.StatusServiceUnavailable})
			if err := a.UpdateProfileAddress(t.Context(), profile.ID, server.URL+"/broken.yaml"); err == nil || !bytes.Equal(read(t, index), beforeFailure) {
				t.Fatal("failed address edit changed the saved address, name, or content revision", err)
			}
		})
	}
}
