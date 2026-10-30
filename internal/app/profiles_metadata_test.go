package app

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProfileSnapshotProvidesSafeCardMetadata(t *testing.T) {
	a, _ := installedApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("rules: ['MATCH,DIRECT']\n"))
	}))
	defer server.Close()
	a.HTTP = server.Client()
	address, err := url.Parse(server.URL + "/private-path-token?token=private-query-token")
	if err != nil {
		t.Fatal(err)
	}
	address.User = url.UserPassword("private-user-token", "private-password-token")
	if err := a.AddSubscription(t.Context(), "remote", address.String()); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "private-local-token.yaml")
	write(t, local, []byte("rules: ['MATCH,DIRECT']\n"))
	if err := a.AddSubscription(t.Context(), "local", local); err != nil {
		t.Fatal(err)
	}
	if err := a.Add(t.Context(), "copied", local); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "remote"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProfileOverrides(t.Context(), "remote", []byte("prepend-rules: ['DOMAIN,private-override-token.invalid,REJECT']\n")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := a.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := a.ListProfiles(t.Context())
	if err != nil || !reflect.DeepEqual(snapshot.ProfileDetails, profiles) {
		t.Fatal("snapshot and profile listing disagree", err)
	}
	if len(profiles) != len(snapshot.Profiles) || profiles[0].Name != "default" || profiles[0].HasOverrides || !profiles[0].Validated {
		t.Fatal("built-in profile metadata is missing or inaccurate")
	}
	for i, profile := range profiles {
		if profile.ID != snapshot.Profiles[i] {
			t.Fatal("profile metadata does not preserve profile order")
		}
		if profile.Name == "remote" && (!profile.HasOverrides || !profile.Pending || !profile.Validated || !snapshot.ProfilePending) {
			t.Fatal("draft override and applied state were not represented", profile)
		}
	}
	for _, profile := range profiles {
		switch profile.ID {
		case "remote":
			if profile.Kind != "remote" || profile.Origin != address.Hostname() || !profile.Refreshable {
				t.Fatal("remote card metadata is inaccurate", profile)
			}
		case "local":
			if profile.Kind != "local" || profile.Origin != "Local file" || !profile.Refreshable {
				t.Fatal("local card metadata is inaccurate", profile)
			}
		case "copied":
			if profile.Kind != "imported" || profile.Origin != "Imported file" || profile.Refreshable {
				t.Fatal("one-time import card metadata is inaccurate", profile)
			}
		}
	}
	public, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-path-token", "private-query-token", "private-user-token", "private-password-token", "private-local-token", "private-override-token", local, address.String()} {
		if strings.Contains(string(public), secret) {
			t.Fatal("card metadata exposed private source or override data")
		}
	}
}

func TestRefreshAllProfilesPreservesIndependentSuccessesAndRunningGeneration(t *testing.T) {
	a, svc := installedApp(t)
	type response struct {
		Content   string
		BadStatus int
	}
	var current atomic.Value
	current.Store(response{Content: "rules: ['MATCH,DIRECT']\n", BadStatus: http.StatusOK})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := current.Load().(response)
		if r.URL.Path == "/bad-private-token" {
			w.WriteHeader(state.BadStatus)
		}
		_, _ = w.Write([]byte(state.Content))
	}))
	defer server.Close()
	a.HTTP = server.Client()
	if err := a.AddSubscription(t.Context(), "a-broken", server.URL+"/bad-private-token?token=query-private-token"); err != nil {
		t.Fatal(err)
	}
	if err := a.AddSubscription(t.Context(), "z-updated", server.URL+"/good-private-token?token=query-private-token"); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "private-local-token.yaml")
	write(t, local, []byte(current.Load().(response).Content))
	if err := a.Add(t.Context(), "copy", local); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "z-updated"); err != nil {
		t.Fatal(err)
	}
	svc.active = true
	before, _, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	beforeTree, err := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil {
		t.Fatal(err)
	}
	good := "rules: ['MATCH,REJECT']\n"
	current.Store(response{Content: good, BadStatus: http.StatusServiceUnavailable})
	write(t, local, []byte(good))
	err = a.RefreshAllProfiles(t.Context())
	if err == nil || !strings.Contains(err.Error(), "a-broken") || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), server.URL) {
		t.Fatal("batch failure omitted source identity or exposed its private address", err)
	}
	after, _, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a-broken", "copy"} {
		if after.Profiles[name].Spec.Revision != before.Profiles[name].Spec.Revision {
			t.Fatal("batch refresh replaced a failed source or a one-time import", name)
		}
	}
	if after.Profiles["z-updated"].Spec.Revision != digest([]byte(good)) {
		t.Fatal("failure in an earlier source prevented an independent successful update")
	}
	afterTree, err := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil || !maps.Equal(beforeTree, afterTree) || !svc.active || len(svc.actions) != 0 {
		t.Fatal("batch refresh applied a configuration or changed the running service", err)
	}
	info, err := a.ProfileDetails(t.Context(), "z-updated")
	if err != nil || !info.Pending {
		t.Fatal("successful source update was not exposed as pending", err)
	}
}

func TestRefreshAllProfilesStopsAfterCancellation(t *testing.T) {
	a, svc := installedApp(t)
	for _, name := range []string{"a-first", "z-last"} {
		path := filepath.Join(t.TempDir(), name+".yaml")
		write(t, path, []byte("rules: ['MATCH,DIRECT']\n"))
		if err := a.AddSubscription(t.Context(), name, path); err != nil {
			t.Fatal(err)
		}
		write(t, path, []byte("rules: ['MATCH,REJECT']\n"))
	}
	index := filepath.Join(a.Home, "configuration", "library.json")
	before := read(t, index)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	validations := 0
	a.Validate = func(context.Context, string, string, string) error {
		validations++
		cancel()
		return context.Canceled
	}
	if err := a.RefreshAllProfiles(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("batch refresh lost cancellation", err)
	}
	if validations != 1 || string(read(t, index)) != string(before) || len(svc.actions) != 0 {
		t.Fatal("cancelled batch continued refreshing or committed state")
	}
}

func TestRefreshAllProfilesSkipsDeletedProfilesAndLeavesNoSourceRecords(t *testing.T) {
	a, _ := testApp(t)
	var visibleRequests, deletedRequests atomic.Int64
	var content atomic.Value
	content.Store("rules: ['MATCH,DIRECT']\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/deleted" {
			deletedRequests.Add(1)
		} else {
			visibleRequests.Add(1)
		}
		_, _ = w.Write([]byte(content.Load().(string)))
	}))
	defer server.Close()
	a.HTTP = server.Client()
	for _, name := range []string{"visible", "deleted"} {
		if err := a.AddSubscription(t.Context(), name, server.URL+"/"+name); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.DeleteProfile(t.Context(), "deleted"); err != nil {
		t.Fatal(err)
	}
	before, index, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := before.Profiles["deleted"]; exists || strings.Contains(string(index), "\"subscriptions\"") || strings.Contains(string(index), server.URL+"/deleted") {
		t.Fatal("deleting a profile left its remembered address or an independent source record")
	}
	updated := "rules: ['MATCH,REJECT']\n"
	content.Store(updated)
	if err := a.RefreshAllProfiles(t.Context()); err != nil {
		t.Fatal("bulk profile update failed", err)
	}
	after, _, err := a.library().Load()
	if err != nil || len(after.Profiles) != 1 || after.Profiles["visible"].Spec.Revision != digest([]byte(updated)) || after.Profiles["visible"].Spec.Revision == before.Profiles["visible"].Spec.Revision {
		t.Fatal("bulk update did not publish only the remaining profile", err)
	}
	if visibleRequests.Load() != 2 || deletedRequests.Load() != 1 {
		t.Fatal("bulk update downloaded a deleted profile or downloaded a live profile twice", visibleRequests.Load(), deletedRequests.Load())
	}
}

func TestRefreshHandlesRememberedAndOneTimeProfiles(t *testing.T) {
	a, svc := installedApp(t)
	local := filepath.Join(t.TempDir(), "profile.yaml")
	write(t, local, []byte("rules: ['MATCH,DIRECT']\n"))
	if err := a.AddSubscription(t.Context(), "saved", local); err != nil {
		t.Fatal(err)
	}
	if err := a.Add(t.Context(), "copied", local); err != nil {
		t.Fatal(err)
	}
	beforeTree, err := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil {
		t.Fatal(err)
	}
	updated := []byte("rules: ['MATCH,REJECT']\n")
	write(t, local, updated)
	if err := a.Refresh(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	lib, before, err := a.library().Load()
	if err != nil || lib.Profiles["saved"].Spec.Revision != digest(updated) {
		t.Fatal("remembered profile was not refreshed", err)
	}
	for _, name := range []string{"default", "copied", "absent"} {
		if err := a.Refresh(t.Context(), name); err == nil {
			t.Fatal("unrefreshable or missing profile was accepted", name)
		}
	}
	_, after, err := a.library().Load()
	afterTree, treeErr := installationTree(filepath.Join(a.Home, "installation"))
	if err != nil || treeErr != nil || string(before) != string(after) || !maps.Equal(beforeTree, afterTree) || len(svc.actions) != 0 {
		t.Fatal("refresh or rejected operations affected runtime or saved data", err, treeErr)
	}
}
