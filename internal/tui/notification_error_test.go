package tui

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestNotificationCriticalTraversesJoinedDiagnostics(t *testing.T) {
	for _, code := range []string{"diagnostic.recovery.serviceFailed", "diagnostic.install.recoveryPending", "diagnostic.runtime.stopInspection", "diagnostic.refresh_state", "diagnostic.wizard_refresh"} {
		t.Run(code, func(t *testing.T) {
			err := errors.Join(
				diagnostic.New("diagnostic.download.timeout", "download failed: %w", context.Canceled),
				fmt.Errorf("apply failed: %w", errors.Join(errors.New("connection lost"), diagnostic.New(code, "recovery failed: %w", errors.New("permission denied")))),
			)
			if !notificationCritical(err) {
				t.Fatal("a later joined recovery failure was hidden by the first diagnostic")
			}
		})
	}
	for _, err := range []error{nil, context.Canceled, errors.New("rollback failed"), diagnostic.New("diagnostic.profile.serviceChanging", "service is changing state")} {
		if notificationCritical(err) {
			t.Fatalf("ordinary feedback was classified by its text: %v", err)
		}
	}
}

func TestNotificationCancellationRequiresEveryCauseToBeCancellation(t *testing.T) {
	for _, sample := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"cancelled", context.Canceled, true},
		{"wrapped", fmt.Errorf("request ended: %w", context.Canceled), true},
		{"joined cancellations", errors.Join(context.Canceled, fmt.Errorf("download: %w", context.Canceled)), true},
		{"timeout", context.DeadlineExceeded, false},
		{"ordinary error", errors.New("context canceled"), false},
		{"cleanup failed", errors.Join(context.Canceled, errors.New("cleanup failed")), false},
		{"critical cancellation", diagnostic.New("diagnostic.recovery.serviceFailed", "recovery: %w", context.Canceled), false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			if got := notificationCancellation(sample.err); got != sample.want {
				t.Fatalf("notificationCancellation(%v) = %t, want %t", sample.err, got, sample.want)
			}
		})
	}
}

func TestUnknownOperationStatePreservesInstallationAndProfileLibrary(t *testing.T) {
	m := ready(&fakeBackend{snapshot: app.Snapshot{
		Installed: true, ConfigReady: true, Version: "installed", RunningVersion: "previous",
		Applied: "saved", Profiles: []string{"default", "saved"},
		Service:      service.Status{Active: true, State: "active", PID: 42},
		ServiceError: "old diagnostic", ServiceDiagnostic: errors.New("old cause"),
		Groups: []mihomo.Group{{Name: "PROXY"}}, Mode: app.ModeStatus{Desired: "rule", Actual: "global"},
	}})
	m.markOperationStateUnknown()
	if !m.snapshotReady || !m.snapshot.Installed || !m.snapshot.ConfigReady || m.snapshot.Version != "installed" || m.snapshot.Applied != "saved" || len(m.snapshot.Profiles) != 2 {
		t.Fatal("an unavailable runtime observation discarded the installed profile library")
	}
	if m.snapshot.ServiceError == "" || m.snapshot.ServiceDiagnostic != nil || m.snapshot.Service.State != "unknown" || !m.snapshot.Service.Active || m.snapshot.Service.PID != 42 {
		t.Fatal("the last observation was presented as stopped or retained a superseded diagnostic")
	}
	if len(m.snapshot.Groups) != 0 || m.snapshot.Mode.Actual != "" || m.snapshot.Mode.Desired != "rule" || m.snapshot.RunningVersion != "" || m.snapshot.APIError == "" {
		t.Fatal("unknown runtime retained unconfirmed actual configuration")
	}
}

func TestUnknownFirstInstallationDoesNotClaimCoreIsAbsent(t *testing.T) {
	m := ready(&fakeBackend{})
	m.markOperationStateUnknown()
	status, _ := coreSummary(m.snapshot, m.locale)
	if status != m.tr("dashboard.status.unknown") {
		t.Fatal("missing final installation observation was presented as not installed", status)
	}
}
