package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestNotificationButtonsKeepTheirBoundsAndMessageWhenFocused(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{16, 20, 32, 54} {
			for _, plain := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/plain=%v", language, width, plain), func(t *testing.T) {
					o := dashboardOptions{Locale: i18n.New(language), Busy: true, PlainButtons: plain}
					item := notificationItem{ID: 7, Kind: "working", Message: "Saved.", Details: "Full details"}
					before := notificationCard(item, o, width, width < 54)
					for _, target := range before.Targets {
						o.Focus, o.Hover = target.ID, target.ID
						after := notificationCard(item, o, width, width < 54)
						if after.Bounds != before.Bounds || len(after.Targets) != len(before.Targets) {
							t.Fatal("focusing a toast action changed its geometry")
						}
						for index, hit := range after.Targets {
							if hit != before.Targets[index] {
								t.Fatalf("toast control moved after focus: before=%+v after=%+v", before.Targets[index], hit)
							}
							for _, other := range after.Targets[index+1:] {
								if regionsOverlap(hit, other) {
									t.Fatalf("toast buttons overlap: %+v %+v", hit, other)
								}
							}
							painted := ansi.Strip(ansi.Cut(strings.Split(after.Content, "\n")[hit.Y], hit.X, hit.X+hit.W))
							marker := "  "
							if hit.ID == target.ID {
								marker = "› "
							}
							if !strings.HasPrefix(painted, marker) || !strings.HasSuffix(painted, " ") || strings.Trim(painted, "› ") == "" {
								t.Fatalf("toast button hit lost its marker, label, or padding: %q", painted)
							}
						}
						if !strings.Contains(ansi.Strip(after.Content), item.Message) {
							t.Fatal("toast controls displaced a short complete message")
						}
						assertCanvas(t, after.Content, width, after.Bounds.H)
						assertRegions(t, after.Targets, width, after.Bounds.H)
					}
				})
			}
		}
	}
}

func TestNotificationPanelTabMovesMarkerWithoutMovingButtons(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{80, 24}, {32, 12}} {
			for _, plain := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%dx%d/plain=%v", language, size[0], size[1], plain), func(t *testing.T) {
					items := []notificationItem{{ID: 1, Kind: "done", Message: "Saved."}}
					var panel notificationPanel
					panel.show(0)
					_, before := panel.render(items, size[0], size[1], i18n.New(language), plain)
					panel.key(special(tea.KeyTab))
					if panel.actions[panel.focus].ID != "notification.panel.dismiss.1" {
						t.Fatal("Tab did not move to Close")
					}
					screen, after := panel.render(items, size[0], size[1], i18n.New(language), plain)
					for index, target := range after {
						if target != before[index] {
							t.Fatalf("notification button moved after Tab: before=%+v after=%+v", before[index], target)
						}
						painted := ansi.Strip(ansi.Cut(strings.Split(screen, "\n")[target.Y], target.X, target.X+target.W))
						if target.ID == "notification.panel.dismiss.1" && !strings.HasPrefix(painted, "›  ") {
							t.Fatalf("focused Close button lost its separate gutter: %q", painted)
						}
						if action := panel.mouse(tea.MouseClickMsg{X: target.X + target.W - 1, Y: target.Y, Button: tea.MouseLeft}); action != target.ID {
							t.Fatalf("button's final painted cell did not activate %s: %q", target.ID, action)
						}
					}
				})
			}
		}
	}
}
