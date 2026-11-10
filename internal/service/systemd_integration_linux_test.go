package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Explicitly opt in on a dedicated Linux VM. These queries use the real
// manager and logind, without installing a unit or changing linger.
func TestSystemdManagerIntegration(t *testing.T) {
	if os.Getenv("LAZYCLASH_TEST_SYSTEMD") != "1" {
		t.Skip("set LAZYCLASH_TEST_SYSTEMD=1 on a dedicated systemd test machine")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	s, err := New(fmt.Sprintf("lazyclash-property-check-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPersistent(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Available(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := s.Inspect(ctx)
	if err != nil || status.State != "not-found" || status.Active || status.PID != 0 || status.Scope != CurrentScope() {
		t.Fatalf("unexpected absent service status: %+v, error: %v", status, err)
	}
	if s.Scope == User && (!status.LingerKnown || !status.Linger) || s.Scope == System && status.LingerKnown {
		t.Fatalf("incorrect scope-specific linger state: %+v", status)
	}
	t.Logf("actual manager queries passed: scope=%s, state=%s, lingerKnown=%t, linger=%t", status.Scope, status.State, status.LingerKnown, status.Linger)
}

// This opt-in experiment owns only a uniquely named, short-lived transient
// sleep service. It never starts or changes a user's Mihomo unit.
func TestSystemdProcessUptimeIntegration(t *testing.T) {
	if os.Getenv("LAZYCLASH_TEST_SYSTEMD_UPTIME") != "1" {
		t.Skip("set LAZYCLASH_TEST_SYSTEMD_UPTIME=1 on a dedicated systemd test machine")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	s, err := New(fmt.Sprintf("lazyclash-uptime-check-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	name := s.Name + ".service"
	args := []string{"--unit=" + name, "--collect", "--property=Type=exec", "--property=RuntimeMaxSec=45s", sleep, "45"}
	if s.Scope != System {
		args = append([]string{"--user"}, args...)
	}
	stoppedCleanly := false
	t.Cleanup(func() {
		if stoppedCleanly {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := s.command(cleanupCtx, "systemctl", "--user", "stop", name); err != nil {
			t.Errorf("clean up isolated transient service: %v", err)
		}
	})
	if _, err := s.command(ctx, "systemd-run", args...); err != nil {
		t.Fatal(err)
	}
	inspectRunning := func() Status {
		t.Helper()
		for {
			status, err := s.Inspect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if status.Active && status.PID > 0 && !status.StartedAt.IsZero() && status.StartTimestampMonotonic > 0 {
				return status
			}
			select {
			case <-time.After(50 * time.Millisecond):
			case <-ctx.Done():
				t.Fatal("real systemd process did not expose its start time")
				return Status{}
			}
		}
	}
	initial := inspectRunning()
	startUptime := time.Since(initial.StartedAt)
	select {
	case <-time.After(1200 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// A completely new adapter has no TUI or service-manager memory to reuse.
	reopened, err := New(s.Name)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.Inspect(ctx)
	if err != nil || current.PID != initial.PID || current.StartTimestampMonotonic != initial.StartTimestampMonotonic ||
		current.StartedAt.IsZero() || time.Since(current.StartedAt)-startUptime < time.Second {
		t.Fatalf("reopening reset real uptime: initial=%+v current=%+v err=%v", initial, current, err)
	}
	beforeRestart := time.Now()
	if err := s.Action(ctx, "restart"); err != nil {
		t.Fatal(err)
	}
	restarted := inspectRunning()
	if restarted.PID == initial.PID || restarted.StartTimestampMonotonic <= initial.StartTimestampMonotonic ||
		restarted.StartedAt.Before(beforeRestart.Add(-100*time.Millisecond)) {
		t.Fatalf("real restart retained previous process timing: initial=%+v restarted=%+v", initial, restarted)
	}
	if err := s.Action(ctx, "stop"); err != nil {
		t.Fatal(err)
	}
	stoppedCleanly = true
	stopped, err := s.Inspect(ctx)
	if err != nil || stopped.Active || stopped.PID != 0 || !stopped.StartedAt.IsZero() || stopped.StartTimestampMonotonic != 0 {
		t.Fatalf("real stop retained uptime: %+v err=%v", stopped, err)
	}
	t.Logf("actual process timing passed: initial PID=%d uptime=%s, reopened uptime=%s, restarted PID=%d uptime=%s, stopped state=%s",
		initial.PID, startUptime, time.Since(current.StartedAt), restarted.PID, time.Since(restarted.StartedAt), stopped.State)
}
