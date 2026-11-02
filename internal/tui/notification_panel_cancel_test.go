package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func notificationPanelTarget(m Model, id string) (hitRegion, bool) {
	for _, target := range m.targets {
		if target.ID == id {
			return target, true
		}
	}
	return hitRegion{}, false
}

func TestNotificationPanelCancelsFoldedTaskWithoutQuitting(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{60, 10}, {20, 7}} {
			for _, mouse := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%dx%d/mouse=%v", language, size[0], size[1], mouse), func(t *testing.T) {
					m := noticeModel()
					m.locale = i18n.New(language)
					m.width, m.height = size[0], size[1]
					cmd := m.begin(strings.Repeat("Waiting for the remote profile source. ", 4), "Saved", func(ctx context.Context) error {
						return ctx.Err()
					}, notificationOperation{Key: "profile.refresh:work", Context: "Work profile"})
					defer m.cancel()
					task := actionCmd(t, cmd)
					m.syncNotification()
					m.layout()
					id := m.notification.items[0].ID
					cancelID := fmt.Sprintf("notification.cancel.%d", id)
					if _, visible := notificationPanelTarget(m, cancelID); visible {
						t.Fatal("fixture did not fold the progress notification")
					}
					expand := findTarget(t, m, "notification.expand")
					if mouse {
						m, _ = click(m, expand)
					} else {
						m, _ = update(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
					}
					if !m.notification.panel.open {
						t.Fatal("could not open folded progress in the notification center")
					}
					if mouse {
						for range 100 {
							if _, visible := notificationPanelTarget(m, cancelID); visible {
								break
							}
							p := &m.notification.panel
							m, _ = update(m, tea.MouseWheelMsg{X: p.bounds.X + 3, Y: p.contentY, Button: tea.MouseWheelDown})
						}
						m, _ = click(m, findTarget(t, m, cancelID))
					} else {
						for range len(m.notification.panel.actions) + 1 {
							p := &m.notification.panel
							if p.actions[p.focus].ID == cancelID {
								break
							}
							m, _ = update(m, special(tea.KeyTab))
						}
						p := &m.notification.panel
						if p.actions[p.focus].ID != cancelID {
							t.Fatal("Cancel is not reachable with Tab")
						}
						findTarget(t, m, cancelID)
						if m.opCtx.Err() != nil {
							t.Fatal("focusing Cancel already canceled the task")
						}
						m, _ = update(m, special(tea.KeyEnter))
					}
					if !errors.Is(m.opCtx.Err(), context.Canceled) || m.quitting {
						t.Fatalf("Cancel must cancel the task without quitting: err=%v quitting=%v", m.opCtx.Err(), m.quitting)
					}
					m, _ = update(m, task())
					if m.busy || m.quitting || !m.notification.panel.open {
						t.Fatal("task cleanup required quitting or closed the notification center")
					}
					assertCanvas(t, m.screen, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
					m, _ = update(m, special(tea.KeyEscape))
					if m.notification.panel.open || m.quitting {
						t.Fatal("could not return from the notification center after canceling")
					}
				})
			}
		}
	}
}

func TestNotificationPanelCloseStillLeavesWorkingTaskRunning(t *testing.T) {
	for _, mouse := range []bool{false, true} {
		t.Run(fmt.Sprintf("mouse=%v", mouse), func(t *testing.T) {
			m := noticeModel()
			m.width, m.height = 20, 7
			m.begin("Updating a subscription", "Saved", func(context.Context) error { return nil }, notificationOperation{Key: "profile.refresh:work"})
			defer m.cancel()
			m.syncNotification()
			id := m.notification.items[0].ID
			m, _ = update(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
			m, _ = update(m, special(tea.KeyTab))
			dismissID := fmt.Sprintf("notification.panel.dismiss.%d", id)
			if p := &m.notification.panel; p.actions[p.focus].ID != dismissID {
				t.Fatal("Close is no longer the second notification action")
			}
			if mouse {
				m, _ = click(m, findTarget(t, m, dismissID))
			} else {
				m, _ = update(m, special(tea.KeyEnter))
			}
			if m.opCtx.Err() != nil || !m.busy || m.quitting || len(m.notification.items) != 0 {
				t.Fatal("closing a working notification changed or canceled its task")
			}
		})
	}
}

func TestNotificationPanelCancelControlsFitAndOnlyApplyToWorkingItems(t *testing.T) {
	items := []notificationItem{
		{ID: 1, Kind: "done", Message: "Saved"},
		{ID: 2, Kind: "working", Message: "Updating"},
		{ID: 3, Kind: "error", Message: "Unavailable"},
	}
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{20, 7}, {60, 10}, {120, 32}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size[0], size[1]), func(t *testing.T) {
				locale := i18n.New(language)
				var panel notificationPanel
				panel.show(0)
				panel.render(items, size[0], size[1], locale)
				foundCancel := false
				for range len(panel.actions) {
					screen, targets := panel.render(items, size[0], size[1], locale)
					assertCanvas(t, screen, size[0], size[1])
					assertRegions(t, targets, size[0], size[1])
					for i, target := range targets {
						for _, other := range targets[i+1:] {
							if regionsOverlap(target, other) {
								t.Fatalf("notification controls overlap: %+v / %+v", target, other)
							}
						}
						if !strings.HasPrefix(target.ID, "notification.cancel.") {
							continue
						}
						if target.ID != "notification.cancel.2" {
							t.Fatalf("non-working notification has a Cancel action: %s", target.ID)
						}
						foundCancel = true
						line := strings.Split(screen, "\n")[target.Y]
						if got := ansi.Strip(ansi.Cut(line, target.X, target.X+target.W)); got != locale.T("notification.cancel") {
							t.Fatalf("Cancel label was obscured: %q", got)
						}
						if action := panel.mouse(tea.MouseClickMsg{X: target.X, Y: target.Y, Button: tea.MouseLeft}); action != target.ID {
							t.Fatalf("Cancel mouse action returned %q", action)
						}
					}
					previous := panel.focus
					panel.key(special(tea.KeyDown))
					if panel.focus != min(previous+1, len(panel.actions)-1) {
						t.Fatal("arrow navigation skipped a notification action")
					}
				}
				if !foundCancel {
					t.Fatal("working notification Cancel never became visible")
				}
			})
		}
	}
}
