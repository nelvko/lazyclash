package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/service"
)

type profileLifecycleService struct {
	Services
	status service.Status
}

func (s *profileLifecycleService) Inspect(ctx context.Context) (service.Status, error) {
	return s.status, ctx.Err()
}

func TestProfileApplyRejectsServiceTransitionsBeforeValidation(t *testing.T) {
	for _, state := range []string{"activating", "deactivating", "reloading"} {
		t.Run(state, func(t *testing.T) {
			a, s := installedApp(t)
			a.Service = &profileLifecycleService{Services: s, status: service.Status{State: state}}
			a.Validate = func(context.Context, string, string, string) error {
				t.Fatal("profile validation began during a service transition")
				return nil
			}
			_, before, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Apply(t.Context(), "default"); err == nil || !strings.Contains(err.Error(), "changing state") {
				t.Fatalf("transition accepted: %v", err)
			}
			assertProfileApplyUntouched(t, a, s, before)
		})
	}
}

func TestProfileApplyRejectsLifecycleChangesDuringValidation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after service.Status
	}{
		{name: "external stop", before: service.Status{State: "active", Active: true, PID: 100}, after: service.Status{State: "inactive"}},
		{name: "automatic restart pending", before: service.Status{State: "active", Active: true, PID: 100}, after: service.Status{State: "activating", SubState: "auto-restart"}},
		{name: "automatic restart completed", before: service.Status{State: "active", Active: true, PID: 100}, after: service.Status{State: "active", Active: true, PID: 101}},
		{name: "external start", before: service.Status{State: "inactive"}, after: service.Status{State: "active", Active: true, PID: 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, s := installedApp(t)
			svc := &profileLifecycleService{Services: s, status: tc.before}
			a.Service = svc
			a.Validate = func(context.Context, string, string, string) error {
				svc.status = tc.after
				return nil
			}
			_, before, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Apply(t.Context(), "default"); err == nil || !strings.Contains(err.Error(), "service changed") {
				t.Fatalf("stale service observation accepted: %v", err)
			}
			assertProfileApplyUntouched(t, a, s, before)
		})
	}
}

func assertProfileApplyUntouched(t *testing.T, a *App, s *fakeService, before string) {
	t.Helper()
	_, after, err := a.load()
	if err != nil || after != before {
		t.Fatalf("rejected apply changed the active configuration: %s %v", after, err)
	}
	if len(s.actions) != 0 {
		t.Fatalf("rejected apply changed service lifecycle: %v", s.actions)
	}
	assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
	entries, err := os.ReadDir(filepath.Join(a.Home, "installation", "generations"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("rejected apply leaked a prepared generation: %v %v", entries, err)
	}
}

func TestProfileApplyJournalGuardsBothGenerations(t *testing.T) {
	a, s := installedApp(t)
	s.active = true
	_, old, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	oldGuard, err := a.generationGuard(old)
	if err != nil {
		t.Fatal(err)
	}
	checked := false
	s.actionHook = func(context.Context, string) error {
		if checked {
			return nil // allow rollback to restore the prior running generation
		}
		checked = true
		var journal pending
		if json.Unmarshal(read(t, filepath.Join(a.Home, "apply-pending.json")), &journal) != nil {
			t.Fatal("invalid profile apply journal")
		}
		newGuard, err := a.generationGuard(journal.New)
		if err != nil || journal.Old != old || journal.OldGuard != oldGuard || journal.NewGuard != newGuard || !journal.Active {
			t.Fatalf("profile apply did not guard its transaction: %+v %v", journal, err)
		}
		return errors.New("restart failed after inspecting guarded journal")
	}
	if err := a.Apply(t.Context(), "default"); err == nil || !checked {
		t.Fatalf("failed apply did not expose guarded recovery: %v", err)
	}
	_, restored, err := a.load()
	if err != nil || restored != old {
		t.Fatalf("guarded profile apply did not restore its old generation: %s %v", restored, err)
	}
	assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
}
