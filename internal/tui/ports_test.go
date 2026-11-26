package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func focusedPortsEntry(backend *fakeBackend, width, height int) Model {
	m := ready(backend)
	m.page, m.focus = "settings", "runtime-settings"
	m, _ = update(m, tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

func portsModel(t *testing.T, backend *fakeBackend) Model {
	t.Helper()
	m := focusedPortsEntry(backend, 120, 32)
	m, cmd := click(m, findTarget(t, m, "runtime-settings"))
	if !m.ports.open || !m.busy || m.ports.step != "loading" {
		t.Fatal("runtime settings did not load asynchronously")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.ports.step != "edit" || m.message != "" {
		t.Fatal("runtime settings did not reach the edit form")
	}
	return m
}

func TestSettingsOpensProxyPortEditorWithoutApplying(t *testing.T) {
	reads, writes := 0, 0
	backend := &fakeBackend{snapshot: onboardingDefaultSnapshot()}
	backend.snapshot.MixedPort = 17890
	backend.snapshot.Controller = "http://private-user:private-password@127.0.0.1:19090/private-path?token=private-token#private-fragment"
	backend.getSettings = func(context.Context) (app.RuntimeSettings, error) {
		reads++
		return app.RuntimeSettings{MixedPort: 17890, ControllerPort: 19090}, nil
	}
	backend.applySettings = func(context.Context, app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
		writes++
		return app.RuntimeSettingsResult{}, nil
	}
	m := focusedPortsEntry(backend, 160, 60)
	m, cmd := click(m, findTarget(t, m, "runtime-settings"))
	if m.page != "settings" || !m.ports.open || !m.busy || m.ports.step != "loading" || reads != 0 || writes != 0 {
		t.Fatal("settings action did not open the existing settings dialog with an asynchronous read")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.ports.step != "edit" || reads != 1 || writes != 0 || m.ports.mixed.Value() != "17890" || m.ports.current.ControllerPort != 19090 {
		t.Fatal("settings action applied settings or failed to show the current configured ports")
	}
	plain := ansi.Strip(m.screen)
	for _, address := range []string{"17890", m.tr("access.http"), m.tr("access.redir"), m.tr("access.tproxy")} {
		if !strings.Contains(plain, address) {
			t.Fatalf("network details omitted %s:\n%s", address, plain)
		}
	}
	for _, secret := range []string{"19090", "private-user", "private-password", "private-path", "private-token", "private-fragment"} {
		if strings.Contains(plain, secret) {
			t.Fatalf("network details leaked %s", secret)
		}
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.ports.open || m.page != "settings" || m.focus != "runtime-settings" || writes != 0 {
		t.Fatal("closing unchanged port settings failed to restore its settings entry point")
	}
	for _, secret := range []string{"private-user", "private-password", "private-path", "private-token", "private-fragment"} {
		if strings.Contains(ansi.Strip(m.screen), secret) {
			t.Fatalf("returning from network details exposed %s in Settings", secret)
		}
	}
}

func reviewPorts(t *testing.T, m Model) Model {
	t.Helper()
	m.ports.mixed.SetValue("18080")
	m.ports.field("ports.http").SetValue("19090")
	m.ports.focus = "ports.review"
	m, cmd := update(m, special(tea.KeyEnter))
	if !m.busy || cmd == nil {
		t.Fatal("review preflight did not start asynchronously")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.ports.step != "review" {
		t.Fatal("preflight did not produce a review page", m.ports.err)
	}
	return m
}

func TestRuntimePortsRequireReviewBeforeApplyAndRefreshDashboard(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprint(active), func(t *testing.T) {
			applies := 0
			b := &fakeBackend{snapshot: app.Snapshot{Installed: true, ConfigReady: true, Applied: "saved", Service: service.Status{Active: active}}}
			b.applySettings = func(ctx context.Context, p app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
				applies++
				if p.Proposed.MixedPort != 18080 || p.Proposed.HTTPPort != 19090 || p.Proposed.ControllerPort != 9097 || p.Active != active {
					t.Fatal("application did not receive the reviewed settings")
				}
				b.snapshot.RuntimeSettings = p.Proposed
				b.snapshot.Controller = "127.0.0.1:9097"
				return app.RuntimeSettingsResult{Settings: p.Proposed, Controller: b.snapshot.Controller, Restarted: active, Changed: true}, nil
			}
			m := reviewPorts(t, portsModel(t, b))
			if applies != 0 || m.ports.focus != "ports.apply" {
				t.Fatal("preflight applied a change without a separate confirmation")
			}
			plain := ansi.Strip(m.screen)
			impact := m.tr("ports.stopped_boundary")
			if active {
				impact = m.tr("ports.restart_warning")
			}
			for _, line := range strings.Split(wrapText(impact, m.ports.viewport.Width()), "\n") {
				if !strings.Contains(plain, line) {
					t.Fatalf("review omitted part of the service impact: %q", line)
				}
			}
			m, cmd := click(m, findTarget(t, m, "ports.apply"))
			m, duplicate := update(m, special(tea.KeyEnter))
			if duplicate != nil || !m.busy || m.ports.step != "applying" {
				t.Fatal("duplicate Apply started a second task")
			}
			m, _ = update(m, actionCmd(t, cmd)())
			if applies != 1 || m.busy || !m.ports.applied || m.ports.step != "result" || m.snapshot.RuntimeSettings.HTTPPort != 19090 || m.snapshot.Controller != "127.0.0.1:9097" {
				t.Fatal("application result or dashboard snapshot was not refreshed")
			}
			m, _ = click(m, findTarget(t, m, "ports.done"))
			if m.ports.open || m.page != "settings" || m.focus != "runtime-settings" {
				t.Fatal("Done did not restore Settings and its triggering control")
			}
		})
	}
}

func TestRuntimePortsInputOwnsDigitsPasteAndNormalCharacters(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	m.ports.mixed.SetValue("")
	for _, char := range "1234567q" {
		m, _ = update(m, key(string(char)))
	}
	if m.ports.mixed.Value() != "1234567q" || m.page != "settings" || m.quitting || m.wizard.open {
		t.Fatal("port input lost printable characters to global shortcuts")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.busy || m.ports.proxyFieldError("ports.mixed") == "" || hasAction(m.targets, "ports.review") {
		t.Fatal("invalid port started a backend task without an input error")
	}
	m.ports.mixed.SetValue("")
	m, _ = update(m, tea.PasteMsg{Content: "0000000000000001x"})
	if m.ports.mixed.Value() != "0000000000000001x" || m.ports.proxyFieldError("ports.mixed") == "" {
		t.Fatal("invalid pasted port was silently truncated to a valid number")
	}
	m.ports.mixed.SetValue("")
	m, _ = update(m, tea.PasteMsg{Content: "18080"})
	for steps := 0; m.ports.focus != "ports.http" && steps < len(m.ports.controls); steps++ {
		m, _ = update(m, special(tea.KeyTab))
	}
	if m.ports.focus != "ports.http" || m.ports.mixed.Value() != "18080" {
		t.Fatal("Tab did not reach the HTTP editor while preserving the proxy port")
	}
	m.ports.field("ports.http").SetValue("")
	m, _ = update(m, tea.PasteMsg{Content: "18080"})
	m, _ = update(m, special(tea.KeyEnter))
	if m.busy || !strings.Contains(m.ports.proxyFieldError("ports.http"), "must differ") || hasAction(m.targets, "ports.review") {
		t.Fatal("duplicate listener ports were accepted")
	}
}

func TestRuntimePortsResizePreservesDraftAndKeepsActionsAccessible(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	m.ports.mixed.SetValue("18080")
	m.ports.field("ports.http").SetValue("19090")
	id := m.id
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {120, 18}, {20, 5}, {120, 32}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if !m.ports.open || m.ports.mixed.Value() != "18080" || m.ports.field("ports.http").Value() != "19090" || m.ports.focus != "ports.mixed" || m.id != id {
			t.Fatal("resize changed a draft, focus or task identity")
		}
		assertCanvas(t, m.View().Content, size[0], size[1])
		assertRegions(t, m.targets, size[0], size[1])
		if size[0] >= 34 && size[1] >= 10 {
			findTarget(t, m, "ports.cancel")
			findTarget(t, m, "ports.review")
			findTarget(t, m, "ports.mixed")
		}
	}
	m = reviewPorts(t, m)
	for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 20}, {120, 18}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		findTarget(t, m, "ports.apply")
		findTarget(t, m, "ports.back")
		assertCanvas(t, m.View().Content, size[0], size[1])
		assertRegions(t, m.targets, size[0], size[1])
	}
}

func TestRuntimePortsCancellationWaitsForApplicationAndRejectsLateMessages(t *testing.T) {
	started := make(chan struct{})
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true}, applySettings: func(ctx context.Context, p app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
		close(started)
		<-ctx.Done()
		return app.RuntimeSettingsResult{}, ctx.Err()
	}}
	m := reviewPorts(t, portsModel(t, b))
	m, cmd := update(m, special(tea.KeyEnter))
	run := actionCmd(t, cmd)
	done := make(chan tea.Msg, 1)
	go func() { done <- run() }()
	<-started
	m, _ = update(m, special(tea.KeyEscape))
	if !m.ports.open || !m.busy || !m.ports.cancelling {
		t.Fatal("cancel discarded the active task before recovery completed")
	}
	select {
	case msg := <-done:
		m, _ = update(m, msg)
	case <-time.After(time.Second):
		t.Fatal("cancellation did not propagate to the application")
	}
	if m.busy || !m.ports.open || m.ports.applied || !strings.Contains(m.ports.err, "canceled") {
		t.Fatal("canceled task did not reach an explicit result")
	}
	m.ports.focus = "ports.retry"
	m, cmd = update(m, special(tea.KeyEnter))
	id := m.id
	m, _ = update(m, portsMsg{id: id - 1, kind: "apply", result: app.RuntimeSettingsResult{Settings: app.RuntimeSettings{MixedPort: 9999}}})
	if !m.busy || m.id != id || m.ports.applied {
		t.Fatal("late application result replaced a newer review task")
	}
	m.cancel()
	_ = cmd
}

func TestRuntimePortsCancellingDisablesRepeatedCancelAction(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	m.ports.step, m.ports.cancelling, m.busy = "applying", true, true
	cancelCalls := 0
	m.cancel = func() { cancelCalls++ }
	m.layout()
	if hasAction(m.targets, "ports.cancel") || len(m.ports.controls) != 0 {
		t.Fatal("cancellation cleanup retained an executable or tabbable Cancel button")
	}
	if m.portsActivate("ports.cancel") != nil || cancelCalls != 0 {
		t.Fatal("cancellation cleanup accepted a duplicate Cancel action")
	}
}

func TestRuntimePortsModalKeepsSharedSessionAndDoesNotClickThrough(t *testing.T) {
	m := focusedPortsEntry(&fakeBackend{snapshot: app.Snapshot{Installed: true}}, 120, 32)
	background := findTarget(t, m, "nav.overview")
	m, cmd := click(m, findTarget(t, m, "runtime-settings"))
	m, _ = update(m, actionCmd(t, cmd)())
	if !m.runtime.active || m.rulesVisible() {
		t.Fatal("modal interrupted shared monitoring or kept rules polling visible")
	}
	for _, hit := range m.targets {
		if !strings.HasPrefix(hit.ID, "ports.") && !strings.HasPrefix(hit.tip.owner, "ports.") {
			t.Fatal("background mouse target remained active")
		}
	}
	m, _ = click(m, background)
	if m.page != "settings" || !m.ports.open {
		t.Fatal("click passed through the ports modal")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.ports.open || m.focus != "runtime-settings" {
		t.Fatal("canceling an unconfirmed draft did not return to Settings")
	}
}

func TestRuntimePortsCommittedSuccessDoesNotBecomeFailureWhenRefreshFails(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true}}
	b.applySettings = func(ctx context.Context, p app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
		b.snapshotErr = errors.New("snapshot temporarily unavailable")
		return app.RuntimeSettingsResult{Settings: p.Proposed, Controller: "127.0.0.1:19090", Changed: true}, nil
	}
	m := reviewPorts(t, portsModel(t, b))
	m, cmd := update(m, special(tea.KeyEnter))
	m, _ = update(m, actionCmd(t, cmd)())
	if !m.ports.applied || m.ports.err != "" || m.ports.refreshError == "" {
		t.Fatal("a dashboard read failure misclassified a committed port change")
	}
}

func TestRuntimePortsConflictPreservesDraftAndAllowsCorrectionBeforeReview(t *testing.T) {
	checks, applies := 0, 0
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true}}}
	b.prepareSettings = func(_ context.Context, settings app.RuntimeSettings) (app.RuntimeSettingsPlan, error) {
		checks++
		if checks == 1 {
			return app.RuntimeSettingsPlan{}, diagnostic.New("diagnostic.settings.portUnavailable", "local %s port %s is unavailable", "tcp", "127.0.0.1:18080")
		}
		return app.RuntimeSettingsPlan{Proposed: settings, Active: true, Changed: true}, nil
	}
	b.applySettings = func(context.Context, app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
		applies++
		return app.RuntimeSettingsResult{}, nil
	}
	m := portsModel(t, b)
	m.ports.mixed.SetValue("18080")
	m.ports.field("ports.http").SetValue("19090")
	m.ports.focus = "ports.http"
	m, cmd := update(m, special(tea.KeyEnter))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.message != "" || m.ports.step != "edit" || m.ports.focus != "ports.mixed" || m.ports.mixed.Value() != "18080" || m.ports.field("ports.http").Value() != "19090" || !strings.Contains(m.ports.proxyFieldError("ports.mixed"), "18080") || hasAction(m.targets, "ports.review") {
		t.Fatal("conflict discarded the editable draft or did not expose the error")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m, _ = update(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m, _ = update(m, tea.PasteMsg{Content: "18081"})
	m, cmd = update(m, special(tea.KeyEnter))
	m, _ = update(m, actionCmd(t, cmd)())
	if checks != 2 || applies != 0 || m.ports.step != "review" || m.ports.err != "" || (m.ports.plan.Proposed.MixedPort != 18081 || m.ports.plan.Proposed.HTTPPort != 19090 || m.ports.plan.Proposed.ControllerPort != 9097) {
		t.Fatal("corrected ports were not reviewed independently before applying")
	}
}

func TestProxyPortConflictDuringApplyReopensOffendingField(t *testing.T) {
	backend := &fakeBackend{snapshot: app.Snapshot{Installed: true}}
	backend.applySettings = func(context.Context, app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
		return app.RuntimeSettingsResult{}, diagnostic.New("diagnostic.settings.portUnavailable", "local %s port %s is unavailable", "tcp", "127.0.0.1:19090")
	}
	m := reviewPorts(t, portsModel(t, backend))
	m, cmd := click(m, findTarget(t, m, "ports.apply"))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.ports.step != "edit" || m.ports.focus != "ports.http" || m.ports.applied || !strings.Contains(m.ports.proxyFieldError("ports.http"), "19090") || hasAction(m.targets, "ports.review") {
		t.Fatal("late bind conflict did not return to the failed input")
	}
	m, _ = click(m, findTarget(t, m, "ports.mixed.clear"))
	if m.ports.proxyFieldError("ports.http") == "" || hasAction(m.targets, "ports.review") {
		t.Fatal("editing an unrelated field cleared the failed port")
	}
	m, _ = click(m, findTarget(t, m, "ports.http.clear"))
	if m.ports.proxyFieldError("ports.http") != "" || !hasAction(m.targets, "ports.review") {
		t.Fatal("correcting the failed port did not restore review")
	}
}

func TestProxyPortBindPermissionErrorAppearsAtTheField(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	m.ports.field("ports.http").SetValue("80")
	m.acceptPorts(portsMsg{id: m.id, kind: "prepare", err: diagnostic.New("diagnostic.settings.portPermission", "binding %s %s is not permitted", "tcp", "127.0.0.1:80")})
	m.layout()
	if m.ports.focus != "ports.http" || m.ports.proxyFieldError("ports.http") == "" || m.ports.err != "" || hasAction(m.targets, "ports.review") {
		t.Fatal("OS permission error did not identify the low port involved")
	}
}

func TestRuntimePortsShortWindowKeepsValidationAndFocusedFieldsVisible(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{80, 24}, {60, 18}, {40, 16}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size[0], size[1]), func(t *testing.T) {
				m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
				m.locale = i18n.New(language)
				m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.ports.mixed.SetValue("18080")
				m.ports.field("ports.http").SetValue("18080")
				m, _ = update(m, special(tea.KeyEnter))
				if m.ports.focus != "ports.mixed" || m.ports.proxyFieldError("ports.mixed") == "" || m.ports.proxyFieldError("ports.http") == "" || m.busy {
					t.Fatal("duplicate ports did not mark both inputs without submitting")
				}
				for _, focused := range []string{"ports.http", "ports.tproxy", "ports.mixed", "ports.cancel"} {
					for attempts := 0; m.ports.focus != focused && attempts <= len(m.ports.controls); attempts++ {
						m, _ = update(m, special(tea.KeyTab))
					}
					hit := findTarget(t, m, focused)
					if m.ports.field(focused) != nil && (hit.H != 1 || hit.InputWidth < 5) {
						t.Fatalf("focused compact input was clipped: %+v", hit)
					}
					if message := m.ports.proxyFieldError(focused); message != "" {
						lines := strings.Split(ansi.Strip(m.screen), "\n")
						for offset, line := range strings.Split(wrapText(message, m.ports.viewport.Width()), "\n") {
							if row := hit.Y + hit.H + offset; row >= len(lines) || !strings.Contains(lines[row], line) {
								t.Fatalf("focused %s lacks adjacent validation %q:\n%s", focused, line, ansi.Strip(m.screen))
							}
						}
					}
					findTarget(t, m, "ports.cancel")
					if hasAction(m.targets, "ports.review") || slices.Contains(m.ports.controls, "ports.review") {
						t.Fatal("invalid draft kept an active review action")
					}
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
				}
			})
		}
	}
}

func TestRuntimePortsHeightFitsContentAndShowsAllFiveEditorsAt80Columns(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 60})
	if m.ports.viewport.TotalLineCount() > m.ports.viewport.Height() {
		t.Fatal("the dialog kept an unnecessary fixed height in a tall terminal")
	}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		m.locale = i18n.New(language)
		m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
		for _, id := range proxyFields {
			if hit := findTarget(t, m, id); hit.H != 1 || hit.InputWidth < 5 {
				t.Fatalf("%s %s field was clipped on the initial 80x24 form: %+v", language, id, hit)
			}
		}
	}
}

func TestRuntimePortsLongFailureCanBeScrolledWithoutHidingActions(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	m.ports.mixed.SetValue("18080")
	m, _ = update(m, tea.WindowSizeMsg{Width: 60, Height: 18})
	m.acceptPorts(portsMsg{id: m.id, kind: "prepare", err: errors.New(strings.Repeat("port conflict detail ", 50) + "END-OF-DIAGNOSTIC")})
	m.layout()
	if !strings.Contains(ansi.Strip(m.screen), "port conflict detail") {
		t.Fatal("prepare failure did not immediately expose its explanation")
	}
	for i := 0; i < 30 && !m.ports.viewport.AtBottom(); i++ {
		m, _ = update(m, special(tea.KeyPgDown))
	}
	if !strings.Contains(ansi.Strip(m.screen), "END-OF-DIAGNOSTIC") {
		t.Fatal("long diagnostic was truncated instead of remaining scrollable")
	}
	findTarget(t, m, "ports.cancel")
	findTarget(t, m, "ports.review")
	m.ports.focus, m.ports.reveal = "ports.http", true
	m.layout()
	if hit := findTarget(t, m, "ports.http"); hit.H != 1 || hit.InputWidth < 5 {
		t.Fatal("returning from the diagnostic failed to reveal the full input")
	}
}

func TestRuntimePortsLateResultCannotQuitWhileNewerRollbackIsPending(t *testing.T) {
	started, cleanupAllowed := make(chan struct{}), make(chan struct{})
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true}, applySettings: func(ctx context.Context, p app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
		close(started)
		<-ctx.Done()
		<-cleanupAllowed
		return app.RuntimeSettingsResult{}, ctx.Err()
	}}
	m := reviewPorts(t, portsModel(t, b))
	m, cmd := update(m, special(tea.KeyEnter))
	run := actionCmd(t, cmd)
	done := make(chan tea.Msg, 1)
	go func() { done <- run() }()
	<-started
	id := m.id
	m, cmd = update(m, ctrlC())
	if cmd != nil || !m.busy || !m.quitting || !m.ports.cancelling {
		t.Fatal("Ctrl+C did not wait for the current task's rollback")
	}
	m, cmd = update(m, portsMsg{id: id - 1, kind: "apply"})
	if cmd != nil || !m.busy || !m.quitting || m.ports.applied || m.id != id {
		t.Fatal("late port result escaped a newer task's pending rollback")
	}
	close(cleanupAllowed)
	select {
	case msg := <-done:
		m, cmd = update(m, msg)
	case <-time.After(time.Second):
		t.Fatal("current task did not finish after rollback was released")
	}
	if m.busy || cmd == nil {
		t.Fatal("current task completion did not allow the pending quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("current rollback completion did not request a safe quit")
	}
}

func TestCommittedPortsSurfacePostApplyFailures(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, sample := range []struct {
			name                    string
			endpointErr, refreshErr error
		}{
			{"endpoint", errors.New("endpoint-write-offline"), nil},
			{"snapshot", nil, errors.New("snapshot-read-offline")},
			{"both", errors.New("endpoint-write-offline"), errors.New("snapshot-read-offline")},
			{"complete", nil, nil},
		} {
			t.Run(language+"/"+sample.name, func(t *testing.T) {
				b := &fakeBackend{snapshot: app.Snapshot{Installed: true}}
				b.applySettings = func(_ context.Context, plan app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
					b.snapshotErr = sample.refreshErr
					return app.RuntimeSettingsResult{Settings: plan.Proposed, Controller: "127.0.0.1:19090", Changed: true}, sample.endpointErr
				}
				m := portsModel(t, b)
				m.locale = i18n.New(language)
				m = reviewPorts(t, m)
				m, cmd := update(m, special(tea.KeyEnter))
				m, _ = update(m, actionCmd(t, cmd)())
				screen := ansi.Strip(m.screen)
				if !m.ports.applied || m.ports.current.MixedPort != 18080 || !strings.Contains(screen, m.tr("ports.applied")) || hasAction(m.targets, "ports.retry") {
					t.Fatalf("committed settings incorrectly presented for reapplication: %s", screen)
				}
				if sample.endpointErr == nil && sample.refreshErr == nil {
					if len(m.notification.items) != 0 {
						t.Fatal("successful result duplicated into dashboard notifications")
					}
					return
				}
				if len(m.notification.items) != 1 {
					t.Fatalf("lost post-apply failure: %+v", m.notification.items)
				}
				n := m.notification.items[0]
				if n.Kind != "error" || !n.Critical || !strings.Contains(n.Message, m.tr("ports.applied")) {
					t.Fatalf("notification lost the committed outcome: %+v", n)
				}
				// Read the visible dialog columns so a wrapped error can span
				// two lines without including the dashboard behind the dialog.
				width := m.ports.viewport.Width()
				left := (m.width - width) / 2
				var visibleResult strings.Builder
				for _, line := range strings.Split(screen, "\n") {
					visibleResult.WriteString(strings.TrimSpace(ansi.Cut(line, left, left+width)))
				}
				for _, err := range []error{sample.endpointErr, sample.refreshErr} {
					if err != nil && (!strings.Contains(visibleResult.String(), err.Error()) || !strings.Contains(n.Details, err.Error())) {
						t.Fatalf("missing cause %v in result or notification: %s / %+v", err, screen, n)
					}
				}
			})
		}
	}
}

func TestExternalControlEditsSectionsAndReviewsSecretWithoutDisclosure(t *testing.T) {
	const replacement = "secret-value-must-never-appear"
	current := app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}
	current.HTTPPort = 8080
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, ConfigReady: true, RuntimeSettings: current}}
	b.getSettings = func(context.Context) (app.RuntimeSettings, error) { return current, nil }
	prepared, applied := 0, 0
	b.prepareSettings = func(_ context.Context, next app.RuntimeSettings) (app.RuntimeSettingsPlan, error) {
		prepared++
		if next.NewSecret != replacement || next.ControllerPort != 9098 || next.HTTPSAddress != "[::1]:9443" || next.CertificatePath != "server.crt" || next.PrivateKeyPath != "server.key" || next.RoutingMark != 16 || next.UnixSocket != "/tmp/core.sock" || next.DoHPath != "/dns-query" || next.AllowedOrigins != "https://one.example\nhttps://two.example" || !next.AllowPrivateNetwork || next.HTTPPort != 8080 || next.MixedPort != 7897 {
			t.Fatal("the control editor lost a setting or changed a proxy listener")
		}
		next.NewSecret = ""
		return app.RuntimeSettingsPlan{Current: current, Proposed: next, Changed: true, SecretChanged: true}, nil
	}
	b.applySettings = func(_ context.Context, p app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
		applied++
		if !p.SecretChanged || p.Proposed.NewSecret != "" || p.Proposed.ControllerPort != 9098 {
			t.Fatal("apply did not receive the reviewed plan")
		}
		b.snapshot.RuntimeSettings = p.Proposed
		return app.RuntimeSettingsResult{Settings: p.Proposed, Changed: true}, nil
	}
	m := focusedPortsEntry(b, 120, 60)
	m.focus = "settings.externalControl"
	m.layout()
	m, cmd := click(m, findTarget(t, m, "settings.externalControl"))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.ports.kind != "control" || m.ports.focus != "ports.address" || m.ports.field("ports.secret").Value() != "" || hasAction(m.targets, "ports.https") {
		t.Fatal("external control did not open on its base fields")
	}
	m.ports.field("ports.address").SetValue("127.0.0.1:9098")
	m, _ = click(m, findTarget(t, m, "ports.secret"))
	m, _ = update(m, tea.PasteMsg{Content: replacement})
	if strings.Contains(ansi.Strip(m.screen), replacement) || !strings.Contains(ansi.Strip(m.screen), "•••") {
		t.Fatal("secret was not masked while editing")
	}
	m, _ = click(m, findTarget(t, m, "ports.tlsSection"))
	m.ports.field("ports.https").SetValue("[::1]:9443")
	m.ports.field("ports.cert").SetValue("server.crt")
	m.ports.field("ports.key").SetValue("server.key")
	m, _ = click(m, findTarget(t, m, "ports.corsSection"))
	m, _ = click(m, findTarget(t, m, "ports.origins"))
	m, _ = update(m, tea.PasteMsg{Content: "https://one.example"})
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, tea.PasteMsg{Content: "https://two.example"})
	m, _ = update(m, special(tea.KeyTab))
	m, _ = update(m, special(tea.KeyEnter))
	m, _ = update(m, special(tea.KeyTab))
	m, _ = update(m, special(tea.KeyEnter))
	m.ports.field("ports.mark").SetValue("0x10")
	m.ports.field("ports.unix").SetValue("/tmp/core.sock")
	m.ports.field("ports.doh").SetValue("/dns-query")
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 16})
	m, cmd = click(m, findTarget(t, m, "ports.review"))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.ports.step != "review" || prepared != 1 || applied != 0 {
		t.Fatal("review did not stop before applying")
	}
	doc, _, _ := m.portsDocument(78)
	plain := ansi.Strip(strings.Join(doc.lines, "\n"))
	for _, want := range []string{"127.0.0.1:9097 → 127.0.0.1:9098", m.tr("access.secretChange"), "https://two.example", "[::1]:9443", "0 → 16"} {
		if !strings.Contains(plain, want) {
			t.Errorf("review omitted %q", want)
		}
	}
	if strings.Contains(plain, replacement) || strings.Contains(plain, "8080") {
		t.Fatal("review exposed raw credentials or an unrelated proxy field")
	}
	m, _ = click(m, findTarget(t, m, "ports.back"))
	if m.ports.field("ports.secret").Value() != replacement || m.ports.textarea("ports.origins").Value() != "https://one.example\nhttps://two.example" {
		t.Fatal("back discarded the draft")
	}
	m, cmd = click(m, findTarget(t, m, "ports.review"))
	m, _ = update(m, actionCmd(t, cmd)())
	m, cmd = click(m, findTarget(t, m, "ports.apply"))
	m, _ = update(m, actionCmd(t, cmd)())
	if prepared != 2 || applied != 1 || !m.ports.applied || m.snapshot.RuntimeSettings.ControllerPort != 9098 || len(m.ports.access.inputs) != 0 || strings.Contains(ansi.Strip(m.screen), replacement) {
		t.Fatal("application lost the reviewed result or retained the secret draft")
	}
	m, _ = click(m, findTarget(t, m, "ports.done"))
	if m.ports.open || m.focus != "settings.externalControl" {
		t.Fatal("did not return to the external control row")
	}
}

func TestProxyPortEditorCanDisableMixedAndPreservesExternalControl(t *testing.T) {
	s := app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}
	s.AllowLAN, s.UnixSocket = true, "/tmp/controller.sock"
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true}, getSettings: func(context.Context) (app.RuntimeSettings, error) { return s, nil }}
	m := portsModel(t, b)
	m.ports.mixed.SetValue("")
	m.ports.field("ports.http").SetValue("8080")
	m.ports.field("ports.socks").SetValue("1080")
	m.ports.field("ports.redir").SetValue("7892")
	m.ports.field("ports.tproxy").SetValue("7893")
	m.layout()
	m, cmd := click(m, findTarget(t, m, "ports.review"))
	m, _ = update(m, actionCmd(t, cmd)())
	p := m.ports.plan.Proposed
	if m.ports.step != "review" || p.MixedPort != 0 || p.HTTPPort != 8080 || p.SOCKSPort != 1080 || p.RedirPort != 7892 || p.TProxyPort != 7893 || !p.AllowLAN || p.ControllerPort != 9097 || p.UnixSocket != "/tmp/controller.sock" {
		t.Fatal("proxy port editing did not isolate its five settings")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.ports.open {
		t.Fatal("Escape did not discard a reviewed draft")
	}
}

func TestProxyPortReviewRequiresAnActualValidChange(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	for _, test := range []struct {
		value   string
		port    int
		invalid bool
	}{
		{"", 0, false}, {"0", 0, false}, {"000", 0, false}, {"1", 1, false}, {"80", 80, false},
		{"65535", 65535, false}, {"65536", 0, true}, {"-1", 0, true}, {"+1", 0, true}, {"3.5", 0, true}, {"port", 0, true},
	} {
		m.ports.field("ports.http").SetValue(test.value)
		m.layout()
		settings, reviewable := m.proxyPortProposal()
		wantReview := !test.invalid && test.port != 0
		if reviewable != wantReview || hasAction(m.targets, "ports.review") != wantReview || slices.Contains(m.ports.controls, "ports.review") != wantReview {
			t.Fatalf("input %q: reviewable=%v, controls=%v", test.value, reviewable, m.ports.controls)
		}
		if (m.ports.proxyFieldError("ports.http") != "") != test.invalid || (!test.invalid && settings.HTTPPort != test.port) {
			t.Fatalf("input %q: port=%d, field error=%q", test.value, settings.HTTPPort, m.ports.proxyFieldError("ports.http"))
		}
		if !wantReview {
			m, _ = update(m, special(tea.KeyEnter))
			if m.busy || m.ports.step != "edit" {
				t.Fatalf("input %q: disabled review submitted by keyboard", test.value)
			}
		}
	}
}

func TestProxyPortZeroNormalizesOnKeyboardAndMouseBlur(t *testing.T) {
	for _, mouse := range []bool{false, true} {
		m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
		m, _ = click(m, findTarget(t, m, "ports.mixed.clear"))
		m, _ = update(m, key("0"))
		if m.ports.mixed.Value() != "0" || !hasAction(m.targets, "ports.review") {
			t.Fatal("zero did not disable the enabled mixed port")
		}
		if mouse {
			m, _ = click(m, findTarget(t, m, "ports.http"))
		} else {
			m, _ = update(m, special(tea.KeyTab))
		}
		if m.ports.focus != "ports.http" || m.ports.mixed.Value() != "" || hasAction(m.targets, "ports.mixed.clear") {
			t.Fatalf("mouse=%v: zero field failed to normalize on blur", mouse)
		}
		m, cmd := click(m, findTarget(t, m, "ports.review"))
		m, _ = update(m, actionCmd(t, cmd)())
		if m.ports.step != "review" || m.ports.plan.Proposed.MixedPort != 0 {
			t.Fatal("disabled port did not reach review as 0")
		}
	}
}

func TestProxyPortDetectsControllerAddressConflicts(t *testing.T) {
	current := app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}
	current.HTTPSAddress = "192.0.2.10:9443"
	for _, lan := range []bool{false, true} {
		current.AllowLAN = lan
		m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}, getSettings: func(context.Context) (app.RuntimeSettings, error) { return current, nil }})
		for _, tc := range []struct {
			value    string
			conflict bool
		}{{"9097", true}, {"9443", lan}} {
			m.ports.field("ports.socks").SetValue(tc.value)
			m.layout()
			if got := m.ports.proxyFieldError("ports.socks"); (got != "") != tc.conflict || hasAction(m.targets, "ports.review") == tc.conflict {
				t.Fatalf("LAN=%v SOCKS=%s: error=%q", lan, tc.value, got)
			}
		}
		if !lan {
			m.acceptPorts(portsMsg{id: m.id, kind: "prepare", err: diagnostic.New("diagnostic.settings.portUnavailable", "local %s port %s is unavailable", "tcp", current.HTTPSAddress)})
			m.layout()
			if m.ports.err == "" || m.ports.proxyFieldError("ports.socks") != "" {
				t.Fatal("an unavailable controller on another address incorrectly marked the SOCKS port")
			}
		}
	}
}

func TestProxyPortPlatformControlsEditingAndReview(t *testing.T) {
	for _, test := range []struct {
		os      string
		visible int
	}{{"windows", 3}, {"darwin", 4}, {"linux", 5}, {"android", 5}, {"", 5}} {
		t.Run(test.os, func(t *testing.T) {
			current := app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}
			current.CoreOS = test.os
			// Inaccessible stored values must not participate in conflict validation.
			if test.visible < 5 {
				current.TProxyPort = 80
			}
			if test.visible < 4 {
				current.RedirPort = 80
			}
			m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}, getSettings: func(context.Context) (app.RuntimeSettings, error) { return current, nil }})
			for i, id := range proxyFields {
				visible := i < test.visible
				if (m.ports.field(id) != nil) != visible || slices.Contains(m.ports.controls, id) != visible || hasAction(m.targets, id) != visible {
					t.Fatalf("platform %q incorrectly exposed %s; controls=%v", test.os, id, m.ports.controls)
				}
			}
			m.ports.field("ports.http").SetValue("80")
			m.layout()
			m, cmd := click(m, findTarget(t, m, "ports.review"))
			m, _ = update(m, actionCmd(t, cmd)())
			want := current
			want.HTTPPort = 80
			if m.ports.step != "review" || m.ports.plan.Proposed != want {
				t.Fatal("changing HTTP modified an unrelated runtime setting")
			}
			doc := wizardDocument{locale: m.locale, width: 60}
			m.accessReview(&doc)
			body := ansi.Strip(strings.Join(doc.lines, "\n"))
			for _, id := range proxyFields {
				if strings.Contains(body, m.tr(accessKey(id))) != (id == "ports.http") {
					t.Fatalf("review should contain only the changed HTTP listener: %s", body)
				}
			}
		})
	}
}

func TestAllowLANUpdatesOnlyThatSettingFromTheCurrentInstance(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, ConfigReady: true}}
	current := app.RuntimeSettings{MixedPort: 7897, ControllerPort: 19097}
	current.HTTPPort, current.UnixSocket = 8081, "/tmp/controller.sock"
	b.getSettings = func(context.Context) (app.RuntimeSettings, error) { return current, nil }
	writes := 0
	b.applySettings = func(_ context.Context, p app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
		want := current
		want.AllowLAN = true
		if p.Proposed != want || p.Current != current {
			t.Fatal("LAN switch overwrote settings changed after the dashboard snapshot")
		}
		b.snapshot.RuntimeSettings = p.Proposed
		writes++
		return app.RuntimeSettingsResult{Settings: p.Proposed, Changed: true}, nil
	}
	m := focusedPortsEntry(b, 120, 60)
	m, cmd := click(m, findTarget(t, m, "settings.allowLAN"))
	if m.ports.open || !m.busy {
		t.Fatal("LAN switch did not start its inline change")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if writes != 1 || !m.snapshot.RuntimeSettings.AllowLAN || m.busy {
		t.Fatal("LAN switch did not persist and refresh")
	}
}

func TestControlErrorsRestoreCollapsedSectionAndDraft(t *testing.T) {
	m := focusedPortsEntry(&fakeBackend{snapshot: app.Snapshot{Installed: true}}, 120, 60)
	m, cmd := click(m, findTarget(t, m, "settings.externalControl"))
	m, _ = update(m, actionCmd(t, cmd)())
	m.ports.textarea("ports.origins").SetValue("malformed")
	m.acceptPorts(portsMsg{id: m.id, kind: "prepare", err: diagnostic.New("diagnostic.settings.corsOrigin", "CORS requires one valid origin per line")})
	m.layout()
	if !m.ports.access.sections["ports.corsSection"] || m.ports.focus != "ports.origins" || m.ports.textarea("ports.origins").Value() != "malformed" || m.ports.err == "" {
		t.Fatal("backend error did not reveal the invalid CORS draft")
	}
	findTarget(t, m, "ports.origins")
}
