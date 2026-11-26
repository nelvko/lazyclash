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

func TestNotificationLayoutSizesAndAlignsNewestCards(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Width: 120, Height: 32, Page: "overview"}
	for i, message := range []string{"Old result", "Saved", "Selected subscription “Office network”"} {
		o.Notifications = append(o.Notifications, notificationItem{ID: uint64(i + 1), Kind: "done", Message: message})
	}
	layers := notificationLayout(o)
	if len(layers) != 3 || len(layers[0].IDs) != 1 || layers[0].IDs[0] != 2 || len(layers[1].IDs) != 1 || layers[1].IDs[0] != 3 {
		t.Fatalf("expected the two newest cards followed by overflow: %+v", layers)
	}
	edge := o.Width - 3
	for _, layer := range layers {
		if layer.Bounds.X+layer.Bounds.W != edge {
			t.Fatalf("card or overflow label left its right anchor: %+v", layer.Bounds)
		}
	}
	if layers[0].Bounds.W != 26 || layers[1].Bounds.W <= layers[0].Bounds.W || layers[1].Bounds.W > maxNotificationWidth {
		t.Fatalf("the card width did not follow its content: %+v / %+v", layers[0].Bounds, layers[1].Bounds)
	}
	for index, item := range o.Notifications[1:] {
		rows := strings.Split(ansi.Strip(layers[index].Content), "\n")
		if len(rows) != 3 || strings.TrimSpace(rows[0]+rows[2]) != "" ||
			!strings.Contains(rows[1], "✓ "+item.Message) || !strings.Contains(rows[1], "×") {
			t.Fatalf("expected a borderless card with padded result and close control: %q", rows)
		}
	}
	if layers[0].Bounds.Y != 0 || layers[1].Bounds.Y <= layers[0].Bounds.Y+layers[0].Bounds.H {
		t.Fatal("independent cards need a free row between their backgrounds")
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

func TestNotificationDetailsMatchWhatTheCardCanDisplay(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.Chinese), PlainButtons: true}
	for _, shape := range []struct {
		width   int
		compact bool
	}{{54, false}, {36, true}} {
		for _, tt := range []struct {
			message, extra   string
			details, clipped bool
		}{
			{"已选用订阅「工作」", "", false, false},
			{"已选用订阅「工作」", " 已选用订阅「工作」 ", false, false},
			{"已选用订阅「工作」", "详细说明", true, false},
			{strings.Repeat("长订阅名", 20), "", true, true},
			{"一行\n二行", "", true, true},
		} {
			item := notificationItem{ID: 5, Page: "profiles", Context: "使用配置", Message: tt.message, Details: tt.extra, Kind: "done"}
			card := notificationCard(item, o, shape.width, shape.compact)
			assertCanvas(t, card.Content, card.Bounds.W, card.Bounds.H)
			assertRegions(t, card.Targets, card.Bounds.W, card.Bounds.H)
			plain := ansi.Strip(card.Content)
			if hasAction(card.Targets, "notification.details.5") != tt.details || (len(card.IDs) == 0) != tt.clipped {
				t.Fatalf("%v/%q: unexpected controls or visibility: %+v", shape, tt.message, card)
			}
			if strings.Contains(plain, "…") != tt.clipped || strings.Contains(plain, item.Context) || strings.Contains(plain, o.Locale.T("notification.done")) {
				t.Fatalf("%v/%q: unexpected title or clipping: %s", shape, tt.message, plain)
			}
			for _, target := range card.Targets {
				row := strings.Split(plain, "\n")[target.Y]
				cells := strings.TrimSpace(cellSlice(row, target.X, target.X+target.W))
				want := "×"
				if target.ID == "notification.details.5" {
					want = "详情"
				}
				if cells != want || target.ID == "notification.close.5" && !strings.Contains(row, string([]rune(tt.message)[0])) {
					t.Fatalf("%v/%q: control %q occupied %q on row %q", shape, tt.message, target.ID, cells, row)
				}
			}
		}
	}
}

func TestOversizedNotificationKeepsDetailAndCloseTargetsWhileTruncated(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Width: 60, Height: 20, Page: "profiles", Body: strings.Repeat("body\n", 50)}
	g := dashboardGeometry(o)
	o.BodyTargets = []hitRegion{{ID: "profiles.url", W: g.width, H: 3}}
	o.Notifications = []notificationItem{{ID: 8, Kind: "error", Page: "profiles", Message: strings.Repeat("Complete user explanation. ", 40), Details: "full technical details"}}
	layers := notificationLayout(o)
	if len(layers) != 1 || len(layers[0].IDs) != 0 || !hasAction(layers[0].Targets, "notification.details.8") || !hasAction(layers[0].Targets, "notification.close.8") || !strings.Contains(ansi.Strip(layers[0].Content), "…") {
		t.Fatal("truncated notification lost its ellipsis, details or close action", layers)
	}
}

func TestNotificationHeaderRetainsTitleAndShowsShortCompleteMessage(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {100, 24}, {80, 24}, {40, 24}, {60, 20}, {120, 18}} {
		for _, language := range []string{i18n.English, i18n.Chinese} {
			o := dashboardOptions{Locale: i18n.New(language), Width: size[0], Height: size[1], Page: "profiles", Body: strings.Repeat("body\n", 50)}
			g := dashboardGeometry(o)
			o.BodyTargets = []hitRegion{{ID: "profiles.url", W: g.width, H: 3}}
			o.Notifications = []notificationItem{{ID: 1, Page: "profiles", Context: "switch profile", Kind: "done", Message: "Saved"}}
			layers := notificationLayout(o)
			if len(layers) != 1 || len(layers[0].IDs) != 1 || len(layers[0].Targets) != 1 || layers[0].Targets[0].ID != "notification.close.1" {
				t.Fatalf("%v/%s: a complete success should show only the close control: %+v", size, language, layers)
			}
			if o.Width >= 100 && o.Height >= 24 && layers[0].Bounds.H != 3 {
				t.Fatalf("%v: wide success lost its padding above the input", size)
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
			if !strings.Contains(plain, o.Notifications[0].Message) || strings.Contains(plain, o.Notifications[0].Context) || strings.Contains(plain, notificationLabel(o.Locale, "done", "")) {
				t.Fatalf("%v/%s: explanation shortened: %s", size, language, plain)
			}
		}
	}
}

func TestFoldedWorkingNotificationKeepsItsMessageAndListAction(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Width: 120, Height: 18, Page: "connections", Busy: true,
		Notifications: []notificationItem{{ID: 1, Kind: "working", Message: "Refreshing subscription"}}}
	o.BodyTargets = []hitRegion{{ID: "conn.search", W: dashboardGeometry(o).width, H: 1}}
	layers := notificationLayout(o)
	if len(layers) != 1 || !hasAction(layers[0].Targets, "notification.expand") || !strings.Contains(ansi.Strip(layers[0].Content), "Refreshing subscription") {
		t.Fatalf("folded task lost its progress or the entry to its controls: %+v", layers)
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

func TestSolidNotificationBesideFoldedSidebarTitle(t *testing.T) {
	o := dashboardOptions{Locale: i18n.New(i18n.English), Width: 100, Height: 24, Page: "connections"}
	base, _, _ := renderDashboard(o)
	titleY := dashboardGeometry(o).headerY
	title := strings.TrimRight(ansi.Strip(strings.Split(base, "\n")[titleY]), " ")
	o.Notifications = []notificationItem{{ID: 1, Kind: "error", Message: "Connection failed."}}
	layers := notificationLayout(o)
	if len(layers) != 1 || layers[0].Bounds.H != 3 || strings.ContainsAny(ansi.Strip(layers[0].Content), "╭╮╰╯│─") {
		t.Fatal("a wide page with free body space should use a padded solid card")
	}
	screen, _, _ := renderDashboard(o)
	if !strings.HasPrefix(ansi.Strip(strings.Split(screen, "\n")[titleY]), title) {
		t.Fatal("wide card covered the menu, logo, page title or core status")
	}
}

func TestNotificationPanelKeepsFullMessagesAndScrollableTechnicalDetails(t *testing.T) {
	items := []notificationItem{
		{ID: 1, Page: "profiles", Kind: "error", Message: "First explanation.", Details: strings.Repeat("Technical paragraph\n", 80) + "last-detail-marker"},
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
	assertCloseInCorner := func(p *notificationPanel, box string) {
		t.Helper()
		plain := ansi.Strip(box)
		rows := strings.Split(plain, "\n")
		before, after, found := strings.Cut(rows[1], "×")
		if !found || strings.Count(plain, "×") != 1 || strings.TrimSpace(after) != "" || ansi.StringWidth(after) > 3 {
			t.Fatalf("expected a single close icon at the right end of the first content row:\n%s", plain)
		}
		click := tea.MouseClickMsg{X: p.bounds.X + ansi.StringWidth(before), Y: p.bounds.Y + 1, Button: tea.MouseLeft}
		if action := p.mouse(click); action != "notification.panel.dismiss.1" {
			t.Fatalf("clicking the visible close icon returned %q", action)
		}
		click.Y = p.bounds.Y + p.bounds.H - 2
		if action := p.mouse(click); action == "notification.panel.dismiss.1" {
			t.Fatal("clicking the former close position in the footer still dismisses the notification")
		}
	}
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {40, 24}, {32, 12}, {20, 7}} {
		width, height := size[0], size[1]
		short := notificationItem{ID: 1, Kind: "error", Message: "启动超时"}
		var p notificationPanel
		p.show(1)
		box, _ := p.render([]notificationItem{short}, width, height, locale)
		shortBounds := p.bounds
		assertCanvas(t, box, shortBounds.W, shortBounds.H)
		assertCloseInCorner(&p, box)
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
		assertCloseInCorner(&p, box)
		visible := strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(box), "×", "")), " ")
		if !strings.Contains(visible, "Final cause") {
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
		{ID: 2, Kind: "done", Message: "已选用订阅「工作」"},
	}
	var p notificationPanel
	p.show(0)
	locale := i18n.New(i18n.Chinese)
	box, hits := p.render(items, 40, 24, locale)
	if !strings.Contains(ansi.Strip(box), items[1].Message) || !hasAction(hits, "notification.panel.details.1") || hasAction(hits, "notification.panel.details.2") || !hasAction(hits, "notification.panel.dismiss.2") {
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

func TestExpandedStackSeparatesBorderlessCardsAndTheirOwnControls(t *testing.T) {
	items := []notificationItem{
		{ID: 9, Kind: "done", Page: "profiles", Context: "Apply", Message: "Selected Work"},
		{ID: 17, Kind: "error", Message: "Retry", Details: "Could not download the subscription"},
	}
	locale := i18n.New(i18n.English)
	for _, size := range [][2]int{{160, 45}, {60, 20}, {40, 24}} {
		for _, plain := range []bool{false, true} {
			var p notificationPanel
			p.show(0)
			stack, targets := p.render(items, size[0], size[1], locale, plain)
			assertCanvas(t, stack, p.bounds.W, p.bounds.H)
			rows := strings.Split(ansi.Strip(stack), "\n")
			if strings.ContainsAny(strings.Join(rows, ""), "╭╮╰╯│─") {
				t.Fatalf("%v: the stack still paints a frame", size)
			}
			previousRow := -1
			for _, item := range items {
				marker := notificationPaint(item.Kind).marker
				message := marker + " " + notificationBody(item, locale, "")
				row := slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, message) })
				if row < 1 || !strings.Contains(rows[row], "×") {
					t.Fatalf("%v: card %d lost its status, result or close action:\n%s", size, item.ID, stack)
				}
				if previousRow >= 0 && (row-previousRow < 3 || strings.TrimSpace(rows[previousRow+2]) != "") {
					t.Fatalf("%v: adjacent notifications have no separation", size)
				}
				previousRow = row
				for _, action := range []string{"details", "dismiss"} {
					id := fmt.Sprintf("notification.panel.%s.%d", action, item.ID)
					inside := slices.ContainsFunc(targets, func(hit hitRegion) bool {
						return hit.ID == id && hit.Y == p.bounds.Y+row
					})
					want := action == "dismiss" || item.Details != ""
					if inside != want {
						t.Fatalf("%v: %s on the message row = %v, want %v", size, id, inside, want)
					}
				}
			}
		}
	}
}
