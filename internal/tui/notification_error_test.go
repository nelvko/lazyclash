package tui

import (
	"errors"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

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
