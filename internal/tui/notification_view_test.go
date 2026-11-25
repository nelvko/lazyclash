package tui

import (
	"fmt"
	"slices"
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
	if len(layers) != 1 || len(layers[0].IDs) != 0 || layers[0].Targets[0].ID != "notification.summary.8" {
		t.Fatal("oversized notification was clipped or became inaccessible", layers)
	}
}

func TestNotificationHeaderRetainsTitleAndShowsShortCompleteMessage(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {100, 24}, {80, 24}, {40, 24}, {60, 20}, {120, 18}} {
		for _, language := range []string{i18n.English, i18n.Chinese} {
			o := dashboardOptions{Locale: i18n.New(language), Width: size[0], Height: size[1], Page: "profiles", Body: strings.Repeat("body\n", 50)}
			g := dashboardGeometry(o)
			o.BodyTargets = []hitRegion{{ID: "profiles.url", W: g.width, H: 3}}
			o.Notifications = []notificationItem{{ID: 1, Page: "profiles", Context: strings.Repeat("Fetching subscription ", 8), Kind: "error", Message: "Save failed. Please retry.", Details: "full diagnostic"}}
			layers := notificationLayout(o)
			if size == [2]int{120, 18} || size == [2]int{40, 24} {
				// There is only one row below the title and no corner large enough
				// for a complete card beside it.
				if len(layers) != 1 || len(layers[0].IDs) != 0 || layers[0].Targets[0].ID != "notification.summary.1" {
					t.Fatalf("%v/%s: compact notification lost its entry point: %+v", size, language, layers)
				}
				continue
			}
			if len(layers) != 1 || len(layers[0].IDs) != 1 {
				t.Fatalf("%v/%s: short message unnecessarily hidden behind overflow: %+v", size, language, layers)
			}
			without := o
			without.Notifications = nil
			base, _, _ := renderDashboard(without)
			screen, _, _ := renderDashboard(o)
			// With no header actions the last nonblank cell of the title
			// row includes the core state when the sidebar is folded.
			title := strings.TrimRight(ansi.Strip(strings.Split(base, "\n")[g.headerY]), " ")
			if !strings.HasPrefix(ansi.Strip(strings.Split(screen, "\n")[g.headerY]), title) {
				t.Fatalf("%v/%s: notification covered page title or core state", size, language)
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

func TestBorderedNotificationBesideFoldedSidebarTitle(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Width: 100, Height: 24, Page: "connections"}
	base, _, _ := renderDashboard(o)
	titleY := dashboardGeometry(o).headerY
	title := strings.TrimRight(ansi.Strip(strings.Split(base, "\n")[titleY]), " ")
	o.Notifications = []notificationItem{{ID: 1, Kind: "error", Message: "Connection failed."}}
	layers := notificationLayout(o)
	if len(layers) != 1 || !strings.HasPrefix(ansi.Strip(layers[0].Content), "╭") {
		t.Fatal("a wide page with free body space should keep its bordered card")
	}
	screen, _, _ := renderDashboard(o)
	if !strings.HasPrefix(ansi.Strip(strings.Split(screen, "\n")[titleY]), title) {
		t.Fatal("wide card covered the menu, logo, page title or core status")
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

func TestNotificationPanelReflowsControlsAndCollapsesOnOutsideClick(t *testing.T) {
	items := []notificationItem{{ID: 1, Page: "profiles", Kind: "error", Message: "Retry saving."}}
	for _, size := range [][2]int{{120, 32}, {60, 20}, {20, 7}, {10, 4}, {1, 1}} {
		var panel notificationPanel
		panel.show(0)
		screen, targets := panel.render(items, size[0], size[1], i18n.New(i18n.English))
		assertCanvas(t, screen, panel.bounds.W, panel.bounds.H)
		assertRegions(t, targets, size[0], size[1])
		if action := panel.mouse(tea.MouseClickMsg{X: -1, Y: -1, Button: tea.MouseLeft}); action != "notification.panel.close" {
			t.Fatal("outside click activated a notification action", action)
		}
		for _, target := range targets {
			if action := panel.mouse(tea.MouseClickMsg{X: target.X, Y: target.Y, Button: tea.MouseLeft}); action != target.ID {
				t.Fatalf("mouse target %q returned %q", target.ID, action)
			}
		}
	}
}

func TestExpandedNotificationsShareToastEdgeAndFitTheirContents(t *testing.T) {
	locale := i18n.New(i18n.English)
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {40, 24}, {32, 12}, {20, 7}} {
		width, height := size[0], size[1]
		short := notificationItem{ID: 1, Kind: "error", Message: "Failed"}
		var p notificationPanel
		p.show(1)
		box, _ := p.render([]notificationItem{short}, width, height, locale)
		shortBounds := p.bounds
		assertCanvas(t, box, shortBounds.W, shortBounds.H)
		right := 2
		if sidebarVisible(width, height) {
			right = 3
		}
		if width < 24 {
			right = 0
		}
		if shortBounds.X+shortBounds.W != width-right || shortBounds.W > 54 || shortBounds.H > 7 || shortBounds.Y > 2 || shortBounds.Y+shortBounds.H >= height {
			t.Fatalf("%v: short result used excess space or moved off its anchor: %+v", size, shortBounds)
		}
		if width < 100 && shortBounds.Y == 0 {
			t.Fatalf("%v: narrow expansion covered the page title", size)
		}
		short.Details = strings.Repeat("Diagnostic line\n", 80) + "Final cause"
		p.render([]notificationItem{short}, width, height, locale)
		if p.bounds.X != shortBounds.X || p.bounds.Y != shortBounds.Y || p.bounds.W != shortBounds.W || p.bounds.H > 18 || p.viewport.AtBottom() {
			t.Fatalf("%v: long details displaced the anchor or lost their scrollable region", size)
		}
		p.key(special(tea.KeyEnd))
		box, _ = p.render([]notificationItem{short}, width, height, locale)
		if !strings.Contains(ansi.Strip(box), "Final cause") {
			t.Fatalf("%v: the end of a long diagnostic was unreachable", size)
		}
	}
}

func TestExpandedNotificationsPreservePageOutsideTheirBounds(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {40, 24}} {
		m := profileNavigationModel(size[0], size[1])
		page := m
		page.focus = "" // The notification controls own keyboard focus while expanded.
		page.renderLayout()
		background := strings.Split(ansi.Strip(page.screen), "\n")
		m.notification.items = []notificationItem{{ID: 1, Kind: "error", Message: "Refresh failed", Details: strings.Repeat("Diagnostic line\n", 30)}}
		for _, action := range []string{"notification.expand", "notification.panel.details.1"} {
			m.notificationAction(action)
			m.layout()
			p := m.notification.panel.bounds
			if !m.notification.panel.open || m.focus != "notification.panel" || m.dashboardDimmed() || !m.notificationsObscured() || len(m.notification.overlays) != 0 {
				t.Fatal("expanded notifications must own input and pause Toasts on the visible page")
			}
			frame := strings.Split(ansi.Strip(m.screen), "\n")
			for y := 0; y < m.height-1; y++ { // The footer shows the expanded controls.
				want, got := background[y], frame[y]
				if y >= p.Y && y < p.Y+p.H {
					want = cellSlice(want, 0, p.X) + cellSlice(want, p.X+p.W, m.width)
					got = cellSlice(got, 0, p.X) + cellSlice(got, p.X+p.W, m.width)
				}
				if got != want {
					t.Fatalf("%v: %s changed page content outside its bounds on row %d", size, action, y)
				}
			}
		}
	}
}

func TestNotificationListKeepsLongEntryActionsAndNextResultInView(t *testing.T) {
	items := []notificationItem{
		{ID: 1, Kind: "error", Context: strings.Repeat("长订阅名", 20), Message: strings.Repeat("很长的说明", 20) + "\n具体原因"},
		{ID: 2, Kind: "done", Message: "已选用「工作」"},
	}
	var p notificationPanel
	p.show(0)
	locale := i18n.New(i18n.Chinese)
	box, hits := p.render(items, 40, 24, locale)
	if !strings.Contains(ansi.Strip(box), items[1].Message) || !hasAction(hits, "notification.panel.details.1") || !hasAction(hits, "notification.panel.details.2") {
		t.Fatal("a long error pushed its own controls or the next result out of the compact list")
	}
	p.show(1)
	p.render(items, 40, 24, locale)
	p.key(special(tea.KeyEnd))
	box, _ = p.render(items, 40, 24, locale)
	if !strings.Contains(ansi.Strip(box), "具体原因") {
		t.Fatal("details lost the part omitted in the list")
	}
}

func TestExpandedStackFramesEachNotificationAndItsOwnControls(t *testing.T) {
	items := []notificationItem{
		{ID: 9, Kind: "done", Message: "Selected Work"},
		{ID: 17, Kind: "error", Message: "Refresh failed"},
	}
	locale := i18n.New(i18n.English)
	for _, size := range [][2]int{{160, 45}, {60, 20}, {40, 24}} {
		var p notificationPanel
		p.show(0)
		stack, targets := p.render(items, size[0], size[1], locale)
		assertCanvas(t, stack, p.bounds.W, p.bounds.H)
		rows := strings.Split(ansi.Strip(stack), "\n")
		if strings.HasPrefix(rows[0], "╭") || strings.HasSuffix(rows[len(rows)-1], "╯") {
			t.Fatalf("%v: an outer border enclosed the whole collection", size)
		}
		var starts, ends []int
		for i, row := range rows {
			if strings.HasPrefix(row, "╭") && strings.HasSuffix(row, "╮") {
				starts = append(starts, i)
			}
			if strings.HasPrefix(row, "╰") && strings.HasSuffix(row, "╯") {
				ends = append(ends, i)
			}
		}
		if len(starts) != 2 || len(ends) != 2 || ends[0] >= starts[1] {
			t.Fatalf("%v: expected two separately bordered cards:\n%s", size, strings.Join(rows, "\n"))
		}
		for i, item := range items {
			card := strings.Join(rows[starts[i]+1:ends[i]], "\n")
			if !strings.Contains(card, notificationLabel(locale, item.Kind, "")) || !strings.Contains(card, item.Message) {
				t.Fatalf("%v: card %d lost its status or result: %s", size, item.ID, card)
			}
			for _, action := range []string{"details", "dismiss"} {
				id := fmt.Sprintf("notification.panel.%s.%d", action, item.ID)
				inside := slices.ContainsFunc(targets, func(hit hitRegion) bool {
					return hit.ID == id && hit.Y > p.bounds.Y+starts[i] && hit.Y < p.bounds.Y+ends[i]
				})
				if !inside {
					t.Fatalf("%v: %s was not inside its own card", size, id)
				}
			}
		}
	}
}
