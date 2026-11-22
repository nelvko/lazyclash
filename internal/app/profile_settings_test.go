package app

import (
	"bytes"
	"testing"

	"github.com/nelvko/lazyclash/internal/configuration"
)

func TestProfileSettingsPreserveAutomaticNameAndProtectUserRename(t *testing.T) {
	a, _ := testApp(t)
	const valid = "rules: ['MATCH,DIRECT']\n"
	server, response, requests := namingServer(t, namingResponse{title: "Original", body: valid})
	a.HTTP = server.Client()
	address := server.URL + "/original?token=private"
	if err := a.AddSubscription(t.Context(), "", address); err != nil {
		t.Fatal(err)
	}
	id := namedProfile(t, a, "Original").ID
	load := func() ProfileSettings {
		t.Helper()
		s, err := a.GetProfileSettings(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	edited := load()
	if edited.Address != address || edited.ID != id {
		t.Fatal("settings did not load the complete remembered address")
	}
	edited.Description = "Office"
	if err := a.SaveProfileSettings(t.Context(), edited); err != nil || requests.Load() != 1 {
		t.Fatal("description edit fetched the source", err)
	}
	requireProfileName(t, a, id, "Original", configuration.NameProvider, "Original")

	stale := load()
	edited = stale
	edited.Address = server.URL + "/new"
	response(namingResponse{title: "New provider", body: valid})
	if err := a.SaveProfileSettings(t.Context(), edited); err != nil || requests.Load() != 2 {
		t.Fatal("replacement source was not read exactly once", err)
	}
	requireProfileName(t, a, id, "New provider", configuration.NameProvider, "New provider")
	stale.Description = "Overwrite new settings"
	if err := a.SaveProfileSettings(t.Context(), stale); err == nil || load().Description != "Office" || requests.Load() != 2 {
		t.Fatal("stale form replaced the updated profile settings")
	}

	edited = load()
	edited.Name, edited.Description, edited.Address = "Personal", "", server.URL+"/again"
	response(namingResponse{title: "Other provider", body: valid})
	if err := a.SaveProfileSettings(t.Context(), edited); err != nil {
		t.Fatal(err)
	}
	if err := a.Refresh(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	requireProfileName(t, a, id, "Personal", configuration.NameCustom, "Personal")
	if got := load(); got.Address != edited.Address || got.Description != "" || requests.Load() != 4 {
		t.Fatal("source, description or request count incorrect")
	}
}

func TestProfileSettingsInvalidSourcePreservesAllFieldsAndAllowsRetry(t *testing.T) {
	a, svc := installedApp(t)
	const valid = "rules: ['MATCH,DIRECT']\n"
	server, response, _ := namingServer(t, namingResponse{title: "Provider", body: valid})
	a.HTTP = server.Client()
	if err := a.AddSubscription(t.Context(), "", server.URL+"/original"); err != nil {
		t.Fatal(err)
	}
	id := namedProfile(t, a, "Provider").ID
	if err := a.Apply(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	_, before, err := a.library().Load()
	if err != nil {
		t.Fatal(err)
	}
	edited, err := a.GetProfileSettings(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	edited.Name, edited.Description, edited.Address = "Private", "Keep draft", server.URL+"/new"
	response(namingResponse{title: "Broken", body: "invalid response"})
	if err := a.SaveProfileSettings(t.Context(), edited); err == nil {
		t.Fatal("invalid replacement source accepted")
	}
	_, after, err := a.library().Load()
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed replacement published part of the edit", err)
	}
	m, generation, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	response(namingResponse{title: "Fixed", body: "rules: ['MATCH,REJECT']\n"})
	if err := a.SaveProfileSettings(t.Context(), edited); err != nil {
		t.Fatal("same form could not retry after validation failure", err)
	}
	if next, target, err := a.load(); err != nil || target == generation || next.SHA == m.SHA || len(svc.actions) != 0 {
		t.Fatal("replacing the source failed to update the stopped instance", err)
	}
	info := requireProfileName(t, a, id, "Private", configuration.NameCustom, "Private")
	if info.Pending || info.Description != "Keep draft" {
		t.Fatal("valid metadata and source were not saved together")
	}
}
