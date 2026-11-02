package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestInstallUsesSharedStableCheckAndPinsItsVersionWithoutAnotherConfirmation(t *testing.T) {
	var events []string
	cfg := downloadnet.Config{Method: "environment"}
	b := &fakeBackend{prefs: app.Preferences{Network: cfg}}
	b.checkUpdate = func(ctx context.Context, channel mihomo.Channel, used downloadnet.Config) (app.CoreUpdateCheck, error) {
		events = append(events, "check")
		if channel != mihomo.ChannelStable || used != cfg {
			t.Fatal("installation did not reuse the stable update checker and selected network")
		}
		return app.CoreUpdateCheck{LatestVersion: "v1.19.33", Status: mihomo.UpdateNotInstalled, Channel: channel, CheckedAt: time.Now()}, ctx.Err()
	}
	b.resolve = func(ctx context.Context, selection string, used downloadnet.Config) (mihomo.Release, error) {
		events = append(events, "resolve:"+selection)
		if selection != "v1.19.33" || used != cfg {
			t.Fatal("installation looked up mutable latest or changed the selected network")
		}
		return testRelease(), ctx.Err()
	}
	b.prepare = func(ctx context.Context, release mihomo.Release, local string, reinstall bool) (app.InstallPlan, error) {
		events = append(events, "prepare")
		if release.Version != "v1.19.33" || local != "" || reinstall {
			t.Fatal("preflight changed the selected release")
		}
		return app.InstallPlan{Release: release, ActualPath: "/tmp/mihomo", Operation: "install"}, ctx.Err()
	}
	b.install = func(ctx context.Context, plan app.InstallPlan, used downloadnet.Config, report func(mihomo.Progress)) (app.InstallResult, error) {
		events = append(events, "install")
		if plan.Release.Version != "v1.19.33" || used != cfg {
			t.Fatal("installation lost its fixed release or selected network")
		}
		return app.InstallResult{InstalledVersion: plan.Release.Version, Operation: plan.Operation}, ctx.Err()
	}
	m := ready(b)
	cmd := m.openManagementWizard(false)
	m, _ = update(m, actionCmd(t, cmd)())
	if strings.Join(events, ",") != "check" || m.busy || m.wizard.step != stepVersion {
		t.Fatal("entering installation downloaded, prepared or installed a core")
	}
	m, cmd = activateWizard(m, "install")
	for _, job := range []string{"resolve", "prepare", "install"} {
		if !m.busy || m.job != job || m.wizard.step == stepConfirm {
			t.Fatalf("one install action did not proceed to %s without a confirmation: step=%s job=%s", job, m.wizard.step, m.job)
		}
		id := m.id
		if duplicate := m.wizardActivate("install"); duplicate != nil || m.id != id {
			t.Fatal("pending installation accepted a duplicate install action")
		}
		m, cmd = update(m, actionCmd(t, cmd)())
	}
	if strings.Join(events, ",") != "check,resolve:v1.19.33,prepare,install" || m.wizard.step != stepResult || m.wizard.err != "" {
		t.Fatalf("unexpected installation sequence: %v, step=%s error=%s", events, m.wizard.step, m.wizard.err)
	}
}

func TestInstallSpecificAndLocalSourcesNeedOnlyOneInstallAction(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "specific", true: "local"}[local], func(t *testing.T) {
			checks, resolves, installs := 0, 0, 0
			b := &fakeBackend{}
			b.checkUpdate = func(context.Context, mihomo.Channel, downloadnet.Config) (app.CoreUpdateCheck, error) {
				checks++
				return app.CoreUpdateCheck{}, errors.New("not needed for this source")
			}
			b.resolve = func(_ context.Context, version string, _ downloadnet.Config) (mihomo.Release, error) {
				resolves++
				if local || version != "v1.19.33" {
					t.Fatal("source resolved through the wrong path")
				}
				return testRelease(), nil
			}
			b.prepare = func(_ context.Context, release mihomo.Release, path string, _ bool) (app.InstallPlan, error) {
				if local && path != "/tmp/local-mihomo" || !local && path != "" {
					t.Fatal("local executable path changed")
				}
				return app.InstallPlan{Release: testRelease(), Local: path, Operation: "install"}, nil
			}
			b.install = func(_ context.Context, _ app.InstallPlan, _ downloadnet.Config, _ func(mihomo.Progress)) (app.InstallResult, error) {
				installs++
				return app.InstallResult{InstalledVersion: "v1.19.33"}, nil
			}
			m := ready(b)
			m.openManagementWizard(true)
			m.wizard.local.SetValue("/tmp/local-mihomo")
			if !local {
				m.wizard.localMode, m.wizard.specific = false, true
				m.wizard.version.SetValue("v1.19.33")
			}
			m, cmd := activateWizard(m, "install")
			if !local {
				m, cmd = update(m, actionCmd(t, cmd)())
			}
			m, cmd = update(m, actionCmd(t, cmd)())
			if m.wizard.step != stepInstalling || !m.busy || installs != 0 {
				t.Fatal("preflight required a second install action")
			}
			m, _ = update(m, actionCmd(t, cmd)())
			if checks != 0 || installs != 1 || resolves != map[bool]int{false: 1, true: 0}[local] || m.wizard.step != stepResult {
				t.Fatal("source performed an unnecessary query or installation")
			}
		})
	}
}

func TestInstallPreflightBlockAndFailureNeverDownload(t *testing.T) {
	for _, failure := range []string{"blocked", "error"} {
		t.Run(failure, func(t *testing.T) {
			installs := 0
			b := &fakeBackend{prepare: func(context.Context, mihomo.Release, string, bool) (app.InstallPlan, error) {
				if failure == "error" {
					return app.InstallPlan{}, errors.New("preflight rejected")
				}
				return app.InstallPlan{Release: testRelease(), BlockedReason: "service unavailable"}, nil
			}, install: func(context.Context, app.InstallPlan, downloadnet.Config, func(mihomo.Progress)) (app.InstallResult, error) {
				installs++
				return app.InstallResult{}, nil
			}}
			m := ready(b)
			m.openManagementWizard(true)
			m.wizard.local.SetValue("/tmp/mihomo")
			m, cmd := activateWizard(m, "install")
			m, _ = update(m, actionCmd(t, cmd)())
			if m.busy || m.wizard.installRequested || installs != 0 {
				t.Fatal("failed preflight still authorized a download")
			}
			if failure == "blocked" {
				_, buttons, _ := m.wizardDocument(80)
				if m.wizard.step != stepConfirm || hasWizardButton(buttons, "install") {
					t.Fatal("blocked plan exposed an executable install action")
				}
			} else if m.wizard.err == "" || m.wizard.focus != "local" {
				t.Fatal("local preflight failure was hidden or left focus on an unavailable action")
			}
		})
	}
}

func TestInstallPendingPreflightCannotContinueAfterLeavingOrEditing(t *testing.T) {
	for _, action := range []string{"escape", "network", "edit"} {
		t.Run(action, func(t *testing.T) {
			installs := 0
			b := &fakeBackend{install: func(context.Context, app.InstallPlan, downloadnet.Config, func(mihomo.Progress)) (app.InstallResult, error) {
				installs++
				return app.InstallResult{}, nil
			}}
			m := ready(b)
			m.openManagementWizard(true)
			m.wizard.local.SetValue("/tmp/mihomo")
			m, cmd := activateWizard(m, "install")
			late := actionCmd(t, cmd)()
			switch action {
			case "escape":
				m, _ = update(m, special(tea.KeyEscape))
			case "network":
				m, _ = activateWizard(m, "network")
			case "edit":
				m.wizard.focus = "local"
				m, _ = update(m, key("x"))
			}
			m, next := update(m, late)
			if m.wizard.installRequested || installs != 0 || m.job == "install" || next != nil && m.busy {
				t.Fatal("a stale preflight continued after the user changed or left installation")
			}
		})
	}
}

func TestInstallEmptyVersionFailsAfterOneSharedCheck(t *testing.T) {
	for _, requested := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial", true: "explicit"}[requested], func(t *testing.T) {
			checks, resolves := 0, 0
			b := &fakeBackend{checkUpdate: func(context.Context, mihomo.Channel, downloadnet.Config) (app.CoreUpdateCheck, error) {
				checks++
				return app.CoreUpdateCheck{Channel: mihomo.ChannelStable, Status: mihomo.UpdateNotInstalled}, nil
			}, resolve: func(context.Context, string, downloadnet.Config) (mihomo.Release, error) {
				resolves++
				return testRelease(), nil
			}}
			m := ready(b)
			var cmd tea.Cmd
			if requested {
				m.openManagementWizard(true)
				m.wizard.localMode = false
				m, cmd = activateWizard(m, "install")
			} else {
				cmd = m.openManagementWizard(false)
			}
			m, _ = update(m, actionCmd(t, cmd)())
			if checks != 1 || resolves != 0 || m.busy || m.wizard.installRequested || m.wizard.err == "" || m.wizard.focus != "resolve" {
				t.Fatal("empty remote version caused a repeated query, a download, or silent success")
			}
		})
	}
}

func TestInstallNetworkApplyRechecksWithoutRetainingPendingInstallation(t *testing.T) {
	for _, stage := range []string{"resolve", "prepare"} {
		t.Run(stage, func(t *testing.T) {
			checks, resolves, installs := 0, 0, 0
			b := &fakeBackend{checkUpdate: func(_ context.Context, _ mihomo.Channel, cfg downloadnet.Config) (app.CoreUpdateCheck, error) {
				checks++
				if checks == 2 && cfg.Method != "environment" {
					t.Fatal("network apply did not use the changed connection")
				}
				return app.CoreUpdateCheck{LatestVersion: "v1.19.33", Channel: mihomo.ChannelStable, Status: mihomo.UpdateNotInstalled, CheckedAt: time.Now()}, nil
			}, resolve: func(context.Context, string, downloadnet.Config) (mihomo.Release, error) {
				resolves++
				return testRelease(), nil
			}, install: func(context.Context, app.InstallPlan, downloadnet.Config, func(mihomo.Progress)) (app.InstallResult, error) {
				installs++
				return app.InstallResult{}, nil
			}}
			m := ready(b)
			initial := m.openManagementWizard(false)
			m, _ = update(m, actionCmd(t, initial)())
			m, cmd := activateWizard(m, "install")
			if stage == "prepare" {
				m, cmd = update(m, actionCmd(t, cmd)())
			}
			late := actionCmd(t, cmd)()
			m, _ = activateWizard(m, "network")
			m.wizard.draft.Method = "environment"
			m, check := activateWizard(m, "apply-network")
			if !m.busy || m.job != "check-update" || m.wizard.installRequested {
				t.Fatal("network apply retained an earlier install authorization")
			}
			m, _ = update(m, late)
			m, _ = update(m, actionCmd(t, check)())
			if checks != 2 || resolves != 1 || installs != 0 || m.busy || m.wizard.step != stepVersion || m.wizard.installRequested {
				t.Fatal("network recheck or stale result resumed an installation")
			}
		})
	}
}

func TestInstallRemoteFailureCanSwitchToLocalWithoutRetainingTheError(t *testing.T) {
	m := ready(&fakeBackend{checkUpdate: func(context.Context, mihomo.Channel, downloadnet.Config) (app.CoreUpdateCheck, error) {
		return app.CoreUpdateCheck{}, errors.New("fixture remote unavailable")
	}})
	cmd := m.openManagementWizard(false)
	m, _ = update(m, actionCmd(t, cmd)())
	if m.wizard.err == "" {
		t.Fatal("remote failure missing")
	}
	m, _ = activateWizard(m, "local-mode")
	if !m.wizard.localMode || m.wizard.focus != "local" || m.wizard.err != "" || m.wizard.failedKind != "" || m.busy {
		t.Fatal("switching to a local source retained the failed remote operation")
	}
}

func TestInstalledCoreManualReplacementStillReviewsThePlan(t *testing.T) {
	installs := 0
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Version: "v1.20.0"}}
	b.prepare = func(ctx context.Context, release mihomo.Release, local string, _ bool) (app.InstallPlan, error) {
		return app.InstallPlan{Release: testRelease(), Local: local, ExistingVersion: "v1.20.0", Operation: "update", ActualPath: "/tmp/mihomo"}, ctx.Err()
	}
	b.install = func(context.Context, app.InstallPlan, downloadnet.Config, func(mihomo.Progress)) (app.InstallResult, error) {
		installs++
		return app.InstallResult{}, nil
	}
	m := ready(b)
	if cmd := m.openManagementWizard(false); cmd != nil || m.busy || m.wizard.step != stepCore {
		t.Fatal("opening installed-core management performed a lookup or replacement")
	}
	m.openManagementWizard(true)
	m.wizard.local.SetValue("/tmp/older-mihomo")
	m, cmd := activateWizard(m, "install")
	m, _ = update(m, actionCmd(t, cmd)())
	_, buttons, _ := m.wizardDocument(80)
	if installs != 0 || m.busy || m.wizard.step != stepConfirm || m.wizard.plan.ExistingVersion != "v1.20.0" || !hasWizardButton(buttons, "install") {
		t.Fatal("manual replacement of an installed core skipped its impact review")
	}
}

// Explicit real-PTY fixture: all backend activity is in memory. No executable,
// network, managed installation or system service is touched.
func TestInstallFlowPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_INSTALL_PTY_REPORT")
	if report == "" {
		t.Skip("explicit PTY report path required")
	}
	var checks, resolves, prepares, installs, starts atomic.Int32
	scenario := os.Getenv("LAZYCLASH_INSTALL_PTY_SCENARIO")
	b := &fakeBackend{}
	b.control = func(ctx context.Context, action string) error {
		if action == "start" {
			starts.Add(1)
		}
		return ctx.Err()
	}
	b.checkUpdate = func(ctx context.Context, channel mihomo.Channel, _ downloadnet.Config) (app.CoreUpdateCheck, error) {
		attempt := checks.Add(1)
		select {
		case <-ctx.Done():
			return app.CoreUpdateCheck{}, ctx.Err()
		case <-time.After(120 * time.Millisecond):
		}
		if channel != mihomo.ChannelStable {
			return app.CoreUpdateCheck{}, errors.New("fixture expected stable channel")
		}
		if scenario == "check-retry" && attempt == 1 {
			return app.CoreUpdateCheck{}, errors.New("Fixture version check failed")
		}
		return app.CoreUpdateCheck{LatestVersion: "v1.19.33", Channel: channel, Status: mihomo.UpdateNotInstalled, CheckedAt: time.Now()}, nil
	}
	b.resolve = func(ctx context.Context, version string, _ downloadnet.Config) (mihomo.Release, error) {
		resolves.Add(1)
		if version != "v1.19.33" {
			return mihomo.Release{}, errors.New("fixture expected the checked version")
		}
		return testRelease(), ctx.Err()
	}
	b.prepare = func(ctx context.Context, release mihomo.Release, local string, _ bool) (app.InstallPlan, error) {
		prepares.Add(1)
		return app.InstallPlan{Release: release, Local: local, ActualPath: "/tmp/fixture/mihomo", Operation: "install"}, ctx.Err()
	}
	b.install = func(ctx context.Context, plan app.InstallPlan, _ downloadnet.Config, progress func(mihomo.Progress)) (app.InstallResult, error) {
		installs.Add(1)
		progress(mihomo.Progress{Stage: "download", Downloaded: 12 << 20, Total: 20 << 20, BytesPerSecond: 2 << 20, Detail: "Fixture transfer"})
		if scenario == "success" || scenario == "check-retry" {
			select {
			case <-ctx.Done():
				return app.InstallResult{}, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
			b.snapshot = onboardingDefaultSnapshot()
			return app.InstallResult{InstalledVersion: plan.Release.Version, Operation: "install"}, nil
		}
		<-ctx.Done()
		time.Sleep(80 * time.Millisecond)
		return app.InstallResult{}, ctx.Err()
	}
	language := os.Getenv("LAZYCLASH_INSTALL_PTY_LANGUAGE")
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]int32{"checks": checks.Load(), "resolves": resolves.Load(), "prepares": prepares.Load(), "installs": installs.Load(), "starts": starts.Load()})
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
