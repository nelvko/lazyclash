package app

import (
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/configuration"
)

func TestProfileQuotaTracksValidatedDownloadsWithoutChangingTheAppliedRecipe(t *testing.T) {
	a, svc := installedApp(t)
	const valid = "rules: ['MATCH,DIRECT']\n"
	body, header := valid, "upload=10;download=20;total=100;expire=2000000000"
	requests := 0
	a.HTTP = &http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		h := make(http.Header)
		h.Set("Subscription-Userinfo", header)
		return &http.Response{StatusCode: http.StatusOK, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	check := func(want configuration.SubscriptionQuota) {
		t.Helper()
		info, err := a.ProfileDetails(t.Context(), "work")
		if err != nil || info.Pending || info.Name != "work" {
			t.Fatal("quota update changed profile identity or recipe", info, err)
		}
		if want == (configuration.SubscriptionQuota{}) {
			if info.Quota != nil {
				t.Fatal("expected no quota", *info.Quota)
			}
		} else if info.Quota == nil || *info.Quota != want {
			t.Fatalf("got quota %+v, want %+v", info.Quota, want)
		}
		lib, raw, err := a.library().Load()
		if err != nil || lib.Profiles["work"].Quota != want || bytes.Contains(raw, []byte(`"quota"`)) != (info.Quota != nil) {
			t.Fatal("stored quota differs from the published metadata", err)
		}
	}
	if err := a.AddSubscription(t.Context(), "work", "https://profile.invalid/work"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(t.Context(), "work"); err != nil {
		t.Fatal(err)
	}
	_, generation, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	check(configuration.SubscriptionQuota{UsedBytes: 30, TotalBytes: 100, ExpiresAt: 2000000000})
	header = "upload=11;download=25;total=200"
	if err := a.Refresh(t.Context(), "work"); err != nil {
		t.Fatal(err)
	}
	updated := configuration.SubscriptionQuota{UsedBytes: 36, TotalBytes: 200}
	check(updated)
	body, header = "invalid: response", "expire=999"
	if err := a.UpdateProfileAddress(t.Context(), "work", "https://new.invalid"); err == nil {
		t.Fatal("invalid source accepted")
	}
	check(updated)
	body, header = valid, "invalid"
	if err := a.Refresh(t.Context(), "work"); err != nil {
		t.Fatal(err)
	}
	check(configuration.SubscriptionQuota{})
	header = "expire=123"
	if err := a.Refresh(t.Context(), "work"); err != nil {
		t.Fatal(err)
	}
	check(configuration.SubscriptionQuota{ExpiresAt: 123})
	local := filepath.Join(t.TempDir(), "local.yaml")
	write(t, local, []byte(valid))
	if err := a.UpdateProfileAddress(t.Context(), "work", local); err != nil {
		t.Fatal(err)
	}
	check(configuration.SubscriptionQuota{})
	header = "upload=1;download=3;total=20"
	if err := a.UpdateProfileAddress(t.Context(), "work", "https://new.invalid"); err != nil {
		t.Fatal(err)
	}
	check(configuration.SubscriptionQuota{UsedBytes: 4, TotalBytes: 20})
	if _, current, err := a.load(); err != nil || current != generation || requests != 6 || len(svc.actions) != 0 {
		t.Fatal("reading quota downloaded twice or changed the active generation", current, requests, err)
	}
	if err := a.DeleteProfileWithReplacement(t.Context(), "work", "default"); err != nil {
		t.Fatal("re-reading the quota record blocked deletion of the selected profile", err)
	}
}
