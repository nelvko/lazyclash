package tui

import (
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

// Stopping remains available for recovery while service status is uncertain or
// a transition is in progress. The application still verifies service ownership.
func runtimeStopIssue(s app.Snapshot, locales ...i18n.Localizer) string {
	var locale i18n.Localizer
	if len(locales) > 0 {
		locale = locales[0]
	}
	if !s.Installed {
		return locale.T("runtime.not_installed")
	}
	capability := s.Capabilities.Runtime
	if s.Service.State == "unsupported" || !capability.Supported && (s.Capabilities.Scope != "" || capability.Reason != "") {
		if reason := clean(capability.Reason); reason != "" {
			return locale.Detail(reason)
		}
		return locale.T("runtime.management_unavailable")
	}
	return ""
}

func runtimeControlIssue(s app.Snapshot, locales ...i18n.Localizer) string {
	var locale i18n.Localizer
	if len(locales) > 0 {
		locale = locales[0]
	}
	if issue := runtimeStopIssue(s, locale); issue != "" {
		return issue
	}
	if s.ServiceError != "" {
		return clean(locale.Diagnostic(s.ServiceError, s.ServiceDiagnostic))
	}
	switch s.Service.State {
	case "activating", "deactivating", "reloading":
		return locale.T("runtime.transition")
	}
	return ""
}
