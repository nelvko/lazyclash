package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestActionableWarningPersistsAfterItsCardBecomesVisible(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {40, 24}} {
		m := connectionsModel(t, &fakeBackend{})
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.notification.items = nil
		m.keyHints.pending, m.keyHints.generation = true, 1
		m, _ = update(m, keyHintsSavedMsg{generation: 1, hidden: m.keyHints.hidden,
			err: errors.New("preference file could not be written and the setting could not be saved")})
		warning := m.notification.items[0]
		if !hasAction(m.targets, fmt.Sprintf("notification.close.%d", warning.ID)) {
			t.Fatalf("%v: expected the warning and its controls above the fixed search", size)
		}
		clipped := !strings.Contains(ansi.Strip(m.notification.overlays[0].Content), "could not be saved")
		if hasAction(m.targets, fmt.Sprintf("notification.details.%d", warning.ID)) != clipped {
			t.Fatalf("%v: Details must appear only when the cause is clipped", size)
		}
		if !strings.Contains(ansi.Strip(m.screen), m.tr("notification.warning")) || !warning.started.IsZero() {
			t.Fatalf("%v: folded warning lost severity or consumed its remaining time", size)
		}
		m, _ = update(m, notificationExpiredMsg{id: warning.ID, generation: warning.generation})
		noticeByID(t, m, warning.ID)
		m, _ = update(m, key("1"))
		m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 45})
		if !hasAction(m.targets, fmt.Sprintf("notification.close.%d", warning.ID)) || !noticeByID(t, m, warning.ID).started.IsZero() {
			t.Fatal("a visible save failure must stay available until dismissed or resolved")
		}
	}
}

func TestConnectionInspectorHidesGlobalFeedbackAndPausesQuietRecords(t *testing.T) {
	m := connectionsModel(t, &fakeBackend{})
	failure := publishNotice(&m, "independent-operation", "error", "Independent failure")
	m.prepareNotification("settings.saved", "Save")
	m.message, m.failure, m.busy = "Saved", "", false
	m, _ = update(m, tea.KeyReleaseMsg{})
	saved := m.notification.items[len(m.notification.items)-1]
	if !saved.Silent || saved.started.IsZero() {
		t.Fatal("fixture requires a quiet confirmation with an active timer")
	}
	m, _ = update(m, key("x"))
	if m.connections.detail == nil || len(m.notification.overlays) != 0 || strings.Contains(ansi.Strip(m.screen), "Independent failure") {
		t.Fatal("global toast remained behind the connection inspector")
	}
	if !noticeByID(t, m, saved.ID).started.IsZero() {
		t.Fatal("the hidden quiet confirmation consumed its deadline")
	}
	m, _ = update(m, notificationExpiredMsg{id: saved.ID, generation: saved.generation})
	m, _ = update(m, special(tea.KeyEscape))
	if len(m.notification.overlays) == 0 || noticeByID(t, m, failure).Kind != "error" || noticeByID(t, m, saved.ID).started.IsZero() {
		t.Fatal("closing the inspector did not restore feedback and resume its deadline")
	}
}

func TestNotificationEscapeDismissesTheFocusedCard(t *testing.T) {
	m := noticeModel()
	older := publishNotice(&m, "service.start", "error", "Start failed")
	newer := publishNotice(&m, "service.stop", "error", "Stop failed")
	m.focus = fmt.Sprintf("notification.close.%d", older)
	findTarget(t, m, m.focus)
	m.notification.restoreFocus = "nav.overview"
	m, _ = update(m, special(tea.KeyEscape))
	if len(m.notification.items) != 1 || m.notification.items[0].ID != newer || m.focus != "nav.overview" {
		t.Fatalf("Escape on older error removed the wrong item or lost focus: %+v, focus=%q", m.notification.items, m.focus)
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 60, Height: 20})
	publishNotice(&m, "service.restart", "error", "Restart failed")
	findTarget(t, m, "notification.expand")
	m.notification.restoreFocus, m.focus = "menu", "notification.expand"
	m, _ = update(m, special(tea.KeyEscape))
	if len(m.notification.items) != 2 || m.focus != "menu" {
		t.Fatal("leaving the overflow button should return focus and retain both errors")
	}
}

func TestSmallNotificationPanelOnlyActivatesItsVisibleControl(t *testing.T) {
	items := []notificationItem{{ID: 7, Kind: "working", Message: "Waiting"}}
	for _, size := range [][2]int{{19, 12}, {60, 6}, {2, 1}} {
		for _, detailID := range []uint64{0, 7} {
			var p notificationPanel
			p.show(detailID)
			locale := i18n.New(i18n.English)
			p.render(items, 80, 24, locale)
			if detailID == 0 {
				p.focus = len(p.actions) - 1
				if p.key(special(tea.KeyEnter)) != "notification.cancel.7" {
					t.Fatal("fixture must first expose the working task's Cancel action")
				}
			}
			_, targets := p.render(items, size[0], size[1], locale)
			if len(targets) != 1 || targets[0].ID != "notification.panel.close" {
				t.Fatalf("%v: expected only the panel close button", size)
			}
			if action := p.key(key("x")); action != "" {
				t.Fatalf("%v: an invisible notification was dismissed: %q", size, action)
			}
			if action := p.key(special(tea.KeyEnter)); action != targets[0].ID {
				t.Fatalf("%v: focused button dispatched %q", size, action)
			}
		}
	}
}

func TestFailedProfileUpdateKeepsHeaderRetryClickable(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {40, 16}, {20, 7}} {
		m, _ := profileClickModel(t, true, true, false, false)
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		anchor := findTarget(t, m, "profiles.update-all")
		m, cmd := click(m, anchor)
		if !m.busy || cmd == nil {
			t.Fatal("fixture did not start the profile update")
		}
		m, _ = update(m, resultMsg{id: m.id, err: errors.New("Subscription refresh failed"), hasSnapshot: true, snapshot: m.snapshot})
		if len(m.notification.overlays) == 0 || m.notification.items[len(m.notification.items)-1].Kind != "error" || findTarget(t, m, "profiles.update-all") != anchor {
			t.Fatalf("%v: update failure obscured or moved its retry action", size)
		}
		m, cmd = click(m, anchor)
		if !m.busy || cmd == nil {
			t.Fatalf("%v: retry was not actionable", size)
		}
		m = finishProfileJob(t, m, cmd)
		if m.busy || m.failure != "" {
			t.Fatalf("%v: retry did not finish the original operation", size)
		}
	}
}

func TestNamedSuccessRemainsReadableOnCompactProfiles(t *testing.T) {
	for _, text := range []struct{ language, name, result string }{
		{i18n.English, "Office", "Selected subscription “Office”"},
		{i18n.Chinese, "办公", "已选用订阅「办公」"},
	} {
		for _, size := range [][2]int{{40, 24}, {80, 24}, {120, 18}} {
			m, _ := profileClickModel(t, true, false, false, false)
			m.locale = i18n.New(text.language)
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			id := publishNotice(&m, "profile.apply", "done", text.result)
			if len(m.notification.overlays) != 1 || !strings.Contains(ansi.Strip(m.notification.overlays[0].Content), text.name) {
				t.Fatalf("%s/%v: compact success omitted the selected subscription name: %+v", text.language, size, m.notification.overlays)
			}
			item := noticeByID(t, m, id)
			if item.started.IsZero() || item.remaining != 3*time.Second {
				t.Fatalf("%s/%v: the complete successful selection has no three-second deadline", text.language, size)
			}
			if targets := m.notification.overlays[0].Targets; len(targets) != 1 || targets[0].ID != fmt.Sprintf("notification.close.%d", id) {
				t.Fatalf("%s/%v: complete selection offered a redundant action: %+v", text.language, size, targets)
			}
			m, _ = update(m, notificationExpiredMsg{id: id, generation: item.generation})
			if len(m.notification.items) != 0 {
				t.Fatal("completed success feedback remained on screen after its deadline")
			}
		}
	}
}

func TestResizedNotificationViewportCannotActivateOffscreenSelection(t *testing.T) {
	var p notificationPanel
	p.show(0)
	items := []notificationItem{
		{ID: 2, Kind: "done", Message: "Saved"},
		{ID: 3, Kind: "working", Message: "Waiting for profile refresh"},
	}
	locale := i18n.New(i18n.English)
	p.render(items, 80, 24, locale)
	p.focus = len(p.actions) - 1
	if p.key(special(tea.KeyEnter)) != "notification.cancel.3" {
		t.Fatal("fixture must first expose the cancel action")
	}
	_, hits := p.render(items, 20, 7, locale)
	if hasAction(hits, "notification.cancel.3") || p.key(special(tea.KeyEnter)) != "" || p.key(key("x")) != "" {
		t.Fatal("a hidden selection remained actionable after the viewport shrank")
	}
	p.key(special(tea.KeyDown))
	p.render(items, 20, 7, locale)
	if p.key(special(tea.KeyEnter)) != "notification.cancel.3" {
		t.Fatal("bringing Cancel into view did not restore the action")
	}
}

func TestTruncatedNamedSuccessRemainsAvailableUntilRead(t *testing.T) {
	m, _ := profileClickModel(t, true, false, false, false)
	m, _ = update(m, tea.WindowSizeMsg{Width: 40, Height: 24})
	name := "Office · " + strings.Repeat("Shared ", 8) + "London"
	message := "Selected subscription “" + name + "”"
	id := publishNotice(&m, "profile.apply", "done", message)
	item := noticeByID(t, m, id)
	details := fmt.Sprintf("notification.details.%d", id)
	if !hasAction(m.targets, details) || !item.started.IsZero() || strings.Contains(ansi.Strip(m.screen), "London") {
		t.Fatal("a truncated name counted as a complete selection confirmation")
	}
	m, _ = click(m, findTarget(t, m, details))
	if m.notification.panel.detailID != id || !strings.Contains(ansi.Strip(m.screen), "London") {
		t.Fatal("the complete selection was unavailable from the one-line summary")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.notification.panel.open {
		t.Fatal("Escape from a single folded result should return directly to the page")
	}
}

func TestProxyHoverPausesTheCardDisplacedByDelayedProgress(t *testing.T) {
	m := proxyHoverModel(t, 48)
	m.width = 80
	m.notification.source = m.notificationSource()
	m.notification.items = []notificationItem{
		{ID: 41, Kind: "done", Message: "Saved", remaining: 3 * time.Second},
		{ID: 42, Kind: "working", Message: "Loading", showAfter: time.Now().Add(time.Hour)},
	}
	m.layout()
	m.syncNotificationTimers(time.Now())
	saved := noticeByID(t, m, 41)
	if saved.started.IsZero() {
		t.Fatal("the visible success should already be counting down")
	}

	m.notification.items[1].showAfter = time.Now().Add(-time.Second)
	hit := findTarget(t, m, "menu")
	m, _ = update(m, tea.MouseMotionMsg{X: hit.X, Y: hit.Y})
	if !hasAction(m.targets, "notification.close.42") || !noticeByID(t, m, 41).started.IsZero() {
		t.Fatal("the hover frame displayed new progress without pausing the folded success")
	}
	m, _ = update(m, notificationExpiredMsg{id: saved.ID, generation: saved.generation})
	noticeByID(t, m, saved.ID)
}
