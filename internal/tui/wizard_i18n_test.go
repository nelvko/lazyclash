package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestWizardBilingualInstallKeepsReviewedMachineValues(t *testing.T) {
	for _, sample := range []struct{ language, title, installed string }{
		{"en", "Install Mihomo", "Mihomo installed and verified"},
		{"zh-Hans", "安装 Mihomo", "Mihomo 已安装并通过校验"},
	} {
		t.Run(sample.language, func(t *testing.T) {
			installs := 0
			b := &fakeBackend{install: func(_ context.Context, plan app.InstallPlan, cfg downloadnet.Config, _ func(mihomo.Progress)) (app.InstallResult, error) {
				installs++
				if plan.Operation != "install" || plan.Release.Asset != testRelease().Asset || cfg.Normalized().Method != "direct" {
					t.Fatal("translation changed the reviewed operation or release")
				}
				return app.InstallResult{InstalledVersion: plan.Release.Version, ActualPath: "/tmp/Install proxy core/mihomo", Operation: plan.Operation}, nil
			}}
			m := ready(b)
			m.locale = i18n.New(sample.language)
			m.width, m.height = 120, 40
			initial := m.openManagementWizard(false)
			m, _ = update(m, actionCmd(t, initial)())
			_, _, title := m.wizardDocument(100)
			if title != sample.title {
				t.Fatalf("unexpected wizard title %q", title)
			}
			m, cmd := activateWizard(m, "install")
			for _, stage := range []string{"resolve", "prepare", "install"} {
				if m.job != stage || m.wizard.step == stepConfirm {
					t.Fatalf("localized installation did not continue through %s", stage)
				}
				m, cmd = update(m, actionCmd(t, cmd)())
			}
			doc, _, _ := m.wizardDocument(100)
			plain := ansi.Strip(strings.Join(doc.lines, "\n"))
			if installs != 1 || m.wizard.step != stepResult || !strings.Contains(plain, sample.installed) || !strings.Contains(plain, "/tmp/Install proxy core/mihomo") {
				t.Fatal("installation result lost localized text or changed a path", plain)
			}
		})
	}
}

func TestWizardNetworkBilingualLabelsKeepIndependentResults(t *testing.T) {
	for _, sample := range []struct{ language, failed, passed string }{
		{"en", "Failed", "Passed"}, {"zh-Hans", "失败", "通过"},
	} {
		t.Run(sample.language, func(t *testing.T) {
			b := &fakeBackend{resolve: func(context.Context, string, downloadnet.Config) (mihomo.Release, error) {
				return mihomo.Release{}, errors.New("fixture-metadata-rejected")
			}}
			m := ready(b)
			m.locale = i18n.New(sample.language)
			m.wizard.release = testRelease()
			m.openNetwork(false)
			commands := commandBatch(t, m.testNetwork())
			m, _ = update(m, commands[0]())
			m, _ = update(m, commands[2]())
			if m.busy || m.wizard.versionTest.Status != checkFailed || m.wizard.fileTest.Status != checkPassed {
				t.Fatal("localized status labels affected test completion")
			}
			doc, _, _ := m.wizardDocument(100)
			screen := strings.Join(doc.lines, "\n")
			if !strings.Contains(screen, themed(sample.failed, errorColor, raised, false)) || !strings.Contains(screen, themed(sample.passed, success, raised, false)) {
				t.Fatal("network test lost a translated semantic state")
			}
			plain := ansi.Strip(screen)
			if !strings.Contains(plain, "fixture-metadata-rejected") || !strings.Contains(plain, testRelease().Asset) {
				t.Fatal("network test altered an external diagnostic or asset")
			}
		})
	}
}

func TestWizardBilingualLayoutKeepsCanvasAndHitRegions(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		m := ready(&fakeBackend{})
		m.locale = i18n.New(language)
		m.openManagementWizard(false)
		m.wizard.release = testRelease()
		m.wizard.plan = app.InstallPlan{Release: testRelease(), ActualPath: "/tmp/mihomo", Operation: "install", FreeBytes: 1024, DiskKnown: true}
		m.wizard.progress = mihomo.Progress{Stage: "download", Downloaded: 1024, Total: 4096, Detail: "fixture-progress"}
		m.wizard.result = app.InstallResult{InstalledVersion: testRelease().Version, ActualPath: "/tmp/mihomo", Operation: "install"}
		for _, size := range [][2]int{{160, 45}, {110, 30}, {80, 24}, {60, 20}, {40, 12}, {20, 5}} {
			m.width, m.height = size[0], size[1]
			for _, step := range []wizardStep{stepCore, stepVersion, stepConfirm, stepInstalling, stepResult} {
				m.wizard.step = step
				screen, hits := m.renderWizard("")
				assertCanvas(t, screen, size[0], size[1])
				assertRegions(t, hits, size[0], size[1])
				if m.wizard.step != step {
					t.Fatal("layout changed the semantic wizard step")
				}
			}
			m.wizard.networkOpen = true
			screen, hits := m.renderWizard("")
			assertCanvas(t, screen, size[0], size[1])
			assertRegions(t, hits, size[0], size[1])
			m.wizard.networkOpen = false
		}
	}
}

func TestPortsBilingualValidationReviewAndApply(t *testing.T) {
	for _, sample := range []struct{ language, rangeError, sameError, restart string }{
		{"en", "whole number", "must differ", "Apply & restart"},
		{"zh-Hans", "之间的整数", "必须不同", "应用并重启"},
	} {
		t.Run(sample.language, func(t *testing.T) {
			b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true}}}
			m := portsModel(t, b)
			m.locale = i18n.New(sample.language)
			m.ports.mixed.SetValue("not-a-port")
			if cmd := m.preparePorts(); cmd != nil || !strings.Contains(m.ports.err, sample.rangeError) || m.ports.mixed.Err == nil {
				t.Fatal("invalid range did not get a localized field error")
			}
			m.ports.mixed.SetValue("18080")
			m.ports.controller.SetValue("18080")
			if cmd := m.preparePorts(); cmd != nil || !strings.Contains(m.ports.err, sample.sameError) {
				t.Fatal("duplicate ports did not get a localized error")
			}
			m = reviewPorts(t, m)
			_, buttons, _ := m.portsDocument(80)
			if len(buttons) != 2 || buttons[1].ID != "ports.apply" || buttons[1].Label != sample.restart || m.ports.plan.Proposed.MixedPort != 18080 {
				t.Fatal("review translation changed apply semantics")
			}
			for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}, {40, 12}, {20, 5}} {
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			}
			m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
			m, cmd := click(m, findTarget(t, m, "ports.apply"))
			m, _ = update(m, actionCmd(t, cmd)())
			if !m.ports.applied || m.ports.result.Settings.MixedPort != 18080 || !m.ports.result.Restarted {
				t.Fatal("localized review failed to apply the original settings")
			}
		})
	}
}

func TestPortsMessageCleanupUsesOperationState(t *testing.T) {
	m := ready(&fakeBackend{})
	m.locale = i18n.New("zh-Hans")
	m.ports.open, m.ports.step = true, "preparing"
	m.message = "正在检查运行设置"
	m.closePorts()
	if m.message != "" {
		t.Fatal("closing a check retained its localized progress message")
	}
	m.ports.open, m.ports.step = true, "result"
	m.message = "Checking runtime is a completed result, not a pending check"
	m.closePorts()
	if m.message == "" {
		t.Fatal("closing a result discarded it based on its English prefix")
	}
}
