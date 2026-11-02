package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/store"
)

// Preferences never contains controller credentials or subscription URLs.
type Preferences struct {
	Network        downloadnet.Config
	GuideDismissed bool
	HasRun         bool
	Language       string `json:",omitempty"`
	HideKeyHints   bool   `json:",omitempty"`
}

func defaultPreferences() Preferences {
	return Preferences{Network: downloadnet.Config{Source: "official", ProxyMode: "none"}}
}

func (a *App) loadPreferencesLocked() (Preferences, []byte, error) {
	path := filepath.Join(a.Home, "preferences.json")
	raw, err := store.Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultPreferences(), nil, nil
	}
	if err != nil {
		return Preferences{}, nil, err
	}
	var p Preferences
	if json.Unmarshal(raw, &p) != nil {
		return p, nil, diagnostic.New("diagnostic.preferences.invalid", "invalid preferences file; preserving it for inspection")
	}
	if err := rememberableNetwork(p.Network); err != nil {
		return p, nil, diagnostic.New("diagnostic.preferences.networkInvalid", "stored download settings are invalid or contain credentials; file preserved")
	}
	return p, raw, nil
}

func (a *App) LoadPreferences(ctx context.Context) (Preferences, error) {
	unlock, err := store.Lock(ctx, a.Home)
	if err != nil {
		return Preferences{}, err
	}
	defer unlock()
	p, _, err := a.loadPreferencesLocked()
	return p, err
}

// ReadPreferences is for read-only commands: it neither creates storage nor
// runs recovery. Writers publish preferences atomically.
func (a *App) ReadPreferences(ctx context.Context) (Preferences, error) {
	if err := ctx.Err(); err != nil {
		return Preferences{}, err
	}
	p, _, err := a.loadPreferencesLocked()
	return p, err
}

func rememberableNetwork(cfg downloadnet.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	for _, text := range []string{cfg.Prefix, cfg.Mirror, cfg.ProxyURL} {
		if text == "" {
			continue
		}
		u, err := url.Parse(text)
		if err != nil || u.Hostname() == "" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return diagnostic.New("diagnostic.preferences.credentials", "download settings containing credentials or query parameters are session-only and cannot be remembered")
		}
		// Restrict remembered path components to public release/proxy vocabulary.
		// Arbitrary paths (which may themselves be tokens) remain session-only.
		for _, part := range strings.Split(u.Path, "/") {
			switch part {
			case "", "proxy", "github", "github.com", "gh", "MetaCubeX", "mihomo", "releases", "release", "download", "downloads", "{version}", "{asset}":
			default:
				return diagnostic.New("diagnostic.preferences.privatePath", "download URLs containing private or unrecognized path components are session-only and cannot be remembered")
			}
		}
	}
	return nil
}

func (a *App) savePreferencesLocked(p Preferences, old []byte) error {
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return store.Replace(filepath.Join(a.Home, "preferences.json"), raw, old, 0600)
}

func (a *App) editPreferences(ctx context.Context, edit func(*Preferences) error) error {
	unlock, err := store.Lock(ctx, a.Home)
	if err != nil {
		return err
	}
	defer unlock()
	p, old, err := a.loadPreferencesLocked()
	if err != nil {
		return err
	}
	if err := edit(&p); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.savePreferencesLocked(p, old)
}

func (a *App) SaveNetworkPreferences(ctx context.Context, cfg downloadnet.Config) error {
	cfg = cfg.Normalized()
	if cfg.Source != "prefix" {
		cfg.Prefix = ""
	}
	if cfg.Source != "mirror" {
		cfg.Mirror = ""
	}
	if cfg.ProxyMode != "custom" {
		cfg.ProxyURL = ""
	}
	if err := rememberableNetwork(cfg); err != nil {
		return err
	}
	return a.editPreferences(ctx, func(p *Preferences) error { p.Network = cfg; return nil })
}

func (a *App) SetGuideDismissed(ctx context.Context, dismissed bool) error {
	return a.editPreferences(ctx, func(p *Preferences) error { p.GuideDismissed = dismissed; return nil })
}

// SaveLanguagePreference changes presentation only and preserves other preferences.
func (a *App) SaveLanguagePreference(ctx context.Context, language string) error {
	if language != "en" && language != "zh-Hans" {
		return diagnostic.New("language.unsupported", "Language must be en or zh-Hans")
	}
	return a.editPreferences(ctx, func(p *Preferences) error { p.Language = language; return nil })
}

// SaveKeyHintsPreference changes presentation without replacing other preferences.
func (a *App) SaveKeyHintsPreference(ctx context.Context, hidden bool) error {
	return a.editPreferences(ctx, func(p *Preferences) error { p.HideKeyHints = hidden; return nil })
}

func (a *App) markHasRunLocked(ctx context.Context) error {
	p, old, err := a.loadPreferencesLocked()
	if err != nil || p.HasRun {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.HasRun = true
	return a.savePreferencesLocked(p, old)
}
