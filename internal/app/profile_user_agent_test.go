package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSubscriptionUserAgentFollowsItsProfileThroughRefreshAndSourceChanges(t *testing.T) {
	a, _ := testApp(t)
	const agent = "Fixture/1.0 (profile)"
	var custom, standard atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/custom":
			custom.Add(1)
			if r.UserAgent() != agent {
				t.Errorf("custom subscription got User-Agent %q", r.UserAgent())
			}
		case "/default":
			standard.Add(1)
			if !strings.HasPrefix(r.UserAgent(), "Go-http-client/") {
				t.Errorf("default subscription got User-Agent %q", r.UserAgent())
			}
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte("rules: ['MATCH,DIRECT']\n"))
	}))
	defer server.Close()
	a.HTTP = server.Client()
	ctx := t.Context()
	if err := a.CreateProfile(ctx, ProfileDraft{Name: "custom", Address: server.URL + "/custom", UserAgent: "  " + agent + " "}); err != nil {
		t.Fatal(err)
	}
	if err := a.AddSubscription(ctx, "standard", server.URL+"/default"); err != nil {
		t.Fatal(err)
	}
	if err := a.Refresh(ctx, "custom"); err != nil {
		t.Fatal(err)
	}
	if summary, err := a.RefreshAllProfiles(ctx); err != nil || summary.Updated != 2 || custom.Load() != 3 || standard.Load() != 2 {
		t.Fatal("refresh did not send each profile's request exactly once", summary, err)
	}
	local := filepath.Join(t.TempDir(), "local.yaml")
	write(t, local, []byte("rules: ['MATCH,REJECT']\n"))
	if err := a.UpdateProfileAddress(ctx, "custom", local); err != nil {
		t.Fatal(err)
	}
	if settings, err := a.GetProfileSettings(ctx, "custom"); err != nil || settings.UserAgent != "" {
		t.Fatal("local replacement retained the previous subscription User-Agent", err)
	}
	if err := a.UpdateProfileAddress(ctx, "custom", server.URL+"/default"); err != nil {
		t.Fatal(err)
	}
}

func TestChangingSubscriptionUserAgentValidatesTheResponseBeforeSaving(t *testing.T) {
	a, _ := testApp(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.UserAgent() == "Rejected/1.0" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		rule := "MATCH,DIRECT"
		if r.UserAgent() == "Updated/1.0" {
			rule = "MATCH,REJECT"
		}
		_, _ = w.Write([]byte("rules: ['" + rule + "']\n"))
	}))
	defer server.Close()
	a.HTTP = server.Client()
	ctx := t.Context()
	draft := ProfileDraft{Name: "work", Address: server.URL, UserAgent: "private-token\r\n"}
	if err := a.CreateProfile(ctx, draft); err == nil || strings.Contains(err.Error(), "private-token") || requests.Load() != 0 {
		t.Fatal("malformed User-Agent was sent or exposed", err)
	}
	draft.UserAgent = "Original/1.0"
	if err := a.CreateProfile(ctx, draft); err != nil {
		t.Fatal(err)
	}
	load := func() ProfileSettings {
		t.Helper()
		s, err := a.GetProfileSettings(ctx, "work")
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	original := load()
	edited := original
	edited.UserAgent = "Rejected/1.0"
	if err := a.SaveProfileSettings(ctx, edited); err == nil || strings.Contains(err.Error(), edited.UserAgent) || original != load() {
		t.Fatal("failed download exposed or persisted rejected settings", err)
	}
	edited.UserAgent = "Updated/1.0"
	if err := a.SaveProfileSettings(ctx, edited); err != nil || load().UserAgent != edited.UserAgent || requests.Load() != 3 {
		t.Fatal("changed User-Agent and response were not saved together", err)
	}
	if got := string(read(t, savedSourcePath(t, a, "work"))); !strings.Contains(got, "MATCH,REJECT") {
		t.Fatalf("snapshot did not reflect the changed User-Agent: %q", got)
	}
	if err := a.SaveProfileSettings(ctx, original); err == nil || requests.Load() != 3 {
		t.Fatal("stale form overwrote the changed User-Agent", err)
	}
	edited = load()
	edited.UserAgent = ""
	if err := a.SaveProfileSettings(ctx, edited); err != nil || load().UserAgent != "" || requests.Load() != 4 {
		t.Fatal("clearing the User-Agent did not fetch and persist the default response", err)
	}
}
