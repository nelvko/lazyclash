package service

import (
	"context"
	"fmt"
	"os"
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
