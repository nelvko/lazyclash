package diagnostic

import (
	"context"
	"strings"
)

// RequiresAttention reports committed changes, incomplete recovery or missing
// observations that require follow-up, including those in a joined error.
func RequiresAttention(err error) bool {
	if err == nil {
		return false
	}
	if attentionDiagnostic(err) {
		return true
	}
	switch value := err.(type) {
	case interface{ Unwrap() []error }:
		for _, cause := range value.Unwrap() {
			if RequiresAttention(cause) {
				return true
			}
		}
	case interface{ Unwrap() error }:
		return RequiresAttention(value.Unwrap())
	}
	return false
}

// CancellationOnly distinguishes a canceled operation from cancellation joined
// with a failure or wrapped in a diagnostic for an already committed change.
func CancellationOnly(err error) bool {
	if err == nil || attentionDiagnostic(err) {
		return false
	}
	switch value := err.(type) {
	case interface{ Unwrap() []error }:
		found := false
		for _, cause := range value.Unwrap() {
			if cause == nil {
				continue
			}
			found = true
			if !CancellationOnly(cause) {
				return false
			}
		}
		return found
	case interface{ Unwrap() error }:
		if cause := value.Unwrap(); cause != nil {
			return CancellationOnly(cause)
		}
	}
	return err == context.Canceled
}

func attentionDiagnostic(err error) bool {
	detail, ok := err.(*Error)
	if !ok {
		return false
	}
	if strings.HasPrefix(detail.Code, "diagnostic.recovery.") {
		return true
	}
	switch detail.Code {
	case "diagnostic.install.recoveryPending", "diagnostic.upgrade.recoveryPending", "diagnostic.upgrade.rollbackRestart", "diagnostic.upgrade.rollbackVersion",
		"diagnostic.runtime.stopInspection", "diagnostic.refresh_state", "diagnostic.wizard_refresh",
		"diagnostic.profile.refreshNotApplied", "diagnostic.profile.reloadRecovery", "diagnostic.profile.metadataSave", "diagnostic.profile.deleteAfterSwitch":
		return true
	}
	return false
}
