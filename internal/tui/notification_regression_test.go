package tui

import (
	"errors"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestNotificationNavigationAndLocalControlsDoNotReplayPreviousFailure(t *testing.T) {
	m := noticeModel()
	id := publishNotice(&m, "service.start", "error", "service failed")
	for _, action := range []string{"nav.profiles", "profile.0", "profiles.close", "profiles.url"} {
		m.activate(action)
		m, _ = update(m, tea.KeyReleaseMsg{})
		if len(m.notification.items) != 1 || m.notification.items[0].ID != id || m.notification.items[0].Page != "overview" {
			t.Fatalf("local action %s replayed previous failure as another operation", action)
		}
	}
}

func TestSuccessfulRefreshRetryResolvesOriginalFailure(t *testing.T) {
	m := noticeModel()
	m, _ = update(m, key("g"))
	m, _ = update(m, resultMsg{id: m.id, transient: true, err: errors.New("state temporarily unavailable")})
	if len(m.notification.items) != 1 {
		t.Fatal("failed refresh did not provide feedback")
	}
	id := m.notification.items[0].ID
	m, _ = update(m, key("g"))
	m, _ = update(m, resultMsg{id: m.id, transient: true, success: "State refreshed", hasSnapshot: true, snapshot: m.snapshot})
	if len(m.notification.items) != 1 || m.notification.items[0].ID != id || m.notification.items[0].Kind != "done" {
		t.Fatal("successful refresh retry left its original failure unresolved")
	}
}

func TestWizardMissingPostOperationSnapshotDoesNotPresentPreviousRuntimeAsKnown(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{
		Installed: true, ConfigReady: true,
		Service: service.Status{Active: true, State: "active"},
	}})
	m.openWizard(false)
	m.newJob("install")
	m, _ = update(m, wizardMsg{id: m.id, kind: "install", err: diagnostic.New("diagnostic.wizard_refresh", "refresh dashboard: %w", errors.New("unavailable"))})
	if m.snapshot.ServiceError == "" || m.snapshot.Service.State != "unknown" {
		t.Fatal("wizard retained a known runtime state after its final observation failed")
	}
}

func TestPortsMissingPostOperationSnapshotDoesNotPresentPreviousRuntimeAsKnown(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{
		Installed: true, ConfigReady: true,
		Service: service.Status{Active: true, State: "active"},
	}})
	m.ports = newPortsState()
	m.ports.open = true
	m.newJob("ports-apply")
	m, _ = update(m, portsMsg{id: m.id, kind: "apply", refreshError: errors.New("unavailable")})
	if m.snapshot.ServiceError == "" || m.snapshot.Service.State != "unknown" {
		t.Fatal("ports result retained a known runtime state after its final observation failed")
	}
}

func TestConnectionClosePreflightFailureDoesNotOverwriteUnrelatedOperation(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	id := publishNotice(&m, "service.start", "error", "service failed")
	m, _ = update(m, key("x"))
	m.runtime.snapshot.Connections.Connections = nil
	m, _ = update(m, key("y"))
	if len(m.notification.items) != 2 || noticeByID(t, m, id).Message != "service failed" {
		t.Fatal("close preflight failure overwrote an unrelated operation notification")
	}
}

func TestCriticalWizardFailureSurvivesDialogAndRetryUpdatesOriginal(t *testing.T) {
	fresh := app.Snapshot{Installed: true, ConfigReady: true, Service: service.Status{Active: true, State: "active"}}
	m := ready(&fakeBackend{snapshot: fresh})
	m.openWizard(false)
	m.newJob("install")
	m, _ = update(m, wizardMsg{id: m.id, kind: "install", err: diagnostic.New("diagnostic.recovery.serviceFailed", "service recovery failed")})
	if len(m.notification.items) != 1 || !m.notification.items[0].Critical || m.notification.items[0].Kind != "error" {
		t.Fatal("critical installation failure was discarded as dialog-only feedback")
	}
	id := m.notification.items[0].ID
	m, _ = update(m, special(tea.KeyEscape))
	if m.wizard.open || len(m.notification.items) != 1 || noticeByID(t, m, id).Kind != "error" {
		t.Fatal("closing installation dialog discarded its unresolved critical failure")
	}
	m.openWizard(false)
	m.newJob("install")
	m.message = "Retrying installation"
	m.syncNotification()
	if len(m.notification.items) != 1 || noticeByID(t, m, id).Kind != "working" {
		t.Fatal("same installation retry created another notification")
	}
	m, _ = update(m, wizardMsg{id: m.id, kind: "install", hasSnapshot: true, snapshot: fresh})
	item := noticeByID(t, m, id)
	if len(m.notification.items) != 1 || item.Kind != "done" || item.Critical || m.snapshot.ServiceError != "" || m.snapshot.Service.State != "active" {
		t.Fatal("successful retry did not update its original failure and recover known page state")
	}
	if !item.started.IsZero() {
		t.Fatal("success display time began behind the installation result dialog")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if noticeByID(t, m, id).started.IsZero() {
		t.Fatal("retry success did not start its clock when the dialog closed")
	}
}

func TestCriticalPortsFailureSurvivesDialogWithoutChangingCommittedOutcome(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true, ConfigReady: true, Service: service.Status{Active: true, State: "active"}}})
	m.width, m.height, m.focus = 120, 32, "runtime-settings"
	m.ports = newPortsState()
	m.ports.open, m.ports.returnFocus = true, "runtime-settings"
	m.newJob("ports-apply")
	m, _ = update(m, portsMsg{id: m.id, kind: "apply", refreshError: errors.New("final observation unavailable")})
	if !m.ports.applied || len(m.notification.items) != 1 || !m.notification.items[0].Critical {
		t.Fatal("refresh failure changed the committed port result or lost its uncertainty notification")
	}
	id := m.notification.items[0].ID
	m.closePorts()
	m, _ = update(m, tea.KeyReleaseMsg{})
	if m.ports.open || noticeByID(t, m, id).Kind != "error" || m.snapshot.Service.State != "unknown" {
		t.Fatal("closing port result cleared the unresolved state uncertainty")
	}
}

func TestNotificationExpirationDoesNotMoveFocusFromAnotherItem(t *testing.T) {
	m := noticeModel()
	one := publishNotice(&m, "first", "done", "Saved")
	two := publishNotice(&m, "second", "error", "Retry failed")
	m.focus = fmt.Sprintf("notification.close.%d", two)
	m.notification.restoreFocus = "nav.overview"
	first := noticeByID(t, m, one)
	m, _ = update(m, notificationExpiredMsg{id: one, generation: first.generation})
	if m.focus != fmt.Sprintf("notification.close.%d", two) {
		t.Fatal("another notification's expiration moved keyboard focus away from the selected control")
	}
}
