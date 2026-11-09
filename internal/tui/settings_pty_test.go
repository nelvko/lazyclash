package tui

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/service"
)

// TestSettingsPTYFixture runs the real TUI using only an opt-in memory backend.
// Preference writes and port reads are observed without storage or services.
func TestSettingsPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_SETTINGS_PTY_REPORT")
	if report == "" {
		t.Skip("explicit Settings PTY report path required")
	}
	b := &settingsPTYBackend{fakeBackend: &fakeBackend{snapshot: app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true,
		Version: "v1.19.33", RunningVersion: "v1.19.33", Controller: "127.0.0.1:9097",
		Applied: "settings-pty", AppliedName: "Settings preview", Generation: "settings-pty-generation",
		Service:      service.Status{Active: true, State: "active", Enabled: true, EnableState: "enabled", Linger: true, LingerKnown: true, PID: 4242},
		Mode:         app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
		Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
	}}}
	language := os.Getenv("LAZYCLASH_SETTINGS_PTY_LANGUAGE")
	if language == "" {
		language = "en"
	}
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	prefs := b.preferences
	b.mu.Unlock()
	raw, err := json.Marshal(map[string]any{
		"language_saves": b.languages.Load(), "hint_saves": b.hints.Load(), "port_reads": b.portReads.Load(),
		"service_changes": b.controls.Load(), "mode_changes": b.modes.Load(), "port_applies": b.portApplies.Load(),
		"language": prefs.Language, "hints_hidden": prefs.HideKeyHints,
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
	mu                                       sync.Mutex
	preferences                              app.Preferences
	languages, hints, portReads, portApplies atomic.Int64
	controls, modes                          atomic.Int64
}

func (b *settingsPTYBackend) LoadPreferences(ctx context.Context) (app.Preferences, error) {
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

func (b *settingsPTYBackend) SaveKeyHintsPreference(ctx context.Context, hidden bool) error {
	b.hints.Add(1)
	b.mu.Lock()
	b.preferences.HideKeyHints = hidden
	b.mu.Unlock()
	return ctx.Err()
}

func (b *settingsPTYBackend) Runtime(ctx context.Context) (app.RuntimeSnapshot, error) {
	return app.RuntimeSnapshot{State: "ready", Service: b.snapshot.Service, RunningVersion: b.snapshot.RunningVersion}, ctx.Err()
}

func (b *settingsPTYBackend) GetRuntimeSettings(ctx context.Context) (app.RuntimeSettings, error) {
	b.portReads.Add(1)
	return app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}, ctx.Err()
}

func (b *settingsPTYBackend) ApplyRuntimeSettings(ctx context.Context, _ app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
	b.portApplies.Add(1)
	return app.RuntimeSettingsResult{}, ctx.Err()
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

func (b *settingsPTYBackend) StreamRuntime(ctx context.Context, emit func(app.RuntimeEvent)) error {
	return streamRuntimeFixture(ctx, b, emit)
}
