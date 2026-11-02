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
)

func TestCoreUpdateRequiresManualCheckAndConfirmation(t *testing.T) {
	checks, upgrades := 0, 0
	b := &fakeBackend{
		checkUpdate: func(ctx context.Context, channel mihomo.Channel, cfg downloadnet.Config) (app.CoreUpdateCheck, error) {
			checks++
			if channel != "" || cfg.Normalized().Source != "official" {
				t.Fatal("check did not use automatic channel and existing network settings")
			}
			return app.CoreUpdateCheck{InstalledVersion: "v1.19.32", LatestVersion: "v1.19.33", Channel: mihomo.ChannelStable, Status: mihomo.UpdateAvailable, CheckedAt: time.Now()}, ctx.Err()
		},
		upgradeCore: func(ctx context.Context, check app.CoreUpdateCheck, cfg downloadnet.Config, report func(mihomo.Progress)) (app.CoreUpgradeResult, error) {
			upgrades++
			if check.InstalledVersion != "v1.19.32" || check.LatestVersion != "v1.19.33" {
				t.Fatal("reviewed target changed")
			}
			report(mihomo.Progress{Stage: "restart", Detail: "fixture readiness"})
			return app.CoreUpgradeResult{PreviousVersion: check.InstalledVersion, InstalledVersion: check.LatestVersion, Restarted: true}, ctx.Err()
		},
	}
	b.snapshot = app.Snapshot{Installed: true, Version: "v1.19.32"}
	m := ready(b)
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
	m.wizard.draft.ProxyMode = "environment"
	if cmd := m.applyNetwork(); cmd != nil || m.busy {
		t.Fatal("network-only settings triggered a stale update operation")
	}
}
