package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
)

func TestKeyHintsPreferenceDefaultsAndPreservesOtherSettings(t *testing.T) {
	a, _ := testApp(t)
	path := filepath.Join(a.Home, "preferences.json")
	write(t, path, []byte(`{"Network":{"source":"official","proxy_mode":"environment"},"Language":"zh-Hans","HasRun":true,"GuideDismissed":true}`))
	p, err := a.LoadPreferences(t.Context())
	if err != nil || p.HideKeyHints {
		t.Fatalf("legacy preference should show hints: %+v, %v", p, err)
	}
	for _, hidden := range []bool{true, false} {
		if err := a.SaveKeyHintsPreference(t.Context(), hidden); err != nil {
			t.Fatal(err)
		}
		other, err := New(a.Home, a.Name)
		if err != nil {
			t.Fatal(err)
		}
		p, err = other.LoadPreferences(t.Context())
		if err != nil || p.HideKeyHints != hidden || p.Language != "zh-Hans" || !p.HasRun || !p.GuideDismissed || p.Network != (downloadnet.Config{Source: "official", ProxyMode: "environment"}) {
			t.Fatalf("save replaced another preference: %+v, %v", p, err)
		}
	}
	before := string(read(t, path))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := a.SaveKeyHintsPreference(ctx, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if string(read(t, path)) != before {
		t.Fatal("canceled save changed preferences")
	}
	write(t, path, []byte("damaged preferences"))
	if err := a.SaveKeyHintsPreference(t.Context(), true); err == nil || string(read(t, path)) != "damaged preferences" {
		t.Fatal("damaged preference file was overwritten")
	}
}
