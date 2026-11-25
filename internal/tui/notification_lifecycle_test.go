package tui

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

func TestBackgroundInstallPhasesShareOneNotification(t *testing.T) {
	for _, returnToOverview := range []bool{false, true} {
		m := ready(&fakeBackend{})
		m, _ = update(m, tea.WindowSizeMsg{Width: 140, Height: 40})
		m, cmd := activateWizard(m, "install")
		m, _ = update(m, key("7"))
		if len(m.notification.items) != 1 {
			t.Fatalf("leaving the installing page lost its operation: %+v", m.notification.items)
		}
		id := m.notification.items[0].ID
		origin := m.notification.operation
		for _, phase := range []string{"resolve", "prepare", "install"} {
			if m.job != phase || m.notification.operation.ID != origin.ID || len(m.notification.items) != 1 || noticeByID(t, m, id).Kind != "working" {
				t.Fatalf("phase %s lost its installation identity: %+v", phase, m.notification.items)
			}
			if phase == "install" && returnToOverview {
				m, _ = update(m, key("1"))
			}
			m, cmd = update(m, inlineOperationMsg(t, cmd))
		}
		result := noticeByID(t, m, id)
		if m.busy || m.wizard.step != stepResult || len(m.notification.items) != 1 || result.Kind != "done" || result.Page != "overview" || result.Key != "install" {
			t.Fatalf("completed installation retained a working phase or lost its source: %+v", m.notification.items)
		}
	}
}

func TestAbortingInlineLookupRetiresPublishedProgress(t *testing.T) {
	m := New(t.Context(), &fakeBackend{}, "en")
	m, cmd := update(m, m.snapshotCmd(m.id, m.opCtx, true)())
	m, _ = update(m, key("7"))
	if m.job != "check-update" || len(m.notification.items) != 1 {
		t.Fatalf("version check was not published on the other page: %+v", m.notification.items)
	}
	m, _ = update(m, key("1"))
	m, _ = activateWizard(m, "later")
	m, _ = update(m, inlineOperationMsg(t, cmd))
	if m.busy || len(m.notification.items) != 0 {
		t.Fatalf("aborted lookup retained or replayed progress: %+v", m.notification.items)
	}
}

func TestInitialSnapshotRetrySettlesBeforeOnboardingStarts(t *testing.T) {
	b := &fakeBackend{snapshotErr: errors.New("snapshot unavailable")}
	m := New(t.Context(), b, "en")
	m, _ = update(m, m.snapshotCmd(m.id, m.opCtx, true)())
	if len(m.notification.items) != 1 || m.notification.items[0].Key != "refresh" {
		t.Fatalf("startup failure has no refresh identity: %+v", m.notification.items)
	}
	id := m.notification.items[0].ID
	b.snapshotErr = nil
	cmd := m.refresh()
	m, _ = update(m, inlineOperationMsg(t, cmd))
	if m.job != "check-update" || noticeByID(t, m, id).Kind != "done" {
		t.Fatalf("starting automatic setup left the resolved failure active: %+v", m.notification.items)
	}
}

func TestSuccessfulProfileSettingsRetryResolvesCriticalFailure(t *testing.T) {
	original := app.ProfileSettings{ID: "profile", Name: "Original"}
	b := &fakeBackend{snapshot: app.Snapshot{Profiles: []string{original.ID}, ProfileDetails: profileFixtures(original.ID)},
		getProfileSettings: func(context.Context, string) (app.ProfileSettings, error) { return original, nil }}
	attempts := 0
	b.saveProfileSettings = func(_ context.Context, _ app.ProfileSettings) error {
		attempts++
		if attempts == 1 {
			b.snapshotErr = errors.New("snapshot offline")
			return errors.New("save failed")
		}
		b.snapshotErr = nil
		return nil
	}
	m := openSettingsForTest(t, profileInteractionsModel(b, 120, 32), original.ID)
	m.profileCreate.name.SetValue("New name")
	m.profileCreateSetField(profileCreateSave)
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if m.form != "profile-settings" || len(m.notification.items) != 1 || !m.notification.items[0].Critical {
		t.Fatalf("lost critical failure before retry: %+v", m.notification.items)
	}
	id := m.notification.items[0].ID
	m, cmd = update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if attempts != 2 || m.form != "" || len(m.notification.items) != 1 || noticeByID(t, m, id).Kind != "done" {
		t.Fatalf("successful retry did not settle the original failure: %+v", m.notification.items)
	}
}
