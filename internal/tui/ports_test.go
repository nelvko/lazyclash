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
	"github.com/nelvko/lazyclash/internal/service"
)

func portsModel(t *testing.T, backend *fakeBackend) Model {
	t.Helper()
	m := ready(backend)
	m.width, m.height, m.page, m.focus = 120, 32, "settings", "runtime-settings"
	m.layout()
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

func reviewPorts(t *testing.T, m Model) Model {
	t.Helper()
	m.ports.mixed.SetValue("18080")
	m.ports.controller.SetValue("19090")
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
				if p.Proposed.MixedPort != 18080 || p.Proposed.ControllerPort != 19090 || p.Active != active {
					t.Fatal("application did not receive the reviewed settings")
				}
				b.snapshot.Controller = "127.0.0.1:19090"
				return app.RuntimeSettingsResult{Settings: p.Proposed, Controller: b.snapshot.Controller, Restarted: active, Changed: true}, nil
			}
			m := reviewPorts(t, portsModel(t, b))
			if applies != 0 || m.ports.focus != "ports.apply" {
				t.Fatal("preflight applied a change without a separate confirmation")
			}
			plain := ansi.Strip(m.screen)
			if active && !strings.Contains(plain, "active connections will be interrupted") {
				t.Fatal("active service change omitted its restart impact")
			}
			if !active && !strings.Contains(plain, "will remain stopped") {
				t.Fatal("stopped service behavior was not explained")
			}
			m, cmd := click(m, findTarget(t, m, "ports.apply"))
			m, duplicate := update(m, special(tea.KeyEnter))
			if duplicate != nil || !m.busy || m.ports.step != "applying" {
				t.Fatal("duplicate Apply started a second task")
			}
			m, _ = update(m, actionCmd(t, cmd)())
			if applies != 1 || m.busy || !m.ports.applied || m.ports.step != "result" || m.snapshot.Controller != "127.0.0.1:19090" {
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
	if m.busy || m.ports.mixed.Err == nil || m.ports.err == "" {
		t.Fatal("invalid port started a backend task without an input error")
	}
	m.ports.mixed.SetValue("")
	m, _ = update(m, tea.PasteMsg{Content: "18080"})
	m, _ = update(m, special(tea.KeyTab))
	m.ports.controller.SetValue("")
	m, _ = update(m, tea.PasteMsg{Content: "18080"})
	m, _ = update(m, special(tea.KeyEnter))
	if m.busy || m.ports.controller.Err == nil || !strings.Contains(m.ports.err, "must differ") {
		t.Fatal("duplicate listener ports were accepted")
	}
}

func TestRuntimePortsResizePreservesDraftAndKeepsActionsAccessible(t *testing.T) {
	m := portsModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	m.ports.mixed.SetValue("18080")
	m.ports.controller.SetValue("19090")
	id := m.id
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {120, 18}, {20, 5}, {120, 32}} {
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if !m.ports.open || m.ports.mixed.Value() != "18080" || m.ports.controller.Value() != "19090" || m.ports.focus != "ports.mixed" || m.id != id {
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

func TestRuntimePortsModalStopsBackgroundAndDoesNotClickThrough(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true}})
	m.width, m.height, m.page, m.focus = 120, 32, "settings", "runtime-settings"
	m.layout()
	background := findTarget(t, m, "nav.overview")
	m, cmd := click(m, findTarget(t, m, "runtime-settings"))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.runtimeVisible() || m.rulesVisible() {
		t.Fatal("background monitoring remained visible through the modal")
	}
	for _, hit := range m.targets {
		if !strings.HasPrefix(hit.ID, "ports.") {
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
			return app.RuntimeSettingsPlan{}, errors.New("local tcp port is unavailable")
		}
		return app.RuntimeSettingsPlan{Proposed: settings, Active: true, Changed: true}, nil
	}
	b.applySettings = func(context.Context, app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
		applies++
		return app.RuntimeSettingsResult{}, nil
	}
	m := portsModel(t, b)
	m.ports.mixed.SetValue("18080")
	m.ports.controller.SetValue("19090")
	m.ports.focus = "ports.controller"
	m, cmd := update(m, special(tea.KeyEnter))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.message != "" || m.ports.step != "edit" || m.ports.focus != "ports.mixed" || m.ports.mixed.Value() != "18080" || m.ports.controller.Value() != "19090" || !strings.Contains(m.ports.err, "unavailable") {
		t.Fatal("conflict discarded the editable draft or did not expose the error")
	}
	m, _ = update(m, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m, _ = update(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m, _ = update(m, tea.PasteMsg{Content: "18081"})
	m, cmd = update(m, special(tea.KeyEnter))
	m, _ = update(m, actionCmd(t, cmd)())
	if checks != 2 || applies != 0 || m.ports.step != "review" || m.ports.err != "" || m.ports.plan.Proposed != (app.RuntimeSettings{MixedPort: 18081, ControllerPort: 19090}) {
		t.Fatal("corrected ports were not reviewed independently before applying")
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
