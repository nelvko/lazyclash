package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveEditorPreferencePreservesOtherPreferencesAndValidates(t *testing.T) {
	a := &App{Home: t.TempDir()}
	program := filepath.Join(t.TempDir(), "test editor")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := a.SaveLanguagePreference(ctx, "zh-Hans"); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveEditorPreference(ctx, program); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveEditorPreference(ctx, "not-installed-lc-test"); err == nil {
		t.Fatal("accepted unavailable editor")
	}
	prefs, err := a.LoadPreferences(ctx)
	if err != nil || prefs.Editor != program || prefs.Language != "zh-Hans" {
		t.Fatalf("preferences: %+v (%v)", prefs, err)
	}
	if err := a.SaveEditorPreference(ctx, ""); err != nil {
		t.Fatal(err)
	}
	prefs, err = a.LoadPreferences(ctx)
	if err != nil || prefs.Editor != "" {
		t.Fatal("could not restore automatic selection")
	}
}
