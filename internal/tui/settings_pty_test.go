package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// TestSettingsPTYFixture runs the real TUI using only an opt-in memory backend.
// Setting writes and port reads are observed without storage or services.
func TestSettingsPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_SETTINGS_PTY_REPORT")
	if report == "" {
		t.Skip("explicit Settings PTY report path required")
	}
	b := &settingsPTYBackend{
		coreSettings:    mihomo.DefaultGeneralSettings(),
		runtimeSettings: app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097, AccessRules: mihomo.AccessRules{LANAllowedIPs: mihomo.DefaultLANAllowedIPs}},
		fakeBackend: &fakeBackend{snapshot: app.Snapshot{
			Installed: true, ConfigReady: true, HasRun: true,
			Version: "v1.19.33", RunningVersion: "v1.19.33", MixedPort: 7897, Controller: "127.0.0.1:9097",
			Applied: "settings-pty", AppliedName: "Settings preview", Generation: "settings-pty-generation",
			Service:      service.Status{Active: true, State: "active", Enabled: true, EnableState: "enabled", Linger: true, LingerKnown: true, PID: 4242},
			Mode:         app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
			Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
		}},
	}
	language := os.Getenv("LAZYCLASH_SETTINGS_PTY_LANGUAGE")
	if language == "" {
		language = "en"
	}
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	prefs := b.preferences
	coreChanges := b.coreChanges
	authCounts := b.authCounts
	bindAddress := b.runtimeSettings.BindAddress
	b.mu.Unlock()
	raw, err := json.Marshal(map[string]any{
		"language_saves": b.languages.Load(), "hint_saves": b.hints.Load(), "port_reads": b.portReads.Load(),
		"service_changes": b.controls.Load(), "mode_changes": b.modes.Load(), "port_applies": b.portApplies.Load(),
		"log_streams": b.logStreams.Load(),
		"language":    prefs.Language, "hints_hidden": prefs.HideKeyHints,
		"theme_saves": b.themes.Load(), "theme": prefs.Theme,
		"core_changes": coreChanges,
		"auth_changes": authCounts,
		"bind_address": bindAddress,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

type settingsPTYBackend struct {
	*fakeBackend
	mu                                               sync.Mutex
	preferences                                      app.Preferences
	coreSettings                                     mihomo.GeneralSettings
	runtimeSettings                                  app.RuntimeSettings
	coreChanges                                      [][2]string
	authUsers, pendingAuthUsers                      []app.ProxyAuthUser
	authCounts                                       []int
	languages, hints, themes, portReads, portApplies atomic.Int64
	controls, modes                                  atomic.Int64
	logStreams                                       atomic.Int64
}

func (b *settingsPTYBackend) Snapshot(ctx context.Context) (app.Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	snapshot, err := b.fakeBackend.Snapshot(ctx)
	snapshot.CoreSettings = b.coreSettings
	snapshot.RuntimeSettings = b.runtimeSettings
	return snapshot, err
}

func (b *settingsPTYBackend) SetCoreSetting(ctx context.Context, key, value string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	settings, err := b.coreSettings.With(key, value)
	if err != nil {
		return err
	}
	b.coreSettings = settings
	b.coreChanges = append(b.coreChanges, [2]string{key, value})
	return nil
}

func (b *settingsPTYBackend) ReadPreferences(ctx context.Context) (app.Preferences, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.preferences, ctx.Err()
}

func (b *settingsPTYBackend) SaveLanguagePreference(ctx context.Context, language string) error {
	b.languages.Add(1)
	timer := time.NewTimer(700 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	b.mu.Lock()
	b.preferences.Language = language
	b.mu.Unlock()
	return ctx.Err()
}

func (b *settingsPTYBackend) SaveStartPagePreference(ctx context.Context, page string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	b.preferences.StartPage = page
	return nil
}

func (b *settingsPTYBackend) SaveKeyHintsPreference(ctx context.Context, hidden bool) error {
	b.hints.Add(1)
	b.mu.Lock()
	b.preferences.HideKeyHints = hidden
	b.mu.Unlock()
	return ctx.Err()
}

func (b *settingsPTYBackend) SaveThemePreference(ctx context.Context, theme string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.preferences.Theme = theme
	b.themes.Add(1)
	return ctx.Err()
}

func (b *settingsPTYBackend) Runtime(ctx context.Context) (app.RuntimeSnapshot, error) {
	return app.RuntimeSnapshot{State: "ready", Service: b.snapshot.Service, RunningVersion: b.snapshot.RunningVersion}, ctx.Err()
}

func (b *settingsPTYBackend) BindAddressOptions(context.Context) []app.BindAddressOption {
	if os.Getenv("LAZYCLASH_SETTINGS_PTY_NO_BIND_IPS") == "1" {
		return nil
	}
	return []app.BindAddressOption{
		{Address: "192.0.2.20", Interface: "eth0"},
		{Address: "[2001:db8:1234:5678:abcd:1234:5678:1979]", Interface: "eth0"},
	}
}

func (b *settingsPTYBackend) GetRuntimeSettings(ctx context.Context) (app.RuntimeSettings, error) {
	b.portReads.Add(1)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.runtimeSettings, ctx.Err()
}

func (b *settingsPTYBackend) GetProxyAuth(ctx context.Context) (app.ProxyAuthSettings, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	saved := app.ProxyAuthSettings{SkipAuthPrefixes: b.runtimeSettings.SkipAuthPrefixes}
	for _, user := range b.authUsers {
		saved.Usernames = append(saved.Usernames, user.Username)
	}
	return saved, ctx.Err()
}

func (b *settingsPTYBackend) GetProxyAddress(ctx context.Context, original app.ProxyAuthSettings, username string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, user := range b.authUsers {
		if username == user.Username && slices.Contains(original.Usernames, username) {
			return b.runtimeSettings.ProxyAddress(url.UserPassword(username, user.Password).String()), ctx.Err()
		}
	}
	return "", errors.New("account is no longer available")
}

func (b *settingsPTYBackend) PrepareProxyAuthRules(ctx context.Context, original app.ProxyAuthSettings, users []app.ProxyAuthUser, prefixes string) (app.RuntimeSettingsPlan, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	proposed := make([]app.ProxyAuthUser, len(users))
	names := make([]string, len(users))
	hasPassword := false
	for i, user := range users {
		names[i] = user.Username
		hasPassword = hasPassword || user.Password != ""
		if user.Password == "" {
			for _, saved := range b.authUsers {
				if user.OriginalUsername == saved.Username {
					user.Password = saved.Password
					break
				}
			}
		}
		proposed[i] = user
	}
	next := b.runtimeSettings
	next.ProxyUserCount = len(names)
	next.SkipAuthPrefixes = prefixes
	plan, err := b.fakeBackend.PrepareRuntimeSettingsEdit(ctx, b.runtimeSettings, next)
	if err == nil {
		plan.Changed = hasPassword || !slices.Equal(original.Usernames, names) || prefixes != original.SkipAuthPrefixes
		b.pendingAuthUsers = proposed
	}
	return plan, err
}

func (b *settingsPTYBackend) PrepareRuntimeSettingsEdit(ctx context.Context, current, settings app.RuntimeSettings) (app.RuntimeSettingsPlan, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pendingAuthUsers = nil
	return b.fakeBackend.PrepareRuntimeSettingsEdit(ctx, current, settings)
}

func (b *settingsPTYBackend) ApplyRuntimeSettings(ctx context.Context, plan app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	result, err := b.fakeBackend.ApplyRuntimeSettings(ctx, plan)
	if err == nil {
		b.runtimeSettings = result.Settings
		b.snapshot.Controller, b.snapshot.MixedPort = result.Controller, result.Settings.MixedPort
		if b.pendingAuthUsers != nil {
			b.authUsers, b.pendingAuthUsers = b.pendingAuthUsers, nil
			b.authCounts = append(b.authCounts, len(b.authUsers))
		} else {
			b.portApplies.Add(1)
		}
	}
	return result, err
}

func (b *settingsPTYBackend) Control(ctx context.Context, _ string) error {
	b.controls.Add(1)
	return ctx.Err()
}

func (b *settingsPTYBackend) SetAutostart(ctx context.Context, _ bool) error {
	b.controls.Add(1)
	return ctx.Err()
}

func (b *settingsPTYBackend) SetTUN(ctx context.Context, _ bool) error {
	b.controls.Add(1)
	return ctx.Err()
}

func (b *settingsPTYBackend) SetMode(ctx context.Context, _ string) error {
	b.modes.Add(1)
	return ctx.Err()
}

func (b *settingsPTYBackend) StreamLogs(ctx context.Context, emit func(mihomo.LogEntry)) error {
	b.logStreams.Add(1)
	emit(mihomo.LogEntry{Level: "info", Message: "settings-pty readonly log"})
	<-ctx.Done()
	return ctx.Err()
}

func (b *settingsPTYBackend) StreamRuntime(ctx context.Context, emit func(app.RuntimeEvent)) error {
	return streamRuntimeFixture(ctx, b, emit)
}
