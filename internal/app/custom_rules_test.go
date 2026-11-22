package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"gopkg.in/yaml.v3"
)

const customRulesSource = "proxy-groups: [{name: ROUTE, type: select, proxies: [DIRECT, REJECT]}]\nrules: ['MATCH,ROUTE']\n"

func customRulesApp(t *testing.T, installed bool) (*App, *fakeService, string) {
	t.Helper()
	var a *App
	var svc *fakeService
	if installed {
		a, svc = installedApp(t)
	} else {
		a, svc = testApp(t)
	}
	path := filepath.Join(t.TempDir(), "profile.yaml")
	write(t, path, []byte(customRulesSource))
	if err := a.AddSubscription(t.Context(), "saved", path); err != nil {
		t.Fatal(err)
	}
	return a, svc, path
}

func customRulesDiagnostic(t *testing.T, err error, code string) {
	t.Helper()
	var detail *diagnostic.Error
	if !errors.As(err, &detail) || detail.Code != code {
		t.Fatalf("expected diagnostic %s, got %v", code, err)
	}
}

func TestCustomRulesSaveLoadsOrderedRulesOnceWithoutRestart(t *testing.T) {
	a, svc, _ := customRulesApp(t, true)
	if err := a.Apply(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	reloads := observeActiveRefresh(t, a, svc, nil)
	draft, err := a.GetCustomRules(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	draft.Rules = []string{"DOMAIN,z.invalid,ROUTE", "DOMAIN,a.invalid,DIRECT", "DOMAIN,z.invalid,ROUTE"}
	saved, err := a.SaveCustomRules(t.Context(), draft)
	if err != nil || saved.Pending || !saved.Validated || !saved.Applied || saved.Token == draft.Token || *reloads != 1 || len(svc.actions) > 0 {
		t.Fatal("saving selected rules did not hot load", err)
	}
	expected := []any{"DOMAIN,z.invalid,ROUTE", "DOMAIN,a.invalid,DIRECT", "DOMAIN,z.invalid,ROUTE", "MATCH,ROUTE"}
	if got := generatedDocument(t, a)["rules"]; !reflect.DeepEqual(got, expected) {
		t.Fatal("saved rule order lost", got)
	}
	if _, err := a.SaveCustomRules(t.Context(), saved); err != nil || *reloads != 1 {
		t.Fatal("repeated save reloaded", err)
	}
}

func TestCustomRulesPreserveOtherOverridesAndRefreshWithoutLosingEdits(t *testing.T) {
	a, _, path := customRulesApp(t, true)
	raw := []byte("set: {log-level: info, native-extension: {nested: [{name: kept, values: [1, true, text]}]}}\nappend-rules: ['DOMAIN,tail.invalid,REJECT']\n")
	if err := a.SetProfileOverrides(t.Context(), "saved", raw); err != nil {
		t.Fatal(err)
	}
	draft, err := a.GetCustomRules(t.Context(), "saved")
	if err != nil {
		t.Fatal(err)
	}
	draft.Rules = []string{"DOMAIN,custom.invalid,ROUTE"}
	draft, err = a.SaveCustomRules(t.Context(), draft)
	if err != nil {
		t.Fatal(err)
	}
	lib, _, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err := yaml.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(lib.Profiles["saved"].Spec.Overrides), &after); err != nil {
		t.Fatal(err)
	}
	delete(after, "prepend-rules")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("custom editor changed another override")
	}
	write(t, path, []byte("rules: ['DOMAIN,new-upstream.invalid,DIRECT', 'MATCH,ROUTE']\nproxy-groups: [{name: ROUTE, type: select, proxies: [DIRECT, REJECT]}]\n"))
	if err := a.Refresh(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	fresh, err := a.GetCustomRules(t.Context(), "saved")
	if err != nil || !reflect.DeepEqual(fresh.Rules, draft.Rules) || fresh.Token == draft.Token {
		t.Fatal("refresh lost custom rules or did not invalidate the old editor token", err)
	}
	index := filepath.Join(a.Home, "configuration", "library.json")
	saved := read(t, index)
	_, err = a.SaveCustomRules(t.Context(), draft)
	customRulesDiagnostic(t, err, "diagnostic.customRules.conflict")
	if !bytes.Equal(saved, read(t, index)) {
		t.Fatal("stale editor replaced refreshed subscription data")
	}
	// Clearing changes only the custom prepend field.
	fresh.Rules = nil
	if _, err := a.SaveCustomRules(t.Context(), fresh); err != nil {
		t.Fatal(err)
	}
	lib, _, err = a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	after = nil
	if err := yaml.Unmarshal([]byte(lib.Profiles["saved"].Spec.Overrides), &after); err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("clearing rules lost unrelated overrides", err)
	}
}

func TestCustomRulesRejectConflictingProfileEditsAndDeletedProfiles(t *testing.T) {
	for _, mutation := range []string{"overrides", "profile-id", "empty-token", "forged-token", "deleted"} {
		t.Run(mutation, func(t *testing.T) {
			a, _, _ := customRulesApp(t, false)
			draft, err := a.GetCustomRules(t.Context(), "saved")
			if err != nil {
				t.Fatal(err)
			}
			draft.Rules = []string{"DOMAIN,custom.invalid,DIRECT"}
			code := "diagnostic.customRules.conflict"
			switch mutation {
			case "overrides":
				err = a.SetProfileOverrides(t.Context(), "saved", []byte("set: {log-level: debug}"))
			case "profile-id":
				addFixtureProfile(t, a, "other")
				draft.ProfileID = "other"
			case "empty-token":
				draft.Token = ""
			case "forged-token":
				draft.Token = "forged"
			case "deleted":
				err = a.DeleteProfile(t.Context(), "saved")
				code = "diagnostic.profile.missing"
			}
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(a.Home, "configuration", "library.json")
			before := read(t, path)
			_, err = a.SaveCustomRules(t.Context(), draft)
			customRulesDiagnostic(t, err, code)
			if !bytes.Equal(before, read(t, path)) {
				t.Fatal("rejected save changed the configuration library")
			}
		})
	}
}

func TestCustomRulesRenamePreservesStableIDAndIgnoresCallerMetadata(t *testing.T) {
	a, _, _ := customRulesApp(t, false)
	draft, err := a.GetCustomRules(t.Context(), "saved")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RenameProfile(t.Context(), "saved", "Renamed profile"); err != nil {
		t.Fatal(err)
	}
	draft.ProfileName, draft.Applied, draft.Pending, draft.Validated = "untrusted", true, true, true
	draft.Rules = []string{"DOMAIN,custom.invalid,DIRECT"}
	saved, err := a.SaveCustomRules(t.Context(), draft)
	if err != nil || saved.ProfileID != "saved" || saved.ProfileName != "Renamed profile" || saved.Applied || saved.Pending || saved.Validated {
		t.Fatalf("caller metadata was trusted or rename retargeted the editor: %+v %v", saved, err)
	}
	if got, err := a.GetCustomRules(t.Context(), "Renamed profile"); err != nil || got.ProfileID != "saved" {
		t.Fatal("display-name lookup did not resolve the stable ID", err)
	}
	raw, err := json.Marshal(saved)
	if err != nil || bytes.Contains(raw, []byte(saved.Token)) || bytes.Contains(raw, []byte("revision")) {
		t.Fatal("editor token or source fingerprint leaked into JSON", err)
	}
}

func TestCustomRulesDefaultRemainsReadOnly(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "uninstalled", true: "installed"}[installed], func(t *testing.T) {
			a, _ := testApp(t)
			if installed {
				a, _ = installedApp(t)
			}
			_, err := a.GetCustomRules(t.Context(), "")
			customRulesDiagnostic(t, err, "diagnostic.customRules.readOnly")
			_, err = a.GetCustomRules(t.Context(), "default")
			customRulesDiagnostic(t, err, "diagnostic.profile.missing")
			_, err = a.SaveCustomRules(t.Context(), CustomRulesDraft{ProfileID: "default", Token: "forged", Rules: []string{"MATCH,REJECT"}})
			customRulesDiagnostic(t, err, "diagnostic.customRules.readOnly")

		})
	}
}

func TestCustomRulesInvalidSaveAndEngineRejectionLeaveLibraryUnchanged(t *testing.T) {
	for _, failure := range []string{"empty", "newline", "missing-target", "missing-provider", "engine", "canceled", "already-canceled"} {
		t.Run(failure, func(t *testing.T) {
			a, svc, _ := customRulesApp(t, true)
			draft, err := a.GetCustomRules(t.Context(), "saved")
			if err != nil {
				t.Fatal(err)
			}
			draft.Rules = []string{"DOMAIN,custom.invalid,DIRECT"}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch failure {
			case "empty":
				draft.Rules = []string{""}
			case "newline":
				draft.Rules = []string{"DOMAIN,private-rule.invalid,DIRECT\nMATCH,REJECT"}
			case "missing-target":
				draft.Rules = []string{"DOMAIN,private-rule.invalid,missing-target"}
			case "missing-provider":
				draft.Rules = []string{"RULE-SET,private-provider,DIRECT"}
			case "engine":
				a.Validate = func(context.Context, string, string, string) error { return errors.New("engine rejected candidate") }
			case "canceled":
				a.Validate = func(context.Context, string, string, string) error { cancel(); return nil }
			case "already-canceled":
				cancel()
			}
			path := filepath.Join(a.Home, "configuration", "library.json")
			before := read(t, path)
			_, err = a.SaveCustomRules(ctx, draft)
			if err == nil || strings.Contains(err.Error(), "private-rule") || strings.Contains(err.Error(), "private-provider") {
				t.Fatal("invalid save accepted or error disclosed rule values", err)
			}
			if strings.Contains(failure, "canceled") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation did not retain its error identity", err)
			}
			if !bytes.Equal(before, read(t, path)) || len(svc.actions) != 0 {
				t.Fatal("rejected save changed persistent data or services")
			}
		})
	}
}

func TestCustomRulesUseEngineOnlyForInstalledSaves(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "uninstalled", true: "installed"}[installed], func(t *testing.T) {
			a, _, _ := customRulesApp(t, installed)
			calls := 0
			a.Validate = func(ctx context.Context, _, config, _ string) error {
				calls++
				if !bytes.Contains(read(t, config), []byte("DOMAIN,custom.invalid,ROUTE")) {
					t.Fatal("engine did not receive the composed candidate configuration")
				}
				return ctx.Err()
			}
			draft, err := a.GetCustomRules(t.Context(), "saved")
			if err != nil || calls != 0 {
				t.Fatal("read-only editor query executed the core", err)
			}
			draft.Rules = []string{"DOMAIN,custom.invalid,ROUTE"}
			saved, err := a.SaveCustomRules(t.Context(), draft)
			want := 0
			if installed {
				want = 1
			}
			if err != nil || calls != want || saved.Validated != installed {
				t.Fatalf("wrong validation behavior: calls=%d saved=%+v err=%v", calls, saved, err)
			}
		})
	}
}

func TestCustomRulesAtomicSavePreservesExternalLibraryChanges(t *testing.T) {
	a, _, _ := customRulesApp(t, true)
	draft, err := a.GetCustomRules(t.Context(), "saved")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.Home, "configuration", "library.json")
	external := append(read(t, path), '\n')
	a.Validate = func(context.Context, string, string, string) error {
		return os.WriteFile(path, external, 0600)
	}
	draft.Rules = []string{"DOMAIN,custom.invalid,DIRECT"}
	if _, err := a.SaveCustomRules(t.Context(), draft); err == nil {
		t.Fatal("save ignored an external edit during validation")
	}
	if !bytes.Equal(external, read(t, path)) {
		t.Fatal("save replaced an external library change")
	}
}

func TestCustomRulesRefuseTakeoverAndMalformedSavedOverrides(t *testing.T) {
	for _, raw := range []string{"set: {rules: ['MATCH,DIRECT']}", "set: {rules: []}", "prepend-rules: null", "prepend-rules: []\nprepend-rules: []", "{}\n---\n{}", "set: {secret: private-token}", "unknown-operation: []"} {
		t.Run(raw, func(t *testing.T) {
			a, _, _ := customRulesApp(t, false)
			lib, old, err := a.library().Load()
			if err != nil {
				t.Fatal(err)
			}
			entry := lib.Profiles["saved"]
			entry.Spec.Overrides = raw
			lib.Profiles["saved"] = entry
			if err := a.library().Save(lib, old); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(a.Home, "configuration", "library.json")
			before := read(t, path)
			_, err = a.GetCustomRules(t.Context(), "saved")
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatal("invalid stored overrides were exposed as editable", err)
			}
			if strings.Contains(raw, "rules:") && strings.HasPrefix(raw, "set:") {
				customRulesDiagnostic(t, err, "diagnostic.customRules.takeover")
			}
			token := (buildRecord{Profile: "saved", Recipe: &entry.Spec}).fingerprint()
			if _, err := a.SaveCustomRules(t.Context(), CustomRulesDraft{ProfileID: "saved", Token: token, Rules: []string{"DOMAIN,custom.invalid,DIRECT"}}); err == nil {
				t.Fatal("save silently repaired an invalid stored override")
			}
			if !bytes.Equal(before, read(t, path)) {
				t.Fatal("invalid overrides were modified")
			}
		})
	}
}
