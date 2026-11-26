package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestCoreManagementShowsVersionsAndConfirmedUptime(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		t.Run(language, func(t *testing.T) {
			m := ready(&fakeBackend{snapshot: app.Snapshot{
				Installed: true, Version: "v1.20.0", RunningVersion: "v1.19.33",
				Service: service.Status{Active: true, State: "active", PID: 42, StartedAt: time.Now().Add(-62 * time.Minute)},
			}})
			m.locale = i18n.New(language)
			m.openManagementWizard(false)
			screen := func() string {
				doc, _, _ := m.wizardDocument(80)
				return ansi.Strip(strings.Join(doc.lines, "\n"))
			}
			details := screen()
			for _, field := range []string{m.tr("wizard.installed_version") + ": v1.20.0", m.tr("wizard.running_version") + ": v1.19.33", m.tr("wizard.uptime") + ": 01:02:"} {
				if !strings.Contains(details, field) {
					t.Fatalf("missing management detail %q in %s", field, details)
				}
			}
			m.snapshot.ServiceError = "service observation failed"
			if strings.Contains(screen(), m.tr("wizard.uptime")) {
				t.Fatal("an unavailable service observation retained an uptime")
			}
		})
	}
}

func TestCoreUpdateRequiresManualCheckAndConfirmation(t *testing.T) {
	checks, upgrades := 0, 0
	download := downloadnet.Config{Method: downloadnet.MethodAccelerated, Prefix: "https://accelerator.example.invalid/"}
	b := &fakeBackend{
		checkUpdate: func(ctx context.Context, channel mihomo.Channel, cfg downloadnet.Config) (app.CoreUpdateCheck, error) {
			checks++
			if channel != "" || cfg != download {
				t.Fatal("check did not use automatic channel and existing network settings")
			}
			return app.CoreUpdateCheck{InstalledVersion: "v1.19.32", LatestVersion: "v1.19.33", Channel: mihomo.ChannelStable, Status: mihomo.UpdateAvailable, CheckedAt: time.Now()}, ctx.Err()
		},
		upgradeCore: func(ctx context.Context, check app.CoreUpdateCheck, cfg downloadnet.Config, report func(mihomo.Progress)) (app.CoreUpgradeResult, error) {
			upgrades++
			if cfg != download {
				t.Fatalf("upgrade used download settings %+v, want %+v", cfg, download)
			}
			if check.InstalledVersion != "v1.19.32" || check.LatestVersion != "v1.19.33" {
				t.Fatal("reviewed target changed")
			}
			report(mihomo.Progress{Stage: "restart", Detail: "fixture readiness"})
			return app.CoreUpgradeResult{PreviousVersion: check.InstalledVersion, InstalledVersion: check.LatestVersion, Restarted: true}, ctx.Err()
		},
	}
	b.snapshot = app.Snapshot{Installed: true, Version: "v1.19.32"}
	m := ready(b)
	directionCleanup(t, &m)
	m.page, m.focus, m.width, m.height = "settings", "network", 160, 45
	m.layout()
	m, _ = update(m, special(tea.KeyEnter))
	if !m.wizard.networkOpen {
		t.Fatal("download settings row did not open the network editor")
	}
	m, _ = update(m, special(tea.KeyLeft))
	m, _ = update(m, special(tea.KeyTab))
	m, _ = update(m, tea.PasteMsg{Content: download.Prefix})
	m, _ = update(m, tea.WindowSizeMsg{Width: 60, Height: 16})
	m, _ = activateWizard(m, "apply-network")
	if m.wizard.networkOpen || m.page != "settings" || m.focus != "network" ||
		!strings.Contains(settingsPaintedTarget(t, m.screen, m.targets, m.focus), m.tr("wizard.method.accelerated")) {
		t.Fatalf("after applying download settings: page %q, focus %q, modal open %t, method %q, error %q",
			m.page, m.focus, m.wizard.networkOpen, m.wizard.cfg.Method, m.wizard.err)
	}
	m.openWizard(false)
	if checks != 0 || upgrades != 0 {
		t.Fatal("opening management queried or upgraded the core")
	}
	m, cmd := activateWizard(m, "check-update")
	if !m.busy || m.wizard.step != stepUpdate {
		t.Fatal("manual check did not show pending state")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	_, buttons, _ := m.wizardDocument(90)
	if checks != 1 || upgrades != 0 || !hasWizardButton(buttons, "upgrade") {
		t.Fatal("check upgraded or did not expose confirmation")
	}
	m, cmd = activateWizard(m, "upgrade")
	if !m.busy || !m.wizard.upgrading || m.wizard.step != stepInstalling {
		t.Fatal("upgrade did not reuse installation progress")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if upgrades != 1 || m.wizard.step != stepResult || !m.wizard.upgradeResult.Restarted || m.wizard.err != "" {
		t.Fatalf("upgrade result: %+v", m.wizard)
	}
}

func hasWizardButton(buttons []wizardButton, id string) bool {
	for _, button := range buttons {
		if button.ID == id {
			return true
		}
	}
	return false
}

func TestCoreUpdateStatusesDoNotAuthorizeUncertainOrDowngradeTargets(t *testing.T) {
	for _, status := range []mihomo.UpdateStatus{mihomo.UpdateCurrent, mihomo.UpdateAhead, mihomo.UpdateIncomparable, mihomo.UpdateNotInstalled, mihomo.UpdateUnavailable} {
		t.Run(string(status), func(t *testing.T) {
			m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Version: "v1.19.32"}})
			m.openWizard(false)
			m.wizard.step, m.wizard.updateMode = stepUpdate, true
			m.wizard.updateCheck = app.CoreUpdateCheck{InstalledVersion: "alpha-1234567", LatestVersion: "alpha-7654321", Channel: mihomo.ChannelAlpha, Status: status, CheckedAt: time.Now()}
			_, buttons, _ := m.wizardDocument(90)
			if hasWizardButton(buttons, "upgrade") || m.wizardActivate("upgrade") != nil || m.busy {
				t.Fatal("non-upgradeable check authorized an upgrade")
			}
		})
	}
}

func TestCoreUpdateCancellationWaitsForRecoveryAndIgnoresLateChecks(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Version: "v1.19.32"}})
	m.openWizard(false)
	m, cmd := activateWizard(m, "check-update")
	late := actionCmd(t, cmd)()
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m, _ = update(m, late)
	if !m.wizard.open || m.wizard.step != stepCore || m.busy || m.wizard.updateCheck.LatestVersion != "" {
		t.Fatal("late cancelled check changed the management dialog")
	}
	m.openWizard(false)
	m.wizard.updateMode, m.wizard.upgrading = true, true
	m.wizard.updateCheck = app.CoreUpdateCheck{InstalledVersion: "v1.19.32", LatestVersion: "v1.19.33", Channel: mihomo.ChannelStable, Status: mihomo.UpdateAvailable}
	cmd = m.runCoreInstall()
	m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.busy || !m.wizard.cancelling || !m.wizard.open {
		t.Fatal("upgrade cancellation exited before recovery completed")
	}
	m, _ = update(m, wizardMsg{id: m.id, kind: "upgrade", err: context.Canceled})
	if m.busy || m.wizard.step != stepResult || m.wizard.err == "" {
		t.Fatal("cancelled recovery result was not shown")
	}
}

func TestSettingsCoreUpdateReturnsToItsTrigger(t *testing.T) {
	const trigger = "core.checkUpdate"
	for _, tc := range []struct {
		name    string
		pending bool
		escape  bool
	}{
		{"back", false, false}, {"escape", false, true},
		{"cancel", true, false}, {"escape while checking", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
			directionCleanup(t, &m)
			m.page, m.focus, m.width, m.height = "settings", trigger, 80, 24
			m.revealContentFocus()
			m.layout()
			if !strings.Contains(m.contextHelp(), m.tr("core_update.check")) {
				t.Fatal("card does not explain the focused update action")
			}
			m, cmd := click(m, findTarget(t, m, trigger))
			if !m.wizard.open || m.wizard.step != stepUpdate || m.job != "check-update" {
				t.Fatal("click did not go directly to the read-only update check")
			}
			result := actionCmd(t, cmd)()
			button := "cancel"
			if !tc.pending {
				m, _ = update(m, result)
				if m.wizard.focus != "upgrade" || m.busy {
					t.Fatal("available update did not wait for confirmation")
				}
				button = "back"
			}
			m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 16})
			if tc.escape {
				m, _ = update(m, special(tea.KeyEscape))
			} else {
				m, _ = click(m, findTarget(t, m, button))
			}
			if tc.pending {
				m, _ = update(m, result)
				if m.wizard.updateCheck.LatestVersion != "" {
					t.Fatal("cancelled check published a late result")
				}
			}
			if m.wizard.open || m.busy || m.page != "settings" || m.focus != trigger || !hasAction(m.targets, trigger) {
				t.Fatal("return did not restore the visible update action in Settings")
			}
			// The management shortcut may start while the card action still has focus.
			m, _ = update(m, key("i"))
			if !m.wizard.open || m.wizard.step != stepCore || m.busy {
				t.Fatal("opening management started another check")
			}
			m, _ = activateWizard(m, "check-update")
			m, _ = click(m, findTarget(t, m, "cancel"))
			if !m.wizard.open || m.wizard.step != stepCore || m.busy {
				t.Fatal("cancelling a check from management did not return to its wizard")
			}
		})
	}
}

func TestSettingsCoreUpdateRequiresReadyInstalledCore(t *testing.T) {
	for _, state := range []string{"unknown", "uninstalled", "busy", "quitting"} {
		t.Run(state, func(t *testing.T) {
			m := ready(&fakeBackend{snapshot: settingsTestSnapshot()})
			m.snapshotReady = state != "unknown"
			m.snapshot.Installed = state != "uninstalled"
			m.busy, m.quitting = state == "busy", state == "quitting"
			id := m.id
			if cmd := m.activate("core.checkUpdate"); cmd != nil || m.id != id || m.wizard.open {
				t.Fatal("unavailable settings action opened or started an update check")
			}
		})
	}
}

func TestCoreUpgradeSuccessRemainsSuccessWhenStatusRefreshFails(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Version: "v1.19.32"}}
	m := ready(b)
	b.snapshotErr = errors.New("fixture status failure")
	m.openWizard(false)
	m.wizard.updateMode, m.wizard.upgrading = true, true
	m.wizard.updateCheck = app.CoreUpdateCheck{InstalledVersion: "v1.19.32", LatestVersion: "v1.19.33", Channel: mihomo.ChannelStable, Status: mihomo.UpdateAvailable}
	cmd := m.runCoreInstall()
	m, _ = update(m, actionCmd(t, cmd)())
	if m.wizard.err != "" || m.wizard.refreshError == "" || m.wizard.upgradeResult.InstalledVersion != "v1.19.33" {
		t.Fatal("status refresh failure disguised a committed upgrade")
	}
}

func TestCoreUpdateBilingualLayoutAndError(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Version: "v1.19.32"}})
		m.locale = i18n.New(language)
		m.openWizard(false)
		m.wizard.updateMode = true
		m.wizard.updateCheck = app.CoreUpdateCheck{InstalledVersion: "v1.19.32", LatestVersion: "v1.19.33", Channel: mihomo.ChannelStable, Status: mihomo.UpdateAvailable, CheckedAt: time.Now()}
		for _, size := range [][2]int{{160, 45}, {110, 30}, {80, 24}, {60, 20}, {40, 12}, {20, 5}} {
			m.width, m.height = size[0], size[1]
			for _, step := range []wizardStep{stepUpdate, stepInstalling, stepResult} {
				m.wizard.step = step
				m.wizard.upgrading = step != stepUpdate
				screen, hits := m.renderWizard("")
				assertCanvas(t, screen, size[0], size[1])
				assertRegions(t, hits, size[0], size[1])
			}
		}
		m.wizard.step, m.wizard.upgrading = stepUpdate, false
		m.wizard.err = "fixture network rejected"
		doc, _, _ := m.wizardDocument(80)
		if !strings.Contains(ansi.Strip(strings.Join(doc.lines, "\n")), "fixture network rejected") {
			t.Fatal("check error not visible")
		}
	}
}

func TestCoreUpdateCompactReviewShowsVersionsStatusAndRestartWarning(t *testing.T) {
	for _, sample := range []struct{ language, status, restart string }{
		{"en", "New stable version available", "running core"},
		{"zh-Hans", "有新的正式版可升级", "内核将重启"},
	} {
		m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Version: "v1.19.32"}})
		m.locale = i18n.New(sample.language)
		m.width, m.height = 40, 16
		m.openWizard(false)
		m.wizard.step, m.wizard.updateMode, m.wizard.focus = stepUpdate, true, "upgrade"
		m.wizard.updateCheck = app.CoreUpdateCheck{InstalledVersion: "v1.19.32", LatestVersion: "v1.19.33", Channel: mihomo.ChannelStable, Status: mihomo.UpdateAvailable, CheckedAt: time.Now()}
		screen, _ := m.renderWizard("")
		plain := ansi.Strip(screen)
		for _, expected := range []string{"v1.19.32", "v1.19.33", sample.status, sample.restart} {
			if !strings.Contains(plain, expected) {
				t.Fatalf("%s compact review hides %q: \n%s", sample.language, expected, plain)
			}
		}
	}
}

func TestCoreUpdateCheckFailureHasRetryFocusAndClosedWizardDoesNotCheckOnNetworkApply(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, Version: "v1.19.32"}, checkUpdate: func(context.Context, mihomo.Channel, downloadnet.Config) (app.CoreUpdateCheck, error) {
		return app.CoreUpdateCheck{}, errors.New("fixture network error")
	}})
	m.openWizard(false)
	cmd := m.checkCoreUpdate()
	m, _ = update(m, actionCmd(t, cmd)())
	if m.wizard.err == "" || m.wizard.focus != "check-update" {
		t.Fatal("failed check did not focus the visible retry action")
	}
	m.wizard.open = false
	m.openNetwork(false)
	m.wizard.draft.Method = "environment"
	if cmd := m.applyNetwork(); cmd != nil || m.busy {
		t.Fatal("network-only settings triggered a stale update operation")
	}
}
