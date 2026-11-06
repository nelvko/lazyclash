package tui

import (
	"context"
	"errors"
	"fmt"
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

func inlineOperationMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	for {
		if cmd == nil {
			t.Fatal("operation command is missing")
		}
		msg := cmd()
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			return msg
		}
		if len(batch) == 0 {
			t.Fatal("operation batch is empty")
		}
		cmd = batch[0]
	}
}

func TestInlineStartupChecksOnceAndPreservesLateNavigation(t *testing.T) {
	for _, leaveBefore := range []bool{false, true} {
		t.Run(fmt.Sprint(leaveBefore), func(t *testing.T) {
			checks, writes := 0, 0
			b := &fakeBackend{checkUpdate: func(ctx context.Context, channel mihomo.Channel, _ downloadnet.Config) (app.CoreUpdateCheck, error) {
				checks++
				return app.CoreUpdateCheck{LatestVersion: "v1.19.33", Channel: channel, CheckedAt: time.Now()}, ctx.Err()
			}, install: func(context.Context, app.InstallPlan, downloadnet.Config, func(mihomo.Progress)) (app.InstallResult, error) {
				writes++
				return app.InstallResult{}, nil
			}}
			m := New(t.Context(), b, "en")
			initial := m.snapshotCmd(m.id, m.opCtx, true)()
			m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
			if leaveBefore {
				m, _ = update(m, key("7"))
			}
			previous := m.focus
			m, cmd := update(m, initial)
			if leaveBefore {
				if m.page != "settings" || m.focus != previous || m.busy || m.wizard.open {
					t.Fatal("late initial state took ownership of navigation")
				}
				m, cmd = update(m, key("1"))
			}
			if !m.wizard.inline || m.wizard.open || !m.busy || m.job != "check-update" || m.inlineCanInstall() {
				t.Fatal("initial inline state did not perform only a read-only version check")
			}
			if !leaveBefore && m.focus != "setup.install" {
				t.Fatalf("default focus is %q", m.focus)
			}
			m, _ = update(m, key("2"))
			previous = m.focus
			m, _ = update(m, inlineOperationMsg(t, cmd))
			if checks != 1 || writes != 0 || m.busy || m.page != "proxies" || m.focus != previous {
				t.Fatal("read-only result installed a core or stole navigation")
			}
			m, _ = update(m, key("1"))
			m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
			if m.busy || checks != 1 || !m.inlineCanInstall() {
				t.Fatal("returning to the region duplicated its check")
			}
		})
	}
}

func TestInlineCheckFailureDisablesInstallAndRetriesInPlace(t *testing.T) {
	checks := 0
	b := &fakeBackend{checkUpdate: func(context.Context, mihomo.Channel, downloadnet.Config) (app.CoreUpdateCheck, error) {
		checks++
		if checks == 1 {
			return app.CoreUpdateCheck{}, errors.New("version unavailable")
		}
		return app.CoreUpdateCheck{LatestVersion: "v1.19.33", Channel: mihomo.ChannelStable}, nil
	}}
	m := New(t.Context(), b, "en")
	m, cmd := update(m, m.snapshotCmd(m.id, m.opCtx, true)())
	m, _ = update(m, inlineOperationMsg(t, cmd))
	if m.inlineCanInstall() || m.wizard.err == "" || m.focus != "setup.resolve" || m.inlineInstallActivate("setup.install") != nil {
		t.Fatal("failed version check exposed an executable installation")
	}
	m, cmd = activateWizard(m, "resolve")
	m, _ = update(m, inlineOperationMsg(t, cmd))
	if checks != 2 || !m.inlineCanInstall() || m.wizard.open || m.wizard.err != "" {
		t.Fatal("inline retry did not recover the same installation region")
	}
}

func TestInlineFailureFocusPreservesOptionsAndRevealsLocalInput(t *testing.T) {
	t.Run("selected-options", func(t *testing.T) {
		m := ready(&fakeBackend{})
		m.newJob("check-update")
		m.wizard.more, m.focus = true, "setup.more"
		m, _ = update(m, wizardMsg{id: m.id, kind: "install-check", err: errors.New("check failed")})
		if m.focus != "setup.more" {
			t.Fatal("failed background check took focus from the chosen options")
		}
	})
	t.Run("local-preflight", func(t *testing.T) {
		m := ready(&fakeBackend{})
		m.wizard.localMode = true
		m.wizard.local.SetValue("/tmp/mihomo")
		m, _ = activateWizard(m, "install")
		m, _ = update(m, wizardMsg{id: m.id, kind: "prepare", err: errors.New("invalid executable")})
		if m.busy || m.focus != "setup.local" || !m.wizard.more || !hasAction(m.targets, "setup.local") || !m.wizard.local.Focused() {
			t.Fatal("local preflight failure did not reveal its editable source")
		}
	})
}

func TestInlineFailedCheckMoreOptionsHaveUniqueKeyboardTargets(t *testing.T) {
	m := New(t.Context(), &fakeBackend{}, "en")
	m, _ = update(m, m.snapshotCmd(m.id, m.opCtx, true)())
	m, _ = update(m, wizardMsg{id: m.id, kind: "install-check", err: errors.New("fixture check failed")})
	m, _ = activateWizard(m, "more")
	seen := make(map[string]bool)
	for _, target := range m.contentTargets() {
		if seen[target.ID] {
			t.Fatalf("expanded failed-check view repeats keyboard target %s", target.ID)
		}
		seen[target.ID] = true
	}
	visited := make(map[string]bool)
	for range len(m.focusTargets()) {
		m, _ = update(m, special(tea.KeyTab))
		visited[m.focus] = true
	}
	for _, id := range []string{"setup.network", "setup.version-mode", "setup.local-mode"} {
		if !visited[id] {
			t.Fatalf("Tab navigation cannot reach %s after a failed check", id)
		}
	}
}

func TestInlineFailedBackgroundCheckEntersRetryFromNavigation(t *testing.T) {
	for _, navigateBeforeSnapshot := range []bool{false, true} {
		t.Run(fmt.Sprint(navigateBeforeSnapshot), func(t *testing.T) {
			m := New(t.Context(), &fakeBackend{}, "en")
			if navigateBeforeSnapshot {
				m, _ = update(m, special(tea.KeyEscape))
			}
			m, _ = update(m, m.snapshotCmd(m.id, m.opCtx, true)())
			if !navigateBeforeSnapshot {
				m, _ = update(m, special(tea.KeyEscape))
			}
			if m.focus != "menu" && m.focus != "nav.overview" {
				t.Fatal("fixture did not return to navigation")
			}
			// At the default compact size Escape focuses the menu. Enter first
			// opens that navigation group; the next Enter enters its content.
			m, _ = update(m, wizardMsg{id: m.id, kind: "install-check", err: errors.New("fixture check failed")})
			if m.focus != "menu" && m.focus != "nav.overview" {
				t.Fatal("background check failure took focus from navigation")
			}
			if m.focus == "menu" {
				m, _ = update(m, special(tea.KeyEnter))
			}
			id := m.id
			m, _ = update(m, special(tea.KeyEnter))
			if m.focus != "setup.resolve" || m.busy || m.id != id {
				t.Fatal("returning to content focused disabled Install or activated Retry")
			}
			m, _ = update(m, special(tea.KeyEnter))
			if !m.busy || m.job != "check-update" || m.id == id {
				t.Fatal("focused Retry did not perform an explicit new check")
			}
			m.abortLookup()
		})
	}
}

func TestInlinePendingQueryDoesNotTrapAlternativeSetupShortcuts(t *testing.T) {
	for _, shortcut := range []string{"a", "n", "b", "i"} {
		t.Run(shortcut, func(t *testing.T) {
			m := New(t.Context(), &fakeBackend{}, "en")
			m, _ = update(m, m.snapshotCmd(m.id, m.opCtx, true)())
			previous, ctx := m.id, m.opCtx
			m, _ = update(m, key(shortcut))
			if ctx.Err() == nil || m.id == previous || m.wizard.installRequested {
				t.Fatal("alternative setup retained the pending query or installation authorization")
			}
			m, _ = update(m, wizardMsg{id: previous, kind: "install-check", updateCheck: app.CoreUpdateCheck{LatestVersion: "stale"}})
			if m.wizard.updateCheck.LatestVersion == "stale" {
				t.Fatal("cancelled query replaced the alternative setup state")
			}
			switch shortcut {
			case "a":
				if m.form != "source" || m.busy {
					t.Fatal("pending query swallowed the import shortcut")
				}
				m, _ = update(m, special(tea.KeyEscape))
				if !hasAction(m.contentTargets(), "setup.resolve") || m.inlineCanInstall() {
					t.Fatal("returning from import left no action to resume the cancelled check")
				}
			case "n":
				if !m.wizard.networkOpen || m.busy {
					t.Fatal("pending query swallowed download settings")
				}
			case "b":
				if !m.wizard.localMode || m.focus != "setup.local" || m.busy {
					t.Fatal("pending query swallowed the local executable shortcut")
				}
			case "i":
				if !m.busy || m.job != "check-update" || m.wizard.open {
					t.Fatal("explicit install shortcut did not restart only the inline check")
				}
				m.abortLookup()
			}
		})
	}
}

func TestInlineOneInstallActionKeepsProgressAndSuccessUntilExplicitStart(t *testing.T) {
	starts, installs := 0, 0
	b := &fakeBackend{control: func(_ context.Context, action string) error {
		starts++
		if action != "start" {
			t.Fatal(action)
		}
		return nil
	}}
	b.install = func(_ context.Context, plan app.InstallPlan, _ downloadnet.Config, report func(mihomo.Progress)) (app.InstallResult, error) {
		installs++
		report(mihomo.Progress{Stage: "download", Downloaded: 5, Total: 10})
		b.snapshot = onboardingDefaultSnapshot()
		return app.InstallResult{InstalledVersion: plan.Release.Version}, nil
	}
	m := ready(b)
	m.width, m.height = 120, 32
	m.layout()
	m, cmd := click(m, findTarget(t, m, "setup.install"))
	for _, job := range []string{"resolve", "prepare", "install"} {
		if m.job != job || m.wizard.open || m.wizard.step == stepConfirm {
			t.Fatalf("unexpected %s state: job=%s step=%s", job, m.job, m.wizard.step)
		}
		if job == "install" {
			m, _ = update(m, key("7"))
			if m.page != "settings" || !m.busy {
				t.Fatal("installation prevented browsing another page")
			}
			m, _ = update(m, key("1"))
		}
		m, cmd = update(m, inlineOperationMsg(t, cmd))
	}
	if installs != 1 || starts != 0 || !m.snapshot.Installed || !m.inlineInstallVisible() || m.wizard.step != stepResult || !hasAction(m.contentTargets(), "setup.start") {
		t.Fatal("success did not retain the explicit Start action")
	}
	m, cmd = activateWizard(m, "start")
	if starts != 0 || !m.busy {
		t.Fatal("start was not an explicit asynchronous action")
	}
	m, _ = update(m, inlineOperationMsg(t, cmd))
	if starts != 1 || m.inlineInstallVisible() || m.wizard.open {
		t.Fatal("successful start did not restore the ordinary overview")
	}
}

func TestInlineSkipCancelsLookupAndRejectsLateResults(t *testing.T) {
	m := New(t.Context(), &fakeBackend{}, "en")
	m, cmd := update(m, m.snapshotCmd(m.id, m.opCtx, true)())
	late := inlineOperationMsg(t, cmd)
	old := m.id
	m, _ = activateWizard(m, "later")
	m, _ = update(m, late)
	if m.busy || m.id == old || m.wizard.inline || !m.guideSkipped || m.snapshot.GuideDismissed {
		t.Fatal("session skip retained its query or persisted a guide dismissal")
	}
	m, _ = update(m, key("1"))
	if m.inlineInstallVisible() || m.busy {
		t.Fatal("returning to Overview reopened skipped setup")
	}
}

func TestInlineInputsOwnPrintableKeysAndPreserveDraftAcrossPages(t *testing.T) {
	m := ready(&fakeBackend{})
	m, _ = activateWizard(m, "more")
	m, _ = activateWizard(m, "version-mode")
	for _, r := range "q1234567" {
		m, _ = update(m, key(string(r)))
	}
	if m.wizard.version.Value() != "q1234567" || m.page != "overview" || m.quitting {
		t.Fatal("global shortcuts intercepted inline input")
	}
	m, _ = update(m, special(tea.KeyEscape))
	m, _ = update(m, key("7"))
	m, _ = update(m, key("1"))
	if m.wizard.version.Value() != "q1234567" || !m.wizard.more || !m.wizard.specific {
		t.Fatal("page navigation lost inline input state")
	}
}

func TestInlineCancelWaitsForCleanupAndKeepsNavigation(t *testing.T) {
	m := ready(&fakeBackend{})
	ctx := m.newJob("install")
	m.wizard.step, m.focus = stepInstalling, "setup.cancel"
	m.wizard.progress = mihomo.Progress{Stage: "download", Downloaded: 10, Total: 100}
	m, _ = update(m, special(tea.KeyEscape))
	if ctx.Err() == nil || !m.busy || !m.wizard.cancelling || !m.inlineInstallVisible() || m.wizard.open {
		t.Fatal("cancel did not wait for installation cleanup in the inline region")
	}
	m, _ = update(m, key("7"))
	previous := m.focus
	m, _ = update(m, wizardMsg{id: m.id, kind: "install", err: context.Canceled})
	if m.busy || m.wizard.step != stepResult || m.page != "settings" || m.focus != previous {
		t.Fatal("cleanup result took navigation or failed to retain the inline result")
	}
	m, _ = update(m, key("1"))
	if !hasAction(m.contentTargets(), "setup.retry") || m.wizard.err == "" {
		t.Fatal("cancelled installation lost its retry action")
	}
}

func TestInlineDownloadMethodChangeInvalidatesPendingInstallation(t *testing.T) {
	m := ready(&fakeBackend{})
	m, cmd := activateWizard(m, "install")
	late := inlineOperationMsg(t, cmd)
	m, _ = activateWizard(m, "network")
	if !m.wizard.networkOpen || m.busy || m.wizard.installRequested {
		t.Fatal("network editor retained an earlier installation authorization")
	}
	m.wizard.draft = downloadnet.Config{Method: downloadnet.MethodEnvironment}
	m, cmd = activateWizard(m, "apply-network")
	if !m.busy || m.job != "check-update" || m.wizard.installRequested {
		t.Fatal("applying a method did not perform only a fresh version check")
	}
	m, _ = update(m, late)
	m, _ = update(m, inlineOperationMsg(t, cmd))
	if m.busy || m.wizard.step != stepVersion || m.wizard.release.Asset != "" || m.wizard.cfg.Method != downloadnet.MethodEnvironment {
		t.Fatal("stale resolution resumed installation after the download method changed")
	}
}

func TestInlineStartFailureRemainsRetryableAndVisibleAfterPageSwitch(t *testing.T) {
	attempts := 0
	b := &fakeBackend{snapshot: onboardingDefaultSnapshot(), control: func(context.Context, string) error {
		attempts++
		if attempts == 1 {
			return errors.New("fixture start failed")
		}
		return nil
	}}
	m := ready(b)
	m.wizard.inline, m.wizard.step = true, stepResult
	m.wizard.result.InstalledVersion = b.snapshot.Version
	m.focus = "setup.start"
	m, cmd := activateWizard(m, "start")
	if duplicate := m.inlineInstallActivate("setup.start"); duplicate != nil {
		t.Fatal("pending start accepted a duplicate action")
	}
	m, _ = update(m, inlineOperationMsg(t, cmd))
	if !m.inlineInstallVisible() || !strings.Contains(m.failure, "fixture start failed") || !hasAction(m.contentTargets(), "setup.start") {
		t.Fatal("start failure lost the retryable success region")
	}
	m, _ = update(m, key("7"))
	if len(m.notification.items) != 1 || m.notification.items[0].Kind != "error" {
		t.Fatal("background start failure became inaccessible after leaving Overview")
	}
	m, _ = update(m, key("1"))
	if len(m.dashboardOptions().Notifications) != 0 {
		t.Fatal("inline failure was duplicated as an overlapping toast")
	}
	m, cmd = activateWizard(m, "start")
	m, _ = update(m, inlineOperationMsg(t, cmd))
	if attempts != 2 || m.inlineInstallVisible() || m.failure != "" {
		t.Fatal("explicit start retry did not restore the ordinary overview")
	}
}

func TestInlineMoreOptionsRetainFocusAndRemoveHiddenFieldsFromTabOrder(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height = 80, 24
	m, _ = activateWizard(m, "more")
	m, _ = activateWizard(m, "local-mode")
	m, _ = update(m, key("x"))
	if m.focus != "setup.local" || !m.wizard.local.Focused() {
		t.Fatal("local source did not focus its Bubbles input")
	}
	if !hasAction(m.targets, "setup.local") || hasAction(m.contentTargets(), "setup.version-mode") {
		t.Fatal("local source did not reveal its input or retained irrelevant remote version controls")
	}
	m, _ = activateWizard(m, "more")
	if m.wizard.more || m.wizard.local.Focused() || m.focus != "setup.more" {
		t.Fatal("collapsing options left focus in a hidden input")
	}
	for _, target := range m.focusTargets() {
		if target.ID == "setup.local" || target.ID == "setup.version-input" || target.ID == "content" {
			t.Fatal("collapsed inline options retained an inaccessible Tab stop")
		}
	}
	m, _ = update(m, key("7"))
	m, _ = update(m, key("1"))
	if m.focus != "setup.more" || m.wizard.local.Value() != "x" {
		t.Fatal("returning to Overview lost the collapsed options focus or draft")
	}
}

func TestInlinePresentationRetainsSidebarAndFitsTerminal(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, size := range [][2]int{{216, 55}, {160, 45}, {120, 32}, {80, 24}, {60, 20}, {120, 18}, {30, 12}, {24, 8}, {20, 7}} {
			for _, state := range []string{"initial", "more", "progress", "success", "failure"} {
				t.Run(fmt.Sprintf("%s/%v/%s", language, size, state), func(t *testing.T) {
					m := ready(&fakeBackend{})
					m.locale = i18n.New(language)
					m.width, m.height = size[0], size[1]
					m.wizard.more = state == "more"
					switch state {
					case "progress":
						m.busy, m.job, m.wizard.step = true, "install", stepInstalling
						m.wizard.progress = mihomo.Progress{Stage: "download", Downloaded: 1 << 20, Total: 4 << 20}
					case "success":
						m.snapshot = onboardingDefaultSnapshot()
						m.wizard.step = stepResult
						m.wizard.result.InstalledVersion = "v1.19.33"
					case "failure":
						m.wizard.step, m.wizard.err = stepResult, "fixture failed"
					}
					m.layout()
					assertCanvas(t, m.View().Content, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
					g := dashboardGeometry(m.dashboardOptions())
					if size[0] >= 38 {
						if g.width != min(96, g.headerWidth) || g.x != g.headerX {
							t.Fatalf("inline body must cap at 96 cells and align with the full header: %+v", g)
						}
						divider := ansi.Strip(strings.Split(m.screen, "\n")[g.headerHeight])
						dividerWidth := size[0]
						if g.sidebar {
							dividerWidth -= sidebarWidth
						}
						if strings.Count(divider, "▔") != dividerWidth {
							t.Fatal("bounded inline body narrowed the page divider")
						}
					}
					if g.sidebar && !hasAction(m.targets, "nav.settings") {
						t.Fatal("inline setup removed the ordinary sidebar")
					}
					if state == "initial" && size[0] >= 60 && size[1] >= 18 && !hasAction(m.targets, "setup.install") {
						t.Fatal("primary installation action is below the initial fold")
					}
					if state == "initial" && strings.Contains(ansi.Strip(m.screen), m.tr("wizard.local.choose")) {
						t.Fatal("advanced options were not collapsed")
					}
				})
			}
		}
	}
}

func TestInlineCompactNoticeFitsAndRetainsEscapeSemantics(t *testing.T) {
	for _, installing := range []bool{false, true} {
		t.Run(fmt.Sprint(installing), func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.width, m.height = 24, 8
			m.focus = "setup.install"
			var ctx context.Context
			if installing {
				ctx = m.newJob("install")
				m.wizard.step = stepInstalling
				m.wizard.progress.Stage = "download"
				m.focus = "setup.cancel"
			}
			m.layout()
			if !strings.Contains(ansi.Strip(m.screen), "Please enlarge terminal") || len(m.contentTargets()) != 0 {
				t.Fatal("compact setup did not replace clipped actions with the size notice")
			}
			assertCanvas(t, m.View().Content, 24, 8)
			assertRegions(t, m.targets, 24, 8)
			m, _ = update(m, special(tea.KeyEnter))
			if !installing && m.busy {
				t.Fatal("compact setup activated an invisible install action")
			}
			m, _ = update(m, special(tea.KeyEscape))
			if installing {
				if !m.busy || !m.wizard.cancelling || ctx.Err() == nil {
					t.Fatal("compact installation did not wait for cancellation cleanup")
				}
			} else if m.focus != "menu" {
				t.Fatal("compact setup did not return to navigation")
			}
		})
	}
}
