package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
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
			{ID: id, Name: "Office display name", Validated: installed, Pending: pending, Refreshable: true},
		},
	}}}
	m := profileInteractionsModel(b.fakeBackend, 160, 40)
	m.backend = b
	t.Cleanup(m.cancel)
	return m, b
}

func TestProfileClickAppliesStableIDWithoutOpeningDetails(t *testing.T) {
	for _, active := range []bool{false, true} {
		name := "stopped"
		if active {
			name = "running"
		}
		t.Run(name, func(t *testing.T) {
			m, b := profileClickModel(t, true, active, false, false)
			controls := 0
			b.control = func(context.Context, string) error { controls++; return nil }
			m, cmd := click(m, findTarget(t, m, "profile.1"))
			if !m.busy || m.profilesUI.detail != "" || m.snapshot.Applied != "default" {
				t.Fatal("single click did not queue application while preserving the last committed selection")
			}
			m = finishProfileJob(t, m, cmd)
			if len(b.calls) != 1 || b.calls[0] != "p-stable-profile-id" || m.snapshot.Applied != b.calls[0] {
				t.Fatalf("click did not apply the stable profile ID: calls=%v selected=%s", b.calls, m.snapshot.Applied)
			}
			if controls != 0 || m.snapshot.Service.Active != active {
				t.Fatal("card click performed lifecycle operations outside the shared Apply use case")
			}
		})
	}
}

func TestProfileClickCurrentSelectionAppliesOnlyPendingChanges(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "pending"}[pending], func(t *testing.T) {
			m, b := profileClickModel(t, true, true, true, pending)
			m, cmd := click(m, findTarget(t, m, "profile.1"))
			if pending {
				m = finishProfileJob(t, m, cmd)
				if len(b.calls) != 1 || b.calls[0] != "p-stable-profile-id" {
					t.Fatal("clicking the current profile did not apply its pending changes")
				}
			} else if cmd != nil || m.busy || len(b.calls) != 0 {
				t.Fatal("clicking an unchanged current profile queued an unnecessary reload")
			}
		})
	}
}

func TestProfileClickRequiresInstalledCore(t *testing.T) {
	m, b := profileClickModel(t, false, false, false, false)
	m, _ = click(m, findTarget(t, m, "profile.1"))
	if m.busy || len(b.calls) != 0 || m.snapshot.Applied != "default" {
		t.Fatal("clicking a profile without an installed core tried to apply it")
	}
}

func TestProfileClickRefreshButtonDoesNotApplyItsCard(t *testing.T) {
	m, b := profileClickModel(t, true, true, false, false)
	var refreshed string
	b.refresh = func(_ context.Context, id string) error { refreshed = id; return nil }
	m, cmd := click(m, findTarget(t, m, "profiles.refresh.1"))
	m = finishProfileJob(t, m, cmd)
	if refreshed != "p-stable-profile-id" || len(b.calls) != 0 || m.snapshot.Applied != "default" {
		t.Fatal("refresh button clicked through to its containing profile")
	}
}

func TestProfileClickFailureRetainsCommittedSelection(t *testing.T) {
	m, b := profileClickModel(t, true, true, false, false)
	b.err = errors.New("core rejected the candidate configuration")
	m, cmd := click(m, findTarget(t, m, "profile.1"))
	m = finishProfileJob(t, m, cmd)
	if len(b.calls) != 1 || m.snapshot.Applied != "default" || m.busy || !strings.Contains(m.failure, b.err.Error()) {
		t.Fatal("failed application changed selection or hid its failure", m.failure)
	}
}

func TestProfileClickIgnoresRightButtonAndMotion(t *testing.T) {
	for _, kind := range []string{"right", "motion", "release"} {
		t.Run(kind, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			card := findTarget(t, m, "profile.1")
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
	for _, state := range []string{"busy", "quitting", "form", "help", "ports", "wizard", "sidebar"} {
		t.Run(state, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, false, false)
			card := findTarget(t, m, "profile.1")
			switch state {
			case "busy":
				m.busy = true
			case "quitting":
				m.quitting = true
			case "form":
				m.openForm()
			case "help":
				m.helpOpen = true
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
