package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// Explicit preview export; these are model views rather than native screenshots.
func TestExportCoreUpdateViews(t *testing.T) {
	dir := os.Getenv("LAZYCLASH_CORE_UPDATE_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set LAZYCLASH_CORE_UPDATE_PREVIEW_DIR to export terminal model previews")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, language := range []string{"en", "zh-Hans"} {
		for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}, {40, 16}} {
			m := ready(&fakeBackend{})
			m.locale = i18n.New(language)
			m.noColor, m.colorProfile = false, colorprofile.TrueColor
			m.width, m.height = size[0], size[1]
			m.openWizard(false)
			m.wizard.updateMode = true
			m.wizard.updateCheck = app.CoreUpdateCheck{InstalledVersion: "v1.19.32", LatestVersion: "v1.19.33", Channel: mihomo.ChannelStable, Status: mihomo.UpdateAvailable, CheckedAt: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
			m.wizard.upgradeResult = app.CoreUpgradeResult{PreviousVersion: "v1.19.32", InstalledVersion: "v1.19.33", Restarted: true}
			m.wizard.progress = mihomo.Progress{Stage: "download", Downloaded: 12 << 20, Total: 20 << 20, BytesPerSecond: 2 << 20, Detail: "Downloading verified release bytes"}
			for _, step := range []wizardStep{stepUpdate, stepInstalling, stepResult} {
				m.wizard.step, m.wizard.upgrading = step, step != stepUpdate
				m.wizard.focus = "upgrade"
				if step == stepInstalling {
					m.wizard.focus = "cancel"
				} else if step == stepResult {
					m.wizard.focus = "done"
				}
				screen, _ := m.renderWizard("")
				path := filepath.Join(dir, fmt.Sprintf("%s-%dx%d-%s.ansi", language, size[0], size[1], step))
				if err := os.WriteFile(path, []byte(screen), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

// A real PTY can run this opt-in fixture through a test binary. It uses no
// app storage, network, executable or system service, and writes only a report.
func TestCoreUpdatePTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_CORE_UPDATE_PTY_REPORT")
	if report == "" {
		t.Skip("explicit PTY fixture report path required")
	}
	var checks, upgrades atomic.Int32
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.32", Service: service.Status{State: "inactive"}, Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}}}}
	b.checkUpdate = func(ctx context.Context, channel mihomo.Channel, cfg downloadnet.Config) (app.CoreUpdateCheck, error) {
		checks.Add(1)
		select {
		case <-ctx.Done():
			return app.CoreUpdateCheck{}, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
		return app.CoreUpdateCheck{InstalledVersion: "v1.19.32", LatestVersion: "v1.19.33", Channel: mihomo.ChannelStable, Status: mihomo.UpdateAvailable, CheckedAt: time.Now()}, nil
	}
	b.upgradeCore = func(ctx context.Context, check app.CoreUpdateCheck, cfg downloadnet.Config, progress func(mihomo.Progress)) (app.CoreUpgradeResult, error) {
		upgrades.Add(1)
		progress(mihomo.Progress{Stage: "download", Downloaded: 1024, Total: 4096, Detail: "Fixture transfer; cancellation is safe"})
		<-ctx.Done()
		progress(mihomo.Progress{Stage: "rollback", Detail: "Fixture recovery in progress"})
		time.Sleep(150 * time.Millisecond)
		return app.CoreUpgradeResult{}, ctx.Err()
	}
	language := os.Getenv("LAZYCLASH_CORE_UPDATE_PTY_LANGUAGE")
	if language == "" {
		language = "en"
	}
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]int32{"checks": checks.Load(), "upgrades": upgrades.Load()})
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
