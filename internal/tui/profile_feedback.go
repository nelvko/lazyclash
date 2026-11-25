package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/configuration"
)

func (m *Model) profileFeedbackName(id string) string {
	return configuration.DisplayName(snapshotProfileName(m.snapshot, id, m.locale), id)
}

func (m *Model) beginProfileImport(draft app.ProfileDraft, operation, label string) tea.Cmd {
	locale, backend := m.locale, m.backend
	return m.beginResult(label, func(ctx context.Context) (string, error) {
		name, err := backend.ImportProfile(ctx, draft)
		if err != nil {
			return "", err
		}
		return locale.T("feedback.profile_created", name), nil
	}, notificationOperation{Key: notificationKey(operation, draft.Address), Context: label})
}

// The saved selection and a running core are distinct observations. A
// successful commit alone cannot establish which profile is now in use.
func (m *Model) finishProfileSelection(result resultMsg) {
	key := m.notification.operation.Key
	switching := strings.HasPrefix(key, "profile.apply:")
	deactivating := strings.HasPrefix(key, "profile.deactivate:")
	if (!switching && !deactivating) || !result.hasSnapshot || m.notification.critical || m.messageInformational {
		return
	}
	s := result.snapshot
	noProfile := s.Applied == "" || s.Applied == "default"
	targetConfirmed := switching && key == notificationKey("profile.apply", s.Applied) || deactivating && noProfile
	name := configuration.DisplayName(snapshotProfileName(s, s.Applied, m.locale), s.Applied)
	current := m.tr("feedback.current", name)
	if noProfile {
		current = m.tr("profiles.none_selected")
	}
	running := s.Service.Active && s.Service.State == "active"
	stopped := !s.Service.Active && s.Service.State == "inactive"
	runtimeKnown := s.Installed && s.ServiceError == "" && (stopped || running && s.APIError == "")
	if result.err != nil {
		if !targetConfirmed && s.Applied == result.previousProfile && runtimeKnown {
			// Preserve the concrete failure in Details when its text does not fit a card.
			withReason := current + "\n" + m.failure
			if ansi.StringWidth(withReason) <= 200 {
				m.failure = withReason
			} else {
				m.failure = m.tr("feedback.switch_failed", current)
			}
		}
		return
	}
	if !result.actionSucceeded {
		return
	}
	if !targetConfirmed {
		m.failure = m.tr("feedback.selection_changed", current)
		return
	}
	if !runtimeKnown {
		m.failure = m.tr("feedback.runtime_unconfirmed", result.success)
		switch {
		case s.ServiceError != "":
			m.notification.details = m.locale.Diagnostic(s.ServiceError, s.ServiceDiagnostic)
		case s.Service.Active && s.APIError != "":
			m.notification.details = m.locale.Diagnostic(s.APIError, s.APIDiagnostic)
		}
		return
	}
	if switching {
		m.message = m.tr("feedback.selected", name)
	}
	if running {
		m.message += "\n" + m.tr("feedback.core_running")
	} else {
		m.message += "\n" + m.tr("feedback.next_start")
	}
}
