package tui

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRoutineSuccessIsQuietButFailuresStillShowImmediately(t *testing.T) {
	m := noticeModel()
	m.begin("Switching profile", "Profile applied", func(context.Context) error { return nil }, notificationOperation{Key: "profile.apply:work", Context: "Work profile"})
	m.syncNotification()
	if len(notificationLayout(m.dashboardOptions())) != 0 {
		t.Fatal("a fast operation flashed a progress toast")
	}
	m, _ = update(m, resultMsg{id: m.id, success: "Profile applied", hasSnapshot: true, snapshot: m.snapshot})
	if len(notificationLayout(m.dashboardOptions())) != 0 || len(m.notification.items) != 1 || !m.notification.items[0].Silent {
		t.Fatal("routine success covered the page instead of remaining quiet")
	}
	item := m.notification.items[0]
	if item.started.IsZero() {
		t.Fatal("quiet success has no expiry and would accumulate indefinitely")
	}
	m.expireNotification(notificationExpiredMsg{id: item.ID, generation: item.generation})
	if len(m.notification.items) != 0 {
		t.Fatal("quiet success did not expire")
	}
	m.begin("Switching profile", "Profile applied", func(context.Context) error { return nil }, notificationOperation{Key: "profile.apply:work", Context: "Work profile"})
	m.syncNotification()
	m, _ = update(m, resultMsg{id: m.id, err: errors.New("fixture apply failed"), hasSnapshot: true, snapshot: m.snapshot})
	if len(notificationLayout(m.dashboardOptions())) == 0 || m.notification.items[0].Kind != "error" || m.notification.items[0].Silent {
		t.Fatal("failure was hidden by the quiet-success policy")
	}
}

func TestLongOperationsRevealProgressWithoutResettingDelay(t *testing.T) {
	m := noticeModel()
	m.begin("Downloading profile", "Profile saved", func(context.Context) error { return nil }, notificationOperation{Key: "profile.refresh:work", Context: "Work profile"})
	m.syncNotification()
	deadline := m.notification.items[0].showAfter
	o := m.dashboardOptions()
	o.Now = deadline.Add(-time.Nanosecond)
	if len(notificationLayout(o)) != 0 {
		t.Fatal("progress appeared before its delay")
	}
	m.message = "Still downloading profile"
	m.syncNotification()
	if m.notification.items[0].showAfter != deadline {
		t.Fatal("a progress update restarted the toast delay")
	}
	o = m.dashboardOptions()
	o.Now = deadline
	if len(notificationLayout(o)) == 0 {
		t.Fatal("long operation lost its progress and cancellation feedback")
	}
}

func TestQuietInformationDoesNotHideAttentionNotifications(t *testing.T) {
	m := noticeModel()
	failed := publishNotice(&m, "service.start", "error", "Start failed")
	m.prepareNotification("profile.cancel", "Cancel")
	m.failure, m.message, m.messageInformational = "", "Cancelled", true
	m.syncNotification()
	layers := notificationLayout(m.dashboardOptions())
	if len(layers) != 1 || len(layers[0].IDs) != 1 || layers[0].IDs[0] != failed {
		t.Fatal("ordinary information displaced the actionable error or added overflow")
	}
	m.prepareNotification("attention", "Action required")
	m.notification.critical = true
	m.message = "Manual follow-up required"
	m.syncNotification()
	if m.notification.items[len(m.notification.items)-1].Silent {
		t.Fatal("explicitly important information was suppressed")
	}
}
