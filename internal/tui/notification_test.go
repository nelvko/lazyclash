package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func noticeModel() Model {
	m := ready(&fakeBackend{})
	m.width, m.height, m.focus = 160, 45, "nav.overview"
	return m
}

func publishNotice(m *Model, operation, kind, message string) uint64 {
	m.prepareNotification(operation, "Update")
	m.message, m.failure, m.messageTransient, m.messageInformational = message, "", false, kind == "info"
	m.busy, m.job = kind == "working", "action"
	if kind == "error" {
		m.failure = message
	}
	m.syncNotification()
	m.layout()
	m.syncNotificationTimers(time.Now())
	return m.notification.items[len(m.notification.items)-1].ID
}

func noticeByID(t *testing.T, m Model, id uint64) notificationItem {
	t.Helper()
	for _, item := range m.notification.items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("notification %d disappeared", id)
	return notificationItem{}
}

func TestNotificationHeadingsFollowLocaleAndOperationContext(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		locale := i18n.New(language)
		for _, kind := range []string{"done", "working", "info", "error"} {
			item := notificationItem{ID: 1, Kind: kind, Page: "profiles", Context: "Work profile", Message: "cancel-is-a-user-name"}
			heading := notificationHeading(item, locale, "")
			if !strings.Contains(heading, notificationLabel(locale, kind, "")) || !strings.Contains(heading, pageTitle("profiles", locale)) || !strings.Contains(heading, item.Context) {
				t.Fatalf("%s/%s lost type, source or operation: %s", language, kind, heading)
			}
		}
	}
}

func TestNotificationRoutineRefreshIsSilentAndCancellationIsInformational(t *testing.T) {
	m := noticeModel()
	m, _ = update(m, key("g"))
	if len(m.notification.items) != 0 {
		t.Fatal("routine refresh generated progress notification")
	}
	m, _ = update(m, resultMsg{id: m.id, transient: true, success: "State refreshed"})
	if len(m.notification.items) != 0 {
		t.Fatal("routine refresh generated result notification")
	}
	m.locale = i18n.New(i18n.Chinese)
	m.openForm()
	m, _ = update(m, special(tea.KeyEscape))
	if len(m.notification.items) != 1 || m.notification.items[0].Kind != "info" || m.notification.items[0].Message != m.tr("model.input_canceled") || m.notification.items[0].remaining != 4*time.Second {
		t.Fatal("cancellation lost localized informational semantics")
	}
}

func TestNotificationProgressResultAndRetryPreserveIdentity(t *testing.T) {
	m := noticeModel()
	m.begin("Updating", "Saved", func(context.Context) error { return nil }, notificationOperation{Key: "profile.refresh:work", Context: "Work profile"})
	m.syncNotification()
	id := m.notification.items[0].ID
	m, _ = update(m, resultMsg{id: m.id, err: errors.New("connection failed"), hasSnapshot: true, snapshot: m.snapshot})
	if len(m.notification.items) != 1 || noticeByID(t, m, id).Kind != "error" {
		t.Fatal("result created another notification for the same operation")
	}
	m.begin("Retrying", "Saved", func(context.Context) error { return nil }, notificationOperation{Key: "profile.refresh:work", Context: "Work profile"})
	m.syncNotification()
	if len(m.notification.items) != 1 || noticeByID(t, m, id).Kind != "working" {
		t.Fatal("retry did not replace its own failed operation")
	}
	m, _ = update(m, resultMsg{id: m.id, success: "Saved", hasSnapshot: true, snapshot: m.snapshot})
	item := noticeByID(t, m, id)
	if item.Kind != "done" || item.remaining != 3*time.Second || item.started.IsZero() {
		t.Fatal("retry success did not start a fresh visible success deadline")
	}
}

func TestNotificationIndependentFailuresAreNotMergedAndSurviveNavigation(t *testing.T) {
	m := noticeModel()
	one := publishNotice(&m, "profile.refresh:work", "error", "connection failed")
	two := publishNotice(&m, "service.start", "error", "connection failed")
	publishNotice(&m, "language.save", "done", "Saved")
	m.navigate("profiles")
	m.layout()
	if len(m.notification.items) != 3 || one == two || noticeByID(t, m, one).Page != "overview" || noticeByID(t, m, two).Page != "overview" {
		t.Fatal("independent failures were merged, overwritten or lost their origin")
	}
	for _, id := range []uint64{one, two} {
		item := noticeByID(t, m, id)
		m.expireNotification(notificationExpiredMsg{id: id, generation: item.generation})
		noticeByID(t, m, id)
	}
}

func TestNotificationTimersArePerItemAndIgnoreOldResults(t *testing.T) {
	m := noticeModel()
	one := publishNotice(&m, "first", "done", "Saved")
	old := noticeByID(t, m, one)
	two := publishNotice(&m, "second", "done", "Saved")
	m.expireNotification(notificationExpiredMsg{id: one, generation: old.generation})
	if len(m.notification.items) != 1 || m.notification.items[0].ID != two {
		t.Fatal("one expiration cleared an independent notification")
	}
	previous := noticeByID(t, m, two)
	m.message = "Saved again"
	m.syncNotification()
	m.syncNotificationTimers(time.Now())
	m.expireNotification(notificationExpiredMsg{id: two, generation: previous.generation})
	if noticeByID(t, m, two).Message != "Saved again" {
		t.Fatal("old timer cleared the operation's newer result")
	}
}

func TestNotificationOverflowStartsClockOnlyWhenExpandedAndVisible(t *testing.T) {
	m := noticeModel()
	m.width, m.height = 60, 20
	m.focus = "menu"
	one := publishNotice(&m, "first", "done", "First saved")
	two := publishNotice(&m, "second", "done", "Second saved")
	old := noticeByID(t, m, one)
	if !old.started.IsZero() || old.remaining <= 0 || noticeByID(t, m, two).started.IsZero() {
		t.Fatalf("overflow consumed hidden notification time: %+v", m.notification.items)
	}
	m.expireNotification(notificationExpiredMsg{id: one, generation: old.generation})
	noticeByID(t, m, one)
	m.notificationAction("notification.expand")
	m.syncNotificationTimers(time.Now())
	if !noticeByID(t, m, two).started.IsZero() {
		t.Fatal("opening the bounded list did not pause Toast timers")
	}
	m.notificationAction(fmt.Sprintf("notification.panel.dismiss.%d", two))
	m.notificationAction("notification.panel.close")
	m.syncNotificationTimers(time.Now())
	if noticeByID(t, m, one).started.IsZero() {
		t.Fatal("revealed overflow item did not begin its display clock")
	}
}

func TestNotificationDialogsHidePreviousItemsWithoutReplayingValidation(t *testing.T) {
	m := noticeModel()
	id := publishNotice(&m, "previous", "error", "previous failure")
	m.openForm()
	m.prepareNotification("dialog", "Import")
	m, _ = update(m, special(tea.KeyEnter))
	if m.form == "" || m.failure == "" || len(m.notification.items) != 1 || len(notificationLayout(m.dashboardOptions())) != 0 {
		t.Fatal("dialog validation leaked into global notification collection")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.form != "" || len(m.notification.items) != 2 || noticeByID(t, m, id).Message != "previous failure" {
		t.Fatal("closing dialog removed earlier failure or replayed validation")
	}
	for _, item := range m.notification.items {
		if item.Message == m.tr("model.value_required") {
			t.Fatal("dialog validation replayed after closing")
		}
	}
}

func TestNotificationEscapeRespectsSearchMenuAndHelp(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	id := publishNotice(&m, "close.connection", "error", "close failed")
	m, _ = update(m, key("/"))
	m, _ = update(m, special(tea.KeyEscape))
	if m.connections.editing {
		t.Fatal("Esc did not leave search")
	}
	noticeByID(t, m, id)
	m, _ = update(m, key("?"))
	m, _ = update(m, special(tea.KeyEscape))
	if m.helpOpen {
		t.Fatal("Esc did not leave help")
	}
	noticeByID(t, m, id)
	m.width, m.height, m.sidebarOpen, m.focus = 60, 20, true, "nav.connections"
	m, _ = update(m, special(tea.KeyEscape))
	if m.sidebarOpen {
		t.Fatal("Esc did not close navigation menu")
	}
	noticeByID(t, m, id)
}

func TestNotificationCloseWorksWithMouseAndTabWithoutCancelingTask(t *testing.T) {
	for _, mouse := range []bool{false, true} {
		m := noticeModel()
		m.begin("Updating", "Saved", func(context.Context) error { return nil }, notificationOperation{Key: "work", Context: "Update"})
		m.syncNotification()
		m.layout()
		id := m.notification.items[0].ID
		action := fmt.Sprintf("notification.close.%d", id)
		if mouse {
			m, _ = click(m, findTarget(t, m, action))
		} else {
			for range len(m.focusTargets()) + 1 {
				if m.focus == action {
					break
				}
				m, _ = update(m, special(tea.KeyTab))
			}
			if m.focus != action {
				t.Fatal("working notification close is not keyboard reachable")
			}
			m, _ = update(m, special(tea.KeyEnter))
		}
		if len(m.notification.items) != 0 || !m.busy || m.opCtx.Err() != nil {
			t.Fatal("closing progress canceled the task or left its card visible")
		}
	}
}

func TestNotificationCanvasRetainsGeometryAndNoClickThrough(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {100, 20}, {80, 24}, {60, 20}, {20, 7}, {10, 4}, {1, 1}} {
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ASCII} {
			m := noticeModel()
			m.width, m.height, m.colorProfile = size[0], size[1], profile
			m.layout()
			before, total := dashboardGeometry(m.dashboardOptions()), m.total
			publishNotice(&m, "failure", "error", strings.Repeat("无法保存配置/长路径/", 30))
			view := m.View().Content
			assertCanvas(t, view, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
			if profile == colorprofile.ASCII && strings.Contains(view, "\x1b") {
				t.Fatal("NO_COLOR notification retained styling")
			}
			if dashboardGeometry(m.dashboardOptions()) != before || m.total != total {
				t.Fatal("notification changed body geometry")
			}
			for _, overlay := range notificationLayout(m.dashboardOptions()) {
				for _, target := range m.targets {
					if !strings.HasPrefix(target.ID, "notification.") && regionsOverlap(target, overlay.Bounds) {
						t.Fatalf("covered control receives clicks: %+v", target)
					}
				}
			}
		}
	}
}
