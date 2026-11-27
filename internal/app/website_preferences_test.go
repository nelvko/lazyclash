package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWebsitePreferencesPersistDefaultsEditsAndAnEmptyList(t *testing.T) {
	a, _ := testApp(t)
	path := filepath.Join(a.Home, "preferences.json")
	write(t, path, []byte(`{"Language":"zh-Hans"}`))
	p, err := a.LoadPreferences(t.Context())
	defaults := []Website{{"GitHub", "https://github.com/"}, {"Google", "https://www.google.com/"}, {"YouTube", "https://www.youtube.com/"}}
	if err != nil || !slices.Equal(p.WebsitesForTesting(), defaults) {
		t.Fatal(p.WebsitesForTesting(), err)
	}
	for _, site := range defaults {
		if _, err := a.RemoveWebsite(t.Context(), site); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.SaveThemePreference(t.Context(), ThemeLight); err != nil {
		t.Fatal(err)
	}
	p, err = a.ReadPreferences(t.Context())
	if err != nil || p.Websites == nil || len(p.WebsitesForTesting()) != 0 || p.Language != "zh-Hans" {
		t.Fatal(p, err)
	}
	want := Website{"example.org", "https://example.org/health"}
	got, err := a.AddWebsite(t.Context(), Website{URL: "  EXAMPLE.ORG:443/health "})
	if err != nil || !slices.Equal(got, []Website{want}) {
		t.Fatal(got, err)
	}
	p, err = a.ReadPreferences(t.Context())
	if err != nil || !slices.Equal(p.WebsitesForTesting(), got) || p.Theme != ThemeLight {
		t.Fatal(p, err)
	}
	for _, duplicate := range []Website{{"EXAMPLE.org", "sample.org"}, {"Different name", "https://EXAMPLE.org:443/health"}} {
		if _, err := a.AddWebsite(t.Context(), duplicate); err == nil {
			t.Fatal("duplicate accepted")
		}
	}
	if current, err := a.RemoveWebsite(t.Context(), Website{Name: want.Name, URL: "https://example.org/old"}); err == nil || !slices.Equal(current, got) {
		t.Fatal("stale removal did not return the current list", current, err)
	}
	p, err = a.LoadPreferences(t.Context())
	if err != nil || !slices.Equal(p.WebsitesForTesting(), got) {
		t.Fatal(p, err)
	}
	if _, err := a.RemoveWebsite(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	if current, err := a.RemoveWebsite(t.Context(), want); err == nil || current == nil || len(current) > 0 {
		t.Fatal("stale removal did not return an explicitly empty list", current, err)
	}
}

func TestWebsiteAddressValidation(t *testing.T) {
	for _, pair := range [][2]string{
		{"HTTP://Example.org:80", "http://example.org/"},
		{"example.org:8443/check", "https://example.org:8443/check"},
		{"https://[::1]:443", "https://[::1]/"},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080/"},
	} {
		got, err := (Website{Name: " 自定义 ", URL: pair[0]}).normalized()
		if err != nil || got.URL != pair[1] || got.Name != "自定义" {
			t.Fatal(pair, got, err)
		}
	}
	a, _ := testApp(t)
	for _, site := range []Website{
		{URL: "https://user:private-token@example.org"}, {URL: "https://example.org/?token=private-token"},
		{URL: "example.org/#private-token"}, {URL: "ftp://example.org"}, {URL: "example.org:65536"},
		{URL: "http://bad host/"}, {URL: "https://[nothost]/"}, {URL: ""}, {Name: "line\nprivate-token", URL: "example.org"},
	} {
		if _, err := a.AddWebsite(t.Context(), site); err == nil || strings.Contains(err.Error(), "private-token") {
			t.Fatal("invalid website accepted or sensitive input echoed")
		}
	}
	path := filepath.Join(a.Home, "preferences.json")
	assertMissing(t, path)
	write(t, path, []byte(`{"Websites":[{"Name":"Example","URL":"https://example.org/?token=private-token"}]}`))
	before := string(read(t, path))
	if _, err := a.LoadPreferences(t.Context()); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("invalid stored URL exposed")
	}
	if err := a.SaveThemePreference(t.Context(), ThemeDark); err == nil || string(read(t, path)) != before {
		t.Fatal("invalid stored target overwritten")
	}
}

func TestConcurrentWebsiteEditsPreserveOtherSessions(t *testing.T) {
	a, _ := testApp(t)
	b, err := New(a.Home, a.Name)
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	go func() { _, err := a.AddWebsite(t.Context(), Website{URL: "a.example"}); errs <- err }()
	go func() { _, err := b.AddWebsite(t.Context(), Website{URL: "b.example"}); errs <- err }()
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	p, err := a.ReadPreferences(t.Context())
	if err != nil || len(p.WebsitesForTesting()) != len(DefaultWebsites())+2 {
		t.Fatal(p, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.RemoveWebsite(ctx, p.Websites[0]); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	path := filepath.Join(a.Home, "preferences.json")
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	after, err := b.ReadPreferences(t.Context())
	if err != nil || !slices.Equal(after.Websites, p.Websites) {
		t.Fatal(after, err)
	}
}
