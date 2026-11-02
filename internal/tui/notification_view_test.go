package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestNotificationLayoutShowsNewestTwoWithoutShorteningOlderMessage(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Width: 120, Height: 32, Page: "overview"}
	for id := uint64(1); id <= 3; id++ {
		o.Notifications = append(o.Notifications, notificationItem{ID: id, Page: "overview", Context: "Save", Kind: "done", Message: fmt.Sprintf("Saved profile %d. The previous configuration is available.", id)})
	}
	layers := notificationLayout(o)
	if len(layers) != 3 || len(layers[0].IDs) != 1 || layers[0].IDs[0] != 2 || layers[1].IDs[0] != 3 {
		t.Fatalf("expected two newest complete cards followed by overflow: %+v", layers)
	}
	for index, item := range o.Notifications[1:] {
		plain := strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(layers[index].Content), "│", " ")), " ")
		if !strings.Contains(plain, "The previous configuration is available.") || !strings.Contains(plain, fmt.Sprintf("Saved profile %d.", item.ID)) {
			t.Fatalf("visible notification lost its explanation: %s", plain)
		}
	}
	if layers[0].Bounds.Y != 0 || layers[0].Bounds.Y+layers[0].Bounds.H > layers[1].Bounds.Y {
		t.Fatal("cards moved away from the top or overlap")
	}
	if !strings.Contains(ansi.Strip(layers[2].Content), "1") || layers[2].Targets[0].ID != "notification.expand" {
		t.Fatal("overflow lost its count or entry point")
	}
}

func TestNotificationLayoutKeepsInputsAccessibleAndNeverMovesBelowThem(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {120, 18}, {20, 7}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			o := dashboardOptions{Locale: i18n.New(i18n.English), Width: size[0], Height: size[1], Page: "profiles", Body: strings.Repeat("body\n", 50)}
			g := dashboardGeometry(o)
			o.BodyTargets = []hitRegion{{ID: "profiles.url", W: g.width, H: 3}}
			for id := uint64(1); id <= 3; id++ {
				o.Notifications = append(o.Notifications, notificationItem{ID: id, Page: "profiles", Kind: "error", Message: "Save failed. Check the connection and retry.", Details: "technical detail"})
			}
			layers := notificationLayout(o)
			if len(layers) == 0 {
				t.Fatal("notifications became inaccessible")
			}
			input := hitRegion{X: g.x, Y: g.y, W: g.width, H: 3}
			for _, layer := range layers {
				if regionsOverlap(layer.Bounds, input) || layer.Bounds.Y >= g.y {
					t.Fatalf("notification covered or moved below input: %+v", layer.Bounds)
				}
				assertRegions(t, append([]hitRegion{layer.Bounds}, layer.Targets...), o.Width, o.Height)
				for _, line := range strings.Split(layer.Content, "\n") {
					if ansi.StringWidth(line) != layer.Bounds.W {
						t.Fatalf("notification row width does not match bounds: %q", ansi.Strip(line))
					}
				}
			}
		})
	}
}

func TestOversizedNotificationRetainsDetailsEntryWithoutPartialToast(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Width: 60, Height: 20, Page: "profiles", Body: strings.Repeat("body\n", 50)}
	g := dashboardGeometry(o)
	o.BodyTargets = []hitRegion{{ID: "profiles.url", W: g.width, H: 3}}
	o.Notifications = []notificationItem{{ID: 8, Kind: "error", Page: "profiles", Message: strings.Repeat("Complete user explanation. ", 40), Details: "full technical details"}}
	layers := notificationLayout(o)
	if len(layers) != 1 || len(layers[0].IDs) != 0 || layers[0].Targets[0].ID != "notification.expand" {
		t.Fatal("oversized notification was clipped or became inaccessible", layers)
	}
}

func TestNotificationHeaderRetainsTitleAndShowsShortCompleteMessage(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {100, 24}, {80, 24}, {60, 20}, {120, 18}} {
		for _, language := range []string{i18n.English, i18n.Chinese} {
			o := dashboardOptions{Locale: i18n.New(language), Width: size[0], Height: size[1], Page: "profiles", Body: strings.Repeat("body\n", 50)}
			g := dashboardGeometry(o)
			o.BodyTargets = []hitRegion{{ID: "profiles.url", W: g.width, H: 3}}
			o.Notifications = []notificationItem{{ID: 1, Page: "profiles", Context: strings.Repeat("Fetching subscription ", 8), Kind: "error", Message: "Save failed. Please retry.", Details: "full diagnostic"}}
			layers := notificationLayout(o)
			if len(layers) != 1 || len(layers[0].IDs) != 1 {
				t.Fatalf("%v/%s: short message unnecessarily hidden behind overflow: %+v", size, language, layers)
			}
			titleX := g.x
			if !g.sidebar {
				titleX = max(12, g.x)
			}
			title := hitRegion{X: titleX, Y: g.headerY, W: ansi.StringWidth(pageTitle(o.Page, o.Locale)), H: 1}
			if regionsOverlap(title, layers[0].Bounds) {
				t.Fatalf("%v/%s: notification covered page title", size, language)
			}
			plain := strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(layers[0].Content), "│", " ")), " ")
			if !strings.Contains(plain, o.Notifications[0].Message) {
				t.Fatalf("%v/%s: explanation shortened: %s", size, language, plain)
			}
		}
	}
}

func TestCompactNotificationKeepsOperationContext(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {60, 20}, {120, 18}} {
		o := dashboardOptions{Locale: i18n.New(i18n.English), Width: size[0], Height: size[1], Page: "overview", Notifications: []notificationItem{{ID: 1, Page: "overview", Context: "Stop service", Kind: "error", Message: "Stop failed. Please retry.", Details: "technical details"}}}
		layers := notificationLayout(o)
		if len(layers) != 1 || !strings.Contains(ansi.Strip(layers[0].Content), "Stop service") {
			t.Fatalf("%v: compact notification lost operation context: %+v", size, layers)
		}
	}
}

func TestNotificationPanelKeepsFullMessagesAndScrollableTechnicalDetails(t *testing.T) {
	items := []notificationItem{
		{ID: 1, Page: "profiles", Kind: "error", Message: "First complete explanation.", Details: strings.Repeat("Technical paragraph\n", 80) + "last-detail-marker"},
		{ID: 2, Page: "settings", Kind: "done", Message: "Second complete explanation."},
	}
	var panel notificationPanel
	panel.show(0)
	screen, targets := panel.render(items, 80, 24, i18n.New(i18n.English))
	if !strings.Contains(ansi.Strip(screen), items[0].Message) || !strings.Contains(ansi.Strip(screen), items[1].Message) {
		t.Fatal("notification collection lost an explanation", ansi.Strip(screen))
	}
	assertRegions(t, targets, 80, 24)
	if action := panel.key(special(tea.KeyEnter)); action != "notification.panel.details.1" {
		t.Fatal("keyboard cannot inspect selected notification", action)
	}
	panel.show(1)
	panel.render(items, 80, 24, i18n.New(i18n.English))
	panel.key(special(tea.KeyEnd))
	screen, _ = panel.render(items, 80, 24, i18n.New(i18n.English))
	if !strings.Contains(ansi.Strip(screen), "last-detail-marker") {
		t.Fatal("long diagnostic could not be read to its end")
	}
	if action := panel.key(key("x")); action != "notification.panel.dismiss.1" {
		t.Fatal("detail view cannot close the specified notification", action)
	}
	if action := panel.key(special(tea.KeyEscape)); action != "notification.panel.back" {
		t.Fatal("detail Escape did not return to list", action)
	}
}

func TestNotificationPanelReflowsControlsAndBlocksOutsideClicks(t *testing.T) {
	items := []notificationItem{{ID: 1, Page: "profiles", Kind: "error", Message: "Retry saving."}}
	for _, size := range [][2]int{{120, 32}, {60, 20}, {20, 7}, {10, 4}, {1, 1}} {
		var panel notificationPanel
		panel.show(0)
		screen, targets := panel.render(items, size[0], size[1], i18n.New(i18n.English))
		assertCanvas(t, screen, size[0], size[1])
		assertRegions(t, targets, size[0], size[1])
		if action := panel.mouse(tea.MouseClickMsg{X: -1, Y: -1, Button: tea.MouseLeft}); action != "" {
			t.Fatal("outside click activated a notification action", action)
		}
		for _, target := range targets {
			if action := panel.mouse(tea.MouseClickMsg{X: target.X, Y: target.Y, Button: tea.MouseLeft}); action != target.ID {
				t.Fatalf("mouse target %q returned %q", target.ID, action)
			}
		}
	}
}
