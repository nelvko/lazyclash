package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
	"gopkg.in/yaml.v3"
)

var profileName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,47}$`)

func (a *App) source(ctx context.Context, source string) ([]byte, error) {
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		req, err := http.NewRequestWithContext(ctx, "GET", source, nil)
		if err != nil {
			return nil, errors.New("invalid subscription URL")
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		defer tr.CloseIdleConnections()
		client := &http.Client{Timeout: 45 * time.Second, Transport: tr}
		if a.HTTP != nil {
			copied := *a.HTTP
			client = &copied
		}
		client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			next.Header.Del("Referer")
			if len(via) >= 10 {
				return errors.New("too many subscription redirects")
			}
			if via[0].URL.Scheme == "https" && next.URL.Scheme != "https" {
				return errors.New("subscription redirect would downgrade HTTPS")
			}
			return nil
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("subscription download failed (URL withheld)")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("subscription download returned HTTP %d", resp.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, mihomo.MaxConfigSize+1))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("cannot read subscription response")
		}
		if len(raw) > mihomo.MaxConfigSize {
			return nil, errors.New("subscription exceeds 8 MiB")
		}
		return raw, nil
	}
	if !filepath.IsAbs(source) {
		return nil, errors.New("subscription source must be an HTTP(S) URL or absolute file path")
	}
	raw, err := store.Read(source)
	if err != nil {
		return nil, errors.New("cannot read subscription file")
	}
	return raw, nil
}
func (a *App) validateConfig(ctx context.Context, m manifest, config []byte) error {
	stage, err := os.MkdirTemp(a.Home, ".validate-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	path := filepath.Join(stage, "config.yaml")
	if err := store.Replace(path, config, nil, 0600); err != nil {
		return err
	}
	bin, _, _ := a.paths()
	return a.Validate(ctx, bin, path, stage)
}
func (a *App) Add(ctx context.Context, name, source string) error {
	if !profileName.MatchString(name) || name == "default" {
		return errors.New("profile name must use 1–48 letters, digits, _ or -; default is reserved")
	}
	return a.locked(ctx, func() error {
		m, _, err := a.load()
		installed := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if !installed {
			m = manifest{Controller: "127.0.0.1:9097", Secret: "structure-validation-only", Port: 7897}
		}
		profiles := filepath.Join(a.Home, "profiles")
		if err := store.Dir(profiles); err != nil {
			return err
		}
		path := filepath.Join(profiles, name+".yaml")
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.New("profile already exists; choose a new name")
		}
		raw, err := a.source(ctx, source)
		if err != nil {
			return err
		}
		effective, err := mihomo.Effective(raw, m.Controller, m.Secret, m.Port)
		if err != nil {
			return err
		}
		if installed {
			if err := a.validateConfig(ctx, m, effective); err != nil {
				return err
			}
		} else if err := validateProfileStructure(effective); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !installed {
			marker := filepath.Join(profiles, name+".unvalidated")
			if err := store.Replace(marker, []byte(digest(raw)), nil, 0600); err != nil {
				return err
			}
			if err := store.Replace(path, raw, nil, 0600); err != nil {
				return errors.Join(err, os.Remove(marker))
			}
			return nil
		}
		return store.Replace(path, raw, nil, 0600)
	})
}
func (a *App) Apply(ctx context.Context, name string) error {
	if !profileName.MatchString(name) {
		return errors.New("invalid profile name")
	}
	return a.locked(ctx, func() error {
		if err := a.requireService(); err != nil {
			return err
		}
		m, old, err := a.load()
		if err != nil {
			return err
		}
		bin, config, data := a.paths()
		if err := a.Service.Verify(ctx, bin, config, data); err != nil {
			return err
		}
		status, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		var cfg []byte
		if name == "default" {
			cfg, err = mihomo.Default(m.Controller, m.Secret, m.Port)
		} else {
			if err := store.Dir(filepath.Join(a.Home, "profiles")); err != nil {
				return err
			}
			raw, e := store.Read(filepath.Join(a.Home, "profiles", name+".yaml"))
			if e != nil {
				return e
			}
			cfg, err = mihomo.Effective(raw, m.Controller, m.Secret, m.Port)
		}
		if err != nil {
			return err
		}
		if err := a.validateConfig(ctx, m, cfg); err != nil {
			return err
		}
		m.Applied = name
		base := filepath.Join(a.Home, "installation")
		next, err := a.writeGeneration(base, m, cfg)
		if err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				_ = os.RemoveAll(filepath.Join(base, next))
			}
		}()
		if err := ctx.Err(); err != nil {
			return err
		}
		p := pending{Old: old, New: next, Active: status.Active}
		raw, _ := json.Marshal(p)
		journal := filepath.Join(a.Home, "apply-pending.json")
		if err := store.Replace(journal, raw, nil, 0600); err != nil {
			return err
		}
		rollback := func(cause error) error {
			c, cancel := cleanupContext()
			defer cancel()
			err := a.recover(c)
			if err != nil {
				committed = true
			}
			return errors.Join(cause, err)
		}
		if err := store.Switch(filepath.Join(base, "current"), next, old); err != nil {
			return rollback(err)
		}
		if status.Active {
			if err := a.restartReady(ctx, m); err != nil {
				return rollback(err)
			}
		}
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		if err := os.Remove(journal); err != nil {
			return rollback(err)
		}
		committed = true
		return a.clearValidationMarker(name)
	})
}

func (a *App) clearValidationMarker(name string) error {
	if name == "default" {
		return nil
	}
	marker := filepath.Join(a.Home, "profiles", name+".unvalidated")
	expected, err := store.Read(marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	raw, err := store.Read(filepath.Join(a.Home, "profiles", name+".yaml"))
	if err != nil {
		return err
	}
	if string(expected) != digest(raw) {
		return errors.New("configuration applied, but validation marker changed; preserving it")
	}
	return os.Remove(marker)
}

// This catches malformed Mihomo containers before an executable is available;
// full field semantics and outbound references still require engine validation.
func validateProfileStructure(effective []byte) error {
	var cfg map[string]any
	if yaml.Unmarshal(effective, &cfg) != nil {
		return errors.New("invalid Mihomo YAML structure")
	}
	for _, key := range []string{"proxies", "proxy-groups", "rules"} {
		value := cfg[key]
		if value == nil {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("Mihomo %s must be a YAML list", key)
		}
		for _, item := range items {
			if key == "rules" {
				if _, ok := item.(string); !ok {
					return errors.New("Mihomo rules must contain strings")
				}
				continue
			}
			entry, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("Mihomo %s entries must be mappings", key)
			}
			name, nameOK := entry["name"].(string)
			kind, kindOK := entry["type"].(string)
			if !nameOK || !kindOK || name == "" || kind == "" {
				return fmt.Errorf("Mihomo %s entries require a name and type", key)
			}
		}
	}
	for _, key := range []string{"proxy-providers", "rule-providers"} {
		if value := cfg[key]; value != nil {
			if _, ok := value.(map[string]any); !ok {
				return fmt.Errorf("Mihomo %s must be a mapping", key)
			}
		}
	}
	return nil
}
