package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
)

func TestLanguagePreferencePersistsAndPreservesOtherPreferences(t *testing.T) {
	a, _ := testApp(t)
	network := downloadnet.Config{Source: "official", ProxyMode: "environment"}
	if err := a.SaveNetworkPreferences(t.Context(), network); err != nil {
		t.Fatal(err)
	}
	if err := a.SetGuideDismissed(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	for _, language := range []string{"zh-Hans", "en"} {
		if err := a.SaveLanguagePreference(t.Context(), language); err != nil {
			t.Fatal(err)
		}
		other, err := New(a.Home, a.Name)
		if err != nil {
			t.Fatal(err)
		}
		p, err := other.LoadPreferences(t.Context())
		if err != nil || p.Language != language || p.Network != network || !p.GuideDismissed {
			t.Fatalf("preferences=%+v err=%v", p, err)
		}
		info, err := os.Stat(filepath.Join(a.Home, "preferences.json"))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("permissions=%v err=%v", info, err)
		}
	}
	before := read(t, filepath.Join(a.Home, "preferences.json"))
	if err := a.SaveLanguagePreference(t.Context(), "invalid"); err == nil {
		t.Fatal("unsupported language saved")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := a.SaveLanguagePreference(ctx, "zh-Hans"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if string(read(t, filepath.Join(a.Home, "preferences.json"))) != string(before) {
		t.Fatal("failed save changed preferences")
	}
}

func TestLanguagePreferenceAcceptsLegacyFileAndPreservesDamagedFile(t *testing.T) {
	a, _ := testApp(t)
	path := filepath.Join(a.Home, "preferences.json")
	write(t, path, []byte(`{"Network":{"Source":"official","ProxyMode":"none"},"HasRun":true}`))
	p, err := a.LoadPreferences(t.Context())
	if err != nil || p.Language != "" || !p.HasRun {
		t.Fatalf("legacy=%+v err=%v", p, err)
	}
	if err := a.SaveLanguagePreference(t.Context(), "zh-Hans"); err != nil {
		t.Fatal(err)
	}
	write(t, path, []byte("damaged user preferences"))
	if err := a.SaveLanguagePreference(t.Context(), "en"); err == nil {
		t.Fatal("damaged preferences overwritten")
	}
	if string(read(t, path)) != "damaged user preferences" {
		t.Fatal("damaged file changed")
	}
}
