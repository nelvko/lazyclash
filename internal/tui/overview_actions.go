package tui

import "github.com/nelvko/lazyclash/internal/app"

// Stopping remains available for recovery while service status is uncertain or
// a transition is in progress. The application still verifies service ownership.
func runtimeStopIssue(s app.Snapshot) string {
	if !s.Installed {
		return "Mihomo is not installed"
	}
	capability := s.Capabilities.Runtime
	if s.Service.State == "unsupported" || !capability.Supported && (s.Capabilities.Scope != "" || capability.Reason != "") {
		if reason := clean(capability.Reason); reason != "" {
			return reason
		}
		return "Service management is unavailable"
	}
	return ""
}

func runtimeControlIssue(s app.Snapshot) string {
	if issue := runtimeStopIssue(s); issue != "" {
		return issue
	}
	if s.ServiceError != "" {
		return clean(s.ServiceError)
	}
	switch s.Service.State {
	case "activating", "deactivating", "reloading":
		return "Service is changing state; wait and retry"
	}
	return ""
}
