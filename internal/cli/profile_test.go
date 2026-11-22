package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
)

func runProfileCLI(t *testing.T, backend *app.App, args ...string) (string, error) {
	t.Helper()
	cmd := newProfileCommand(func() *app.App { return backend })
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return output.String(), err
}

func writeCLIConfig(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.yaml")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func configDigest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func storedCLIProfileRevision(t *testing.T, a *app.App, id string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(a.Home, "configuration", "library.json"))
	if err != nil {
		t.Fatal(err)
	}
	var library struct {
		Profiles map[string]struct{ Spec struct{ Revision string } }
	}
	if err := json.Unmarshal(raw, &library); err != nil {
		t.Fatal(err)
	}
	return library.Profiles[id].Spec.Revision
}

func TestProfileRememberAddressAndExplicitRefreshPreserveStoppedCore(t *testing.T) {
	a, service := installBackend(t)
	initial := "rules: ['MATCH,DIRECT']\n"
	source := writeCLIConfig(t, initial)
	if _, err := runProfileCLI(t, a, "add", source, "--name", "saved", "--remember-source"); err != nil {
		t.Fatal(err)
	}
	profile, err := a.ProfileDetails(t.Context(), "saved")
	if err != nil || !profile.Refreshable || storedCLIProfileRevision(t, a, "saved") != configDigest(initial) {
		t.Fatal("remembered profile metadata is incorrect", profile, err)
	}
	updated := "rules: ['MATCH,REJECT']\n"
	if err := os.WriteFile(source, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runProfileCLI(t, a, "refresh", "saved"); err != nil {
		t.Fatal(err)
	}
	if storedCLIProfileRevision(t, a, "saved") != configDigest(updated) {
		t.Fatal("refresh did not publish the profile revision")
	}
	if service.installs != 0 || service.actions != 0 {
		t.Fatal("profile refresh touched the core lifecycle")
	}
}

func TestProfileImportDoesNotRememberAddressByDefault(t *testing.T) {
	a, service := installBackend(t)
	source := writeCLIConfig(t, "rules: ['MATCH,DIRECT']\n")
	if _, err := runProfileCLI(t, a, "add", source, "--name", "copied"); err != nil {
		t.Fatal(err)
	}
	profile, err := a.ProfileDetails(t.Context(), "copied")
	if err != nil || profile.Refreshable {
		t.Fatal("one-time import retained its address", profile, err)
	}
	if _, err := runProfileCLI(t, a, "refresh", "copied"); err == nil {
		t.Fatal("one-time import was refreshed")
	}
	if service.installs != 0 || service.actions != 0 {
		t.Fatal("import touched the core lifecycle")
	}
}

func TestProfileOverridesAndDeletionWithoutInstallation(t *testing.T) {
	a, service := installBackend(t)
	source := writeCLIConfig(t, "proxies: [{name: first, type: direct}]\nrules: ['MATCH,DIRECT']\n")
	if _, err := runProfileCLI(t, a, "add", source, "--name", "work"); err != nil {
		t.Fatal(err)
	}
	override := writeCLIConfig(t, "prepend-rules: ['DOMAIN,local.example,DIRECT']\n")
	if _, err := runProfileCLI(t, a, "override", "work", override); err != nil {
		t.Fatal(err)
	}
	output, err := runProfileCLI(t, a, "show", "work")
	var profile app.ProfileInfo
	if err != nil || json.Unmarshal([]byte(output), &profile) != nil || profile.ID != "work" || !profile.HasOverrides || profile.Core != "mihomo" {
		t.Fatal("profile details omitted metadata", output, err)
	}
	if strings.Contains(output, "local.example") || strings.Contains(output, source) {
		t.Fatal("public profile metadata leaked configuration content")
	}
	if _, err := runProfileCLI(t, a, "delete", "work"); err != nil {
		t.Fatal(err)
	}
	profiles, err := a.ListProfiles(t.Context())
	if err != nil || len(profiles) != 0 {
		t.Fatal("profile deletion left a saved entry", profiles, err)
	}
	if service.installs != 0 || service.actions != 0 {
		t.Fatal("profile metadata edit touched the core lifecycle")
	}
}

func TestProfileInputRejectsUnsafeFiles(t *testing.T) {
	file := writeCLIConfig(t, "set: {}\n")
	link := filepath.Join(t.TempDir(), "link.yaml")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProfileInput(link); err == nil {
		t.Fatal("accepted symlink input")
	}
	if _, err := readProfileInput("relative.yaml"); err == nil {
		t.Fatal("accepted relative input")
	}
	large := writeCLIConfig(t, strings.Repeat("a", (8<<20)+1))
	if _, err := readProfileInput(large); err == nil {
		t.Fatal("accepted oversized input")
	}
}

func TestProfileCLIAutomaticNamesRenameAndURLKeepStableID(t *testing.T) {
	a, service := installBackend(t)
	providerName := "Airport A"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("profile-title", providerName)
		_, _ = w.Write([]byte("rules: ['MATCH,DIRECT']\n"))
	}))
	defer server.Close()
	if _, err := runProfileCLI(t, a, "add", server.URL+"/subscription", "--remember-source"); err != nil {
		t.Fatal(err)
	}
	profiles, err := a.ListProfiles(t.Context())
	if err != nil || len(profiles) != 1 || profiles[0].Name != providerName || profiles[0].NameSource != "provider" || profiles[0].ID == "" {
		t.Fatalf("automatic CLI name: %+v %v", profiles, err)
	}
	id := profiles[0].ID
	if _, err := runProfileCLI(t, a, "rename", id, "我的机场"); err != nil {
		t.Fatal(err)
	}
	providerName = "Airport B"
	if _, err := runProfileCLI(t, a, "refresh", id); err != nil {
		t.Fatal(err)
	}
	detail, err := a.ProfileDetails(t.Context(), id)
	if err != nil || detail.ID != id || detail.Name != "我的机场" || detail.NameSource != "custom" {
		t.Fatalf("custom CLI name was overwritten: %+v %v", detail, err)
	}
	if _, err := runProfileCLI(t, a, "url", id, server.URL+"/replacement"); err != nil {
		t.Fatal(err)
	}
	detail, err = a.ProfileDetails(t.Context(), id)
	if err != nil || detail.ID != id || detail.Name != "我的机场" {
		t.Fatalf("URL edit changed profile identity: %+v %v", detail, err)
	}
	if _, err := runProfileCLI(t, a, "add", server.URL+"/other", "--name", "Manual name"); err != nil {
		t.Fatal(err)
	}
	profiles, err = a.ListProfiles(t.Context())
	custom := false
	for _, profile := range profiles {
		custom = custom || profile.Name == "Manual name" && profile.NameSource == "custom"
	}
	if err != nil || !custom {
		t.Fatalf("--name was not marked custom: %+v %v", profiles, err)
	}
	if _, err := runProfileCLI(t, a, "add", "legacy", server.URL); err == nil {
		t.Fatal("accepted the removed two-positional-argument syntax")
	}
	if service.actions != 0 || service.installs != 0 {
		t.Fatal("metadata commands changed the core lifecycle")
	}
}

func TestProfileCLIEmptyLibraryAndExplicitDeleteIntent(t *testing.T) {
	a, _ := installBackend(t)
	list, err := runProfileCLI(t, a, "list")
	if err != nil || strings.TrimSpace(list) != "[]" {
		t.Fatalf("expected an empty saved library, got %s, %v", list, err)
	}
	if _, err := runProfileCLI(t, a, "deactivate"); err != nil {
		t.Fatal("deactivate should be idempotent with no selection", err)
	}
	source := writeCLIConfig(t, "rules: ['MATCH,DIRECT']\n")
	if _, err := runProfileCLI(t, a, "add", source, "--name", "work"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"show", "default"}, {"apply", "default"},
		{"delete", "work", "--use", ""}, {"delete", "work", "--use", "default"},
		{"delete", "work", "--use", "work", "--no-profile"}, {"delete", "work", "--no-profile"},
	} {
		if _, err := runProfileCLI(t, a, args...); err == nil {
			t.Fatalf("accepted invalid operation %q", args)
		}
	}
	if _, err := a.ProfileDetails(t.Context(), "work"); err != nil {
		t.Fatal("invalid delete removed the profile", err)
	}
}

func TestProfileCLIUserNamedDefaultRemainsAnOrdinarySubscription(t *testing.T) {
	a, _ := installBackend(t)
	binary := filepath.Join(t.TempDir(), "mihomo")
	if err := os.WriteFile(binary, []byte("fixture core"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.Install(t.Context(), binary, nil); err != nil {
		t.Fatal(err)
	}
	source := writeCLIConfig(t, "rules: ['MATCH,REJECT']\n")
	for _, args := range [][]string{
		{"add", source, "--name", "default"}, {"add", source, "--name", "other"},
		{"apply", "default"}, {"apply", "other"}, {"delete", "other", "--use", "default"},
	} {
		if _, err := runProfileCLI(t, a, args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	raw, err := runProfileCLI(t, a, "show", "default")
	var profile app.ProfileInfo
	if err != nil || json.Unmarshal([]byte(raw), &profile) != nil || profile.Name != "default" || profile.ID == "default" || profile.ID == "" {
		t.Fatal("default label did not resolve to the saved user profile", raw, err)
	}
	s, err := a.Snapshot(t.Context())
	if err != nil || s.Applied != profile.ID || s.AppliedName != profile.Name || len(s.Profiles) != 1 {
		t.Fatal("replacement did not apply the user-provided profile", s, err)
	}
	if _, err := runProfileCLI(t, a, "delete", "default", "--no-profile"); err != nil {
		t.Fatal(err)
	}
	s, err = a.Snapshot(t.Context())
	if err != nil || s.Applied != "" || s.AppliedName != "" || len(s.Profiles) != 0 || len(s.ProfileDetails) != 0 || s.Service.Active {
		t.Fatal("last-profile deletion did not leave the installed instance stopped with no subscription", s, err)
	}
}
