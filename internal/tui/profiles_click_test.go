package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

type profileClickBackend struct {
	*fakeBackend
	calls []string
	err   error
}

func (b *profileClickBackend) Apply(_ context.Context, id string) error {
	b.calls = append(b.calls, id)
	if b.err == nil {
		b.snapshot.Applied = id
	}
	return b.err
}

func (b *profileClickBackend) DeactivateProfile(ctx context.Context, expectedID string) error {
	if b.snapshot.Applied != expectedID {
		return errors.New("selection changed")
	}
	return b.Apply(ctx, "")
}

func profileClickModel(t *testing.T, installed, active, current, pending bool) (Model, *profileClickBackend) {
	t.Helper()
	const id = "p-stable-profile-id"
	applied := "default"
	if current {
		applied = id
	}
	b := &profileClickBackend{fakeBackend: &fakeBackend{snapshot: app.Snapshot{
		Installed: installed, Applied: applied, Service: service.Status{Active: active},
		Profiles: []string{id},
		ProfileDetails: []app.ProfileInfo{
			{ID: "default", Name: "default", Validated: installed},
			{ID: id, Name: "Office display name", Validated: installed, Pending: pending},
		},
	}}}
	m := profileInteractionsModel(b.fakeBackend, 160, 40)
	m.backend = b
	t.Cleanup(m.cancel)
	return m, b
}

func TestProfileClickAppliesStableIDWithoutOpeningDetails(t *testing.T) {
	for _, tt := range []struct {
		language string
		state    string
		want     string
	}{
		{i18n.English, "inactive", "Selected subscription “Office display name”"},
		{i18n.English, "active", "Selected subscription “Office display name”"},
		{i18n.Chinese, "inactive", "已选用订阅「Office display name」"},
		{i18n.Chinese, "active", "已选用订阅「Office display name」"},
	} {
		t.Run(tt.language+"/"+tt.state, func(t *testing.T) {
			active := tt.state == "active"
			m, b := profileClickModel(t, true, active, false, false)
			m.locale = i18n.New(tt.language)
			b.snapshot.Service.State = tt.state
			controls := 0
			b.control = func(context.Context, string) error { controls++; return nil }
			m, cmd := click(m, findTarget(t, m, "profile.0"))
			if !m.busy || m.form != "" || m.snapshot.Applied != "default" {
				t.Fatal("single click did not queue application while preserving the last committed selection")
			}
			m = finishProfileJob(t, m, cmd)
			if len(b.calls) != 1 || b.calls[0] != "p-stable-profile-id" || m.snapshot.Applied != b.calls[0] {
				t.Fatalf("click did not apply the stable profile ID: calls=%v selected=%s", b.calls, m.snapshot.Applied)
			}
			if len(m.notification.items) != 1 {
				t.Fatalf("expected one notification, got %+v", m.notification.items)
			}
			item := m.notification.items[0]
			if item.Kind != "done" || item.Message != tt.want || item.Details != "" {
				t.Fatalf("expected only %q, got %+v", tt.want, item)
			}
			if controls != 0 || m.snapshot.Service.Active != active {
				t.Fatal("card click performed lifecycle operations outside the shared Apply use case")
			}
		})
	}
}

func TestProfileClickCurrentSelectionNeverReapplies(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "pending"}[pending], func(t *testing.T) {
			m, b := profileClickModel(t, true, true, true, pending)
			m, cmd := click(m, findTarget(t, m, "profile.0"))
			if cmd != nil || m.busy || len(b.calls) != 0 || len(m.notification.items) != 0 {
				t.Fatal("clicking the current card queued an unnecessary reload")
			}
		})
	}
}

func TestProfileClickRequiresInstalledCore(t *testing.T) {
	m, b := profileClickModel(t, false, false, false, false)
	m, load := click(m, findTarget(t, m, "profile.0"))
	m = finishProfileSettingsLoad(t, m, load)
	if m.form != "profile-settings" || m.busy || len(b.calls) != 0 || m.snapshot.Applied != "default" {
		t.Fatal("clicking a profile without an installed core tried to apply it")
	}
}

func TestProfileClickRefreshButtonDoesNotApplyItsCard(t *testing.T) {
	m, b := profileClickModel(t, true, true, false, false)
	var refreshed string
	b.refresh = func(_ context.Context, id string) error { refreshed = id; return nil }
	m, cmd := click(m, findTarget(t, m, "profiles.refresh.0"))
	m = finishProfileJob(t, m, cmd)
	if refreshed != "p-stable-profile-id" || len(b.calls) != 0 || m.snapshot.Applied != "default" {
		t.Fatal("refresh button clicked through to its containing profile")
	}
}

func TestProfileClickFailureRetainsCommittedSelection(t *testing.T) {
	m, b := profileClickModel(t, true, true, false, false)
	b.err = errors.New("core rejected the candidate configuration")
	m, cmd := click(m, findTarget(t, m, "profile.0"))
	m = finishProfileJob(t, m, cmd)
	if len(b.calls) != 1 || m.snapshot.Applied != "default" || m.busy || !strings.Contains(m.failure, b.err.Error()) {
		t.Fatal("failed application changed selection or hid its failure", m.failure)
	}
}

func TestProfileDetailsSwitchBackToDefault(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "applied", want: ""},
		{name: "rejected", err: errors.New("could not apply default"), want: "p-stable-profile-id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, true, false)
			m, load := update(m, special(tea.KeyEnter))
			m = finishProfileSettingsLoad(t, m, load)
			b.err = test.err
			m, cmd := click(m, findTarget(t, m, "profile-settings.deactivate"))
			if !m.busy || m.snapshot.Applied != "p-stable-profile-id" {
				t.Fatal("switching back must keep the selected profile until the operation finishes")
			}
			m = finishProfileJob(t, m, cmd)
			if len(b.calls) != 1 || b.calls[0] != "" || m.snapshot.Applied != test.want {
				t.Fatalf("apply calls=%v, selection=%q", b.calls, m.snapshot.Applied)
			}
			if test.err != nil && !strings.Contains(m.failure, test.err.Error()) {
				t.Fatalf("failed to report rejection: %q", m.failure)
			}
			if test.err == nil && m.focus != "profile.0" {
				t.Fatalf("focus remained on a removed action after switching back: %q", m.focus)
			}
		})
	}
}

func TestDeactivationRejectsAChangedSelection(t *testing.T) {
	m, b := profileClickModel(t, true, true, true, false)
	m, load := update(m, special(tea.KeyEnter))
	m = finishProfileSettingsLoad(t, m, load)
	m, cmd := click(m, findTarget(t, m, "profile-settings.deactivate"))
	b.snapshot.Applied = "another-profile"
	m = finishProfileJob(t, m, cmd)
	if b.snapshot.Applied != "another-profile" || len(b.calls) != 0 || !strings.Contains(m.failure, "selection changed") {
		t.Fatal("an action on the former current profile deactivated a newer selection")
	}
}

func TestProfileDetailsOfferDefaultOnlyForInstalledCurrentProfile(t *testing.T) {
	for _, test := range []struct {
		name      string
		installed bool
		current   bool
	}{
		{name: "not installed", current: true},
		{name: "inactive profile", installed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, b := profileClickModel(t, test.installed, false, test.current, false)
			m, load := update(m, special(tea.KeyEnter))
			m = finishProfileSettingsLoad(t, m, load)
			if m.form != "profile-settings" || m.name != "p-stable-profile-id" {
				t.Fatal("expected open profile details")
			}
			for _, hit := range m.targets {
				if hit.ID == "profile-settings.deactivate" {
					t.Fatal("unavailable default action is visible")
				}
			}
			if m.busy || len(b.calls) != 0 {
				t.Fatal("opening Settings started a switch")
			}
		})
	}
}

func TestProfileClickIgnoresRightButtonAndMotion(t *testing.T) {
	for _, kind := range []string{"right", "motion", "release"} {
		t.Run(kind, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			card := findTarget(t, m, "profile.0")
			mouse := tea.Mouse{X: card.X + card.W/2, Y: card.Y + card.H/2, Button: tea.MouseRight}
			var msg tea.Msg = tea.MouseClickMsg(mouse)
			if kind == "motion" {
				msg = tea.MouseMotionMsg(mouse)
			} else if kind == "release" {
				mouse.Button = tea.MouseLeft
				msg = tea.MouseReleaseMsg(mouse)
			}
			m, _ = update(m, msg)
			if m.busy || len(b.calls) != 0 || m.snapshot.Applied != "default" {
				t.Fatal("a non-left-click event applied a profile")
			}
		})
	}
}

func TestProfileClickCannotEscapeBusyOrOverlayState(t *testing.T) {
	for _, state := range []string{"busy", "quitting", "form", "ports", "wizard", "sidebar"} {
		t.Run(state, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			card := findTarget(t, m, "profile.0")
			switch state {
			case "busy":
				m.busy = true
			case "quitting":
				m.quitting = true
			case "form":
				m.openForm()
			case "ports":
				m.ports.open = true
			case "wizard":
				m.wizard.open = true
			case "sidebar":
				m.sidebarOpen = true
			}
			job, generation := m.job, m.id
			m, _ = click(m, card)
			if len(b.calls) != 0 || m.job != job || m.id != generation || m.snapshot.Applied != "default" {
				t.Fatal("a card click escaped the current task or modal layer")
			}
		})
	}
}

func TestProfileSwitchReportsOnlyConfirmedOutcome(t *testing.T) {
	const initial = "old-profile"
	for _, tt := range []struct {
		label        string
		actionErr    error
		observed     string
		serviceIssue string
		want         string
		forbid       string
	}{
		{"ordinary failure", errors.New("candidate rejected"), initial, "", "Old subscription", ""},
		{"recovery failure", diagnostic.New("diagnostic.recovery.profileSwitch", "could not restore prior generation"), initial, "", "restore", "Old subscription"},
		{"service read failed after commit", nil, "p-stable-profile-id", "inspect failed", "could not be confirmed", "Core is running"},
		{"selection changed after commit", nil, initial, "", "Old subscription", "Office display name"},
	} {
		t.Run(tt.label, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			b.err = tt.actionErr
			b.snapshot.Applied = initial
			b.snapshot.Service.State = "active"
			b.snapshot.ProfileDetails = append(b.snapshot.ProfileDetails, app.ProfileInfo{ID: initial, Name: "Old subscription"})
			m.snapshot = b.snapshot
			m, cmd := click(m, findTarget(t, m, "profile.0"))
			result := inlineOperationMsg(t, cmd).(resultMsg)
			result.snapshot.Applied = tt.observed
			result.snapshot.ServiceError = tt.serviceIssue
			m, _ = update(m, result)
			if len(m.notification.items) != 1 {
				t.Fatal(m.notification.items)
			}
			item := m.notification.items[0]
			if item.Kind != "error" || !strings.Contains(item.Message, tt.want) ||
				(tt.forbid != "" && strings.Contains(item.Message, tt.forbid)) {
				t.Fatalf("incorrect switch outcome: %+v", item)
			}
			if tt.serviceIssue != "" && !strings.Contains(item.Details, tt.serviceIssue) {
				t.Fatal("missing service diagnostic", item)
			}
		})
	}
}

func TestProfileSwitchReplacesProgressWithObservedName(t *testing.T) {
	m, b := profileClickModel(t, true, false, false, false)
	b.snapshot.Service.State = "inactive"
	m, cmd := click(m, findTarget(t, m, "profile.0"))
	if len(m.notification.items) != 1 || m.notification.items[0].Message != m.tr("profiles.applying") {
		t.Fatalf("missing working message: %+v", m.notification.items)
	}
	workingID := m.notification.items[0].ID
	result := inlineOperationMsg(t, cmd).(resultMsg)
	result.snapshot.ProfileDetails[1].Name = "Maruko"
	m, _ = update(m, result)
	if len(m.notification.items) != 1 {
		t.Fatalf("expected one completed notification, got %+v", m.notification.items)
	}
	item := m.notification.items[0]
	if item.ID != workingID || item.Kind != "done" || item.Message != m.tr("feedback.selected", "Maruko") {
		t.Fatalf("progress was not replaced with the confirmed selection: %+v", item)
	}
}
