package diagnostic

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestRequiresAttentionTraversesJoinedDiagnostics(t *testing.T) {
	for _, code := range []string{"diagnostic.recovery.serviceFailed", "diagnostic.install.recoveryPending", "diagnostic.upgrade.rollbackRestart", "diagnostic.upgrade.rollbackVersion", "diagnostic.upgrade.recoveryPending", "diagnostic.runtime.stopInspection", "diagnostic.refresh_state", "diagnostic.wizard_refresh"} {
		t.Run(code, func(t *testing.T) {
			err := errors.Join(
				New("diagnostic.download.timeout", "download failed: %w", context.Canceled),
				fmt.Errorf("apply failed: %w", errors.Join(errors.New("connection lost"), New(code, "recovery failed: %w", errors.New("permission denied")))),
			)
			if !RequiresAttention(err) {
				t.Fatal("a later joined recovery failure was hidden by the first diagnostic")
			}
		})
	}
	for _, err := range []error{nil, context.Canceled, errors.New("rollback failed"), New("diagnostic.profile.serviceChanging", "service is changing state")} {
		if RequiresAttention(err) {
			t.Fatalf("ordinary feedback was classified by its text: %v", err)
		}
	}
}

func TestCancellationOnlyRequiresEveryCauseToBeCancellation(t *testing.T) {
	for _, sample := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"cancelled", context.Canceled, true},
		{"wrapped", fmt.Errorf("request ended: %w", context.Canceled), true},
		{"joined cancellations", errors.Join(context.Canceled, fmt.Errorf("download: %w", context.Canceled)), true},
		{"saved source still needs runtime update", New("diagnostic.profile.refreshNotApplied", "source saved: %w", context.Canceled), false},
		{"canceled reload with recovery failure", New("diagnostic.profile.reloadRecovery", "recovery incomplete: %w", errors.Join(context.Canceled, errors.New("recovery failed"))), false},
		{"timeout", context.DeadlineExceeded, false},
		{"ordinary error", errors.New("context canceled"), false},
		{"cleanup failed", errors.Join(context.Canceled, errors.New("cleanup failed")), false},
		{"critical cancellation", New("diagnostic.recovery.serviceFailed", "recovery: %w", context.Canceled), false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			if got := CancellationOnly(sample.err); got != sample.want {
				t.Fatalf("CancellationOnly(%v) = %t, want %t", sample.err, got, sample.want)
			}
		})
	}
}
