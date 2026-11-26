package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProxyAuthAccountsReviewedAgainstAppliedGeneration(t *testing.T) {
	a, _ := installedApp(t)
	ctx := t.Context()
	ports := availableSettings(t)
	ports.AllowLAN = true
	plan, err := a.PrepareRuntimeSettings(ctx, ports)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyRuntimeSettings(ctx, plan); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "local.yaml")
	write(t, source, []byte("rules: ['MATCH,DIRECT']\nauthentication: ['source:inherited-fixture']\nskip-auth-prefixes: ['0.0.0.0/0']\n"))
	if err := a.AddSubscription(ctx, "source", source); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "source"); err != nil {
		t.Fatal(err)
	}
	editor := func() ProxyAuthSettings {
		t.Helper()
		open, err := a.GetProxyAuth(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return open
	}
	current := editor()
	if !slices.Equal(current.Usernames, []string{"source"}) {
		t.Fatal("missing current inherited account")
	}
	draft := []ProxyAuthUser{{OriginalUsername: "source", Username: "renamed"}, {Username: "second", Password: "new:fixture"}}
	plan, err = a.PrepareProxyAuth(ctx, current, draft)
	if err != nil || !plan.Changed || plan.Current.ProxyUserCount != 1 || plan.Proposed.ProxyUserCount != 2 {
		t.Fatal("invalid proxy authentication review", err)
	}
	draft[1].Password = "mutated"
	assertPublic := func(value any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		out := string(raw) + fmt.Sprintf("%+v %#v", value, value)
		for _, password := range []string{"inherited-fixture", "new:fixture"} {
			if strings.Contains(out, password) {
				t.Fatal("credentials escaped the editor")
			}
		}
	}
	assertPublic(current)
	assertPublic(plan)
	result, err := a.ApplyRuntimeSettings(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	assertPublic(result)
	snap, err := a.Snapshot(ctx)
	if err != nil || snap.RuntimeSettings.ProxyUserCount != 2 {
		t.Fatal("snapshot missing current user count", err)
	}
	assertPublic(snap)
	if _, err := a.PrepareProxyAuth(ctx, current, nil); err == nil {
		t.Fatal("stale credential edit accepted")
	}
	if _, err := a.ApplyRuntimeSettings(ctx, plan); err == nil {
		t.Fatal("stale credential review accepted")
	}
	verify := func(want []string) {
		t.Helper()
		_, config, _ := a.paths()
		var cfg struct {
			Auth []string `yaml:"authentication"`
			Skip []string `yaml:"skip-auth-prefixes"`
		}
		if err := yaml.Unmarshal(read(t, config), &cfg); err != nil || !slices.Equal(cfg.Auth, want) || len(cfg.Skip) != 0 {
			t.Fatal("effective authentication or bypass list differs from reviewed settings")
		}
	}
	saved := []string{"renamed:inherited-fixture", "second:new:fixture"}
	verify(saved)
	write(t, source, []byte("rules: ['MATCH,DIRECT']\nauthentication: ['updated:discarded']\nskip-auth-prefixes: ['0.0.0.0/0']\n"))
	if err := a.Refresh(ctx, "source"); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "source"); err != nil {
		t.Fatal(err)
	}
	verify(saved)
	ports.AllowLAN = false
	plan, err = a.PrepareRuntimeSettings(ctx, ports)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyRuntimeSettings(ctx, plan); err != nil {
		t.Fatal(err)
	}
	verify(saved)
	plan, err = a.PrepareProxyAuth(ctx, editor(), nil)
	if err != nil || !plan.Changed || plan.Proposed.ProxyUserCount != 0 {
		t.Fatal("cannot review removal of all proxy users", err)
	}
	if _, err := a.ApplyRuntimeSettings(ctx, plan); err != nil {
		t.Fatal(err)
	}
	ports.AllowLAN = true
	plan, err = a.PrepareRuntimeSettings(ctx, ports)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyRuntimeSettings(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, "source"); err != nil {
		t.Fatal(err)
	}
	verify(nil)
	if !slices.Equal(editor().Usernames, []string{}) {
		t.Fatal("source repopulated the explicitly empty account list")
	}
	plan, err = a.PrepareProxyAuth(ctx, editor(), nil)
	if err != nil || plan.Changed {
		t.Fatal("repeated empty save should be a no-op", err)
	}
}

func TestProxyAuthRejectsInvalidDraftsWithoutLeakingCredentials(t *testing.T) {
	secret := "fixture:password"
	for _, draft := range [][]ProxyAuthUser{
		{{Username: "", Password: secret}}, {{Username: "a:b", Password: secret}}, {{Username: "a\nb", Password: secret}},
		{{Username: strings.Repeat("人", 86), Password: secret}}, {{Username: "a", Password: ""}},
		{{Username: "a", Password: secret + "\n"}}, {{Username: "a", Password: strings.Repeat("人", 86)}},
		{{Username: "a", Password: secret}, {Username: "a", Password: "fixture"}},
		{{OriginalUsername: "absent", Username: "renamed"}},
	} {
		edit := proxyAuthEdit{users: draft, revision: "current"}
		if _, err := edit.resolve(nil, "current"); err == nil || strings.Contains(err.Error(), secret) {
			t.Fatal("invalid draft accepted or a credential appeared in validation")
		}
	}
}
