package tui

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRoutineSuccessIsQuietButFailuresStillShowImmediately(t *testing.T) {
	m := noticeModel()
	m.begin("Saving theme", "Theme saved", func(context.Context) error { return nil }, notificationOperation{Key: "theme", Context: "Theme"})
	m.syncNotification()
	if len(notificationLayout(m.dashboardOptions())) != 0 {
		t.Fatal("a fast operation flashed a progress toast")
	}
	m, _ = update(m, resultMsg{id: m.id, success: "Theme saved", hasSnapshot: true, snapshot: m.snapshot})
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
	m.begin("Saving theme", "Theme saved", func(context.Context) error { return nil }, notificationOperation{Key: "theme", Context: "Theme"})
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

func TestNotificationSuccessDeliveryFollowsOperationAndVisibleResult(t *testing.T) {
	for _, tc := range []struct {
		key, origin, page string
		popup             bool
	}{
		{"profile.apply:work", "profiles", "profiles", true},
		{"profile.add:work", "profiles", "overview", true},
		{"profile.create:work", "profiles", "profiles", true},
		{"profile.delete:work", "profiles", "profiles", true},
		{"profile.deactivate:work", "profiles", "profiles", true},
		{"profile.refresh:work", "profiles", "profiles", true},
		{"profile.refresh-all", "profiles", "profiles", true},
		{"service.restart", "overview", "overview", true},
		{"terminal-proxy", "overview", "overview", true},
		{"service.start", "overview", "overview", false},
		{"service.stop", "overview", "settings", true},
		{"tun", "overview", "overview", false},
		{"tun", "overview", "settings", true},
		{"install", "overview", "settings", true},
		{"node.select:group", "proxies", "proxies", false},
		{"mode.direct", "overview", "overview", false},
		{"connections.close-all", "connections", "connections", false},
		{"check-update", "overview", "settings", false},
	} {
		t.Run(tc.key+"/"+tc.page, func(t *testing.T) {
			m := noticeModel()
			m.page = tc.origin
			m.snapshot.Installed = true
			m.wizard.inline = false
			m.begin("Working", "Finished", func(context.Context) error { return nil }, notificationOperation{Key: tc.key})
			m.syncNotification()
			if len(m.notification.items) != 1 {
				t.Fatalf("expected one in-flight operation, got %+v", m.notification.items)
			}
			progress := m.notification.items[0]
			m.page = tc.page
			m, _ = update(m, resultMsg{id: m.id, success: "Finished", snapshot: m.snapshot, hasSnapshot: true})
			if len(m.notification.items) != 1 {
				t.Fatalf("expected one result for %s, got %+v", tc.key, m.notification.items)
			}
			item := m.notification.items[0]
			if item.ID != progress.ID || item.Kind != "done" || item.Silent == tc.popup || item.remaining != 3*time.Second {
				t.Fatalf("unexpected result for %s on %s: %+v", tc.key, tc.page, item)
			}
		})
	}
}

func TestControlNotificationReportsUnconfirmedModeAndShellActivation(t *testing.T) {
	m := noticeModel()
	m.begin("Changing mode", "Mode saved", func(context.Context) error { return nil }, notificationOperation{Key: "mode.direct"})
	snapshot := m.snapshot
	snapshot.Service.Active = true
	snapshot.Mode.Desired, snapshot.Mode.Actual = "direct", "rule"
	m, _ = update(m, resultMsg{id: m.id, success: "Mode saved", snapshot: snapshot, actionSucceeded: true, hasSnapshot: true})
	item := m.notification.items[0]
	if item.Kind != "warning" || item.Silent || item.Message != m.tr("notification.modeUnconfirmed") {
		t.Fatalf("unconfirmed mode: %+v", item)
	}
	m.begin("Updating shell", "Saved", func(context.Context) error { return nil }, notificationOperation{Key: "terminal-proxy"})
	snapshot.TerminalProxy.State = "pending"
	snapshot.TerminalProxy.Pending = true
	snapshot.TerminalProxy.ActivationCommand = "source private-path"
	m, _ = update(m, resultMsg{id: m.id, success: "Saved", snapshot: snapshot, actionSucceeded: true, hasSnapshot: true})
	item = m.notification.items[len(m.notification.items)-1]
	if item.Silent || item.Message != m.tr("terminal.savedActivation") {
		t.Fatalf("pending shell activation: %+v", item)
	}
}
