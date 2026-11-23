package tui

import (
	"context"
	"strings"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// Inspect every branch: a cancelled request can also carry a failed rollback.
func notificationCritical(err error) bool {
	if err == nil {
		return false
	}
	if detail, ok := err.(*diagnostic.Error); ok {
		if strings.HasPrefix(detail.Code, "diagnostic.recovery.") {
			return true
		}
		switch detail.Code {
		case "diagnostic.install.recoveryPending", "diagnostic.upgrade.rollbackRestart", "diagnostic.upgrade.rollbackVersion", "diagnostic.upgrade.recoveryPending", "diagnostic.runtime.stopInspection", "diagnostic.refresh_state", "diagnostic.wizard_refresh":
			return true
		}
	}
	switch value := err.(type) {
	case interface{ Unwrap() []error }:
		for _, cause := range value.Unwrap() {
			if notificationCritical(cause) {
				return true
			}
		}
	case interface{ Unwrap() error }:
		return notificationCritical(value.Unwrap())
	}
	return false
}

func notificationCancellation(err error) bool {
	if err == nil || notificationCritical(err) {
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
			if !notificationCancellation(cause) {
				return false
			}
		}
		return found
	case interface{ Unwrap() error }:
		if cause := value.Unwrap(); cause != nil {
			return notificationCancellation(cause)
		}
	}
	return err == context.Canceled
}

func (m *Model) markOperationStateUnknown() {
	message := m.tr("notification.state_unknown")
	m.snapshot.ServiceError, m.snapshot.ServiceDiagnostic = message, nil
	m.snapshot.Service.State = "unknown"
	m.snapshot.Groups = nil
	m.snapshot.APIError, m.snapshot.APIDiagnostic = message, nil
	m.snapshot.Mode.Actual = ""
	m.snapshot.RunningVersion = ""
}
