package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestOccupiedDefaultPortsAllowStoppedInstallationButPreventStart(t *testing.T) {
	a, svc := testApp(t)
	a.CheckPorts = func(string, int) error { return errors.New("fixture port occupied") }
	local := filepath.Join(t.TempDir(), "mihomo")
	if err := os.WriteFile(local, []byte("fixture engine"), 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PrepareInstall(t.Context(), mihomo.Release{}, local, false)
	if err != nil || plan.BlockedReason != "" || len(plan.Warnings) == 0 || !strings.Contains(strings.Join(plan.Warnings, " "), "change ports in Settings") {
		t.Fatalf("install preflight incorrectly blocked on ports it will not open: %+v %v", plan, err)
	}
	if _, err := a.InstallCore(t.Context(), plan, downloadnet.Config{}, nil); err != nil {
		t.Fatal(err)
	}
	if svc.active || len(svc.actions) != 0 {
		t.Fatal("install touched a running service or opened runtime ports")
	}
	if err := a.Control(t.Context(), "start"); err == nil || !strings.Contains(err.Error(), "fixture port occupied") {
		t.Fatal("start did not recheck occupied runtime ports", err)
	}
	if svc.active || len(svc.actions) != 0 {
		t.Fatal("failed start stopped another service or started the managed unit")
	}
}
