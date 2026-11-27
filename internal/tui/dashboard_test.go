package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func assertCanvas(t *testing.T, value string, width, height int) {
	t.Helper()
	lines := strings.Split(value, "\n")
	if len(lines) > height {
		t.Fatalf("%dx%d: got %d lines", width, height, len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > width {
			t.Fatalf("%dx%d: line %d width %d: %q", width, height, i, w, ansi.Strip(line))
		}
	}
}

func renderDashboard(o dashboardOptions) (string, []hitRegion, int) {
	screen, targets, total, _ := drawDashboard(o)
	return screen, targets, total
}

func assertRegions(t *testing.T, regions []hitRegion, width, height int) {
	t.Helper()
	for _, r := range regions {
		if r.X < 0 || r.Y < 0 || r.W <= 0 || r.H <= 0 || r.X+r.W > width || r.Y+r.H > height {
			t.Fatalf("outside %dx%d: %+v", width, height, r)
		}
	}
}
func hasAction(regions []hitRegion, id string) bool {
	for _, r := range regions {
		if r.ID == id {
			return true
		}
	}
	return false
}

func TestDashboardResponsiveCanvas(t *testing.T) {
	sizes := [][2]int{{160, 45}, {120, 32}, {100, 28}, {80, 24}, {60, 20}, {120, 18}, {20, 7}, {10, 4}, {1, 1}}
	long := strings.Repeat("中文路径长字符串/", 20)
	snapshot := app.Snapshot{Installed: true, ConfigReady: true, Version: long, Controller: "http://127.0.0.1:9090/" + long, Applied: long, Profiles: []string{"default", long}, Groups: []mihomo.Group{{Name: long, Type: "Selector", Now: long}}, Service: service.Status{Active: true, State: "active"}}
	for _, size := range sizes {
		for _, page := range []string{"overview", "proxies", "profiles", "settings"} {
			for _, scroll := range []int{0, 3, 10000} {
				t.Run(fmt.Sprintf("%dx%d/%s/%d", size[0], size[1], page, scroll), func(t *testing.T) {
					options := dashboardOptions{Width: size[0], Height: size[1], Page: page, Snapshot: snapshot, Scroll: scroll, Body: wrapText(long, max(1, size[0]-4)), BodyTargets: []hitRegion{{ID: "test", X: 0, Y: 0, W: 4, H: 1}}}
					screen, regions, _ := renderDashboard(options)
					assertCanvas(t, screen, size[0], size[1])
					assertRegions(t, regions, size[0], size[1])
				})
			}
		}
	}
}

func TestOverviewUninitializedIsBrowsable(t *testing.T) {
	options := dashboardOptions{Width: 160, Height: 45, Page: "overview"}
	value, targets, total := renderDashboard(options)
	body, bodyTargets := overviewContent(options, dashboardBodyWidth(options))
	plain := ansi.Strip(value + "\n" + body)
	allTargets := append(append([]hitRegion(nil), targets...), bodyTargets...)
	for _, part := range []string{"Get Started", "Install the Mihomo Core", "Install Mihomo", "Set up later", "− Not installed"} {
		if !strings.Contains(plain, part) {
			t.Errorf("missing %q", part)
		}
	}
	for _, id := range []string{"install", "guide.later", "nav.overview", "nav.proxies", "nav.profiles", "nav.connections", "nav.rules", "nav.logs", "nav.settings"} {
		if !hasAction(allTargets, id) {
			t.Errorf("missing action %s", id)
		}
	}
	for _, part := range []string{"sing-box", options.Locale.T("dashboard.overview.traffic"), options.Locale.T("dashboard.overview.route"), "1 / 3"} {
		if strings.Contains(plain, part) {
			t.Errorf("unexpected placeholder navigation %q", part)
		}
	}
	if total <= 0 {
		t.Fatal("body height missing")
	}
	// A dismissed guide leaves status cards and permanent navigation intact.
	options.GuideDismissed = true
	value, targets, _ = renderDashboard(options)
	if strings.Contains(ansi.Strip(value), "Get Started") || hasAction(targets, "dismiss") {
		t.Fatal("dismissed guide still visible")
	}
	if !hasAction(targets, "nav.settings") {
		t.Fatal("dismiss removed permanent navigation")
	}
	if !hasAction(targets, "card.profiles") || !hasAction(targets, "core.install") || hasAction(targets, "card.install") {
		t.Fatal("dismiss removed the profile or core card action")
	}
}

func TestOverviewContentFillsAreaBesideNavigationAndReservesUsableRows(t *testing.T) {
	for _, width := range []int{115, 120, 140, 160, 197, 240, 320} {
		g := dashboardGeometry(dashboardOptions{Width: width, Height: 45})
		left, right := g.x-sidebarWidth, width-g.x-g.width
		if left-right > 1 || right-left > 1 || g.width != width-sidebarWidth-5 || g.y != 3 || g.height != 41 {
			t.Fatalf("Overview does not fill the area beside the navigation: %+v", g)
		}
	}
	g := dashboardGeometry(dashboardOptions{Width: 80, Height: 24})
	if g.sidebar || g.x != 2 || g.height != 20 {
		t.Fatalf("compact frame wasted content space: %+v", g)
	}
	g = dashboardGeometry(dashboardOptions{Width: 240, Height: 45, Page: "proxies"})
	if g.x != sidebarWidth+2 {
		t.Fatalf("Overview sizing moved another page: %+v", g)
	}
}

func TestDashboardContentReachesRowBeforeFooter(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {100, 20}, {80, 24}, {60, 20}, {120, 18}, {30, 12}, {20, 7}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			o := dashboardOptions{Width: size[0], Height: size[1], Page: "proxies"}
			g := dashboardGeometry(o)
			var body []string
			for i := 0; i < g.height+3; i++ {
				label := fmt.Sprintf("Row%02d", i)
				body = append(body, label)
				o.BodyTargets = append(o.BodyTargets, hitRegion{ID: label, Y: i, W: 5, H: 1})
			}
			o.Body = strings.Join(body, "\n")
			for _, atEnd := range []bool{false, true} {
				last, marker := g.height-1, "↓"
				if atEnd {
					o.Scroll, last, marker = 10000, len(body)-1, "↑"
				}
				screen, targets, _ := renderDashboard(o)
				rows := strings.Split(screen, "\n")
				label := fmt.Sprintf("Row%02d", last)
				if !strings.Contains(ansi.Strip(rows[o.Height-2]), label) {
					t.Fatalf("last content row was reserved instead of showing %s", label)
				}
				if !strings.Contains(ansi.Strip(rows[o.Height-2]), marker) || !strings.Contains(ansi.Strip(rows[o.Height-1]), o.Locale.T("dashboard.footer.hideHints")) {
					t.Fatal("scroll marker or footer moved away from its final row")
				}
				found := false
				for _, hit := range targets {
					if strings.HasPrefix(hit.ID, "Row") && hit.Y+hit.H > o.Height-1 {
						t.Fatalf("content target overlaps footer: %+v", hit)
					}
					found = found || (hit.ID == label && hit.Y == o.Height-2)
				}
				if !found {
					t.Fatal("last content row has no matching mouse target")
				}
				assertCanvas(t, screen, o.Width, o.Height)
				assertRegions(t, targets, o.Width, o.Height)
			}
		})
	}
}

func TestSidebarExtendsToContentBottomAndShieldsMouseTargets(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 25}, {100, 20}, {80, 24}, {60, 20}, {120, 18}, {30, 12}, {20, 7}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			o := dashboardOptions{Width: size[0], Height: size[1], Page: "proxies", SidebarOpen: true, Focus: "nav.settings"}
			g := dashboardGeometry(o)
			rows := make([]string, o.Height)
			for i := range rows {
				rows[i] = strings.Repeat(".", o.Width)
			}
			drawSidebar(rows, o, !g.sidebar)
			if strings.Contains(ansi.Strip(ansi.Cut(rows[o.Height-2], 0, min(sidebarWidth, o.Width))), ".") {
				t.Fatal("sidebar left the last content row uncovered")
			}
			if rows[o.Height-1] != strings.Repeat(".", o.Width) {
				t.Fatal("sidebar painted over the footer")
			}
			if g.sidebar {
				return
			}
			o.Body = strings.Repeat("underlay\n", g.height)
			o.BodyTargets = []hitRegion{{ID: "underlay", Y: g.height - 1, W: 8, H: 1}}
			o.SidebarOpen = false
			_, targets, _ := renderDashboard(o)
			if !hasAction(targets, "underlay") {
				t.Fatal("fixture is missing the last content row target")
			}
			o.SidebarOpen = true
			screen, targets, _ := renderDashboard(o)
			if hasAction(targets, "underlay") {
				t.Fatal("compact navigation allows clicks through its final row")
			}
			if !strings.Contains(ansi.Strip(strings.Split(screen, "\n")[o.Height-1]), o.Locale.T("dashboard.footer.hideHints")) {
				t.Fatal("compact navigation obscured the footer")
			}
		})
	}
}

func TestDashboardSidebarHasSevenCenteredContiguousItems(t *testing.T) {
	o := dashboardOptions{Width: 160, Height: 45, Page: "profiles", Focus: "nav.connections"}
	screen, targets, _ := renderDashboard(o)
	rows := strings.Split(screen, "\n")
	previousY, previousHeight := -1, 0
	for i, page := range navPages() {
		var hit hitRegion
		for _, target := range targets {
			if target.ID == "nav."+page {
				hit = target
				break
			}
		}
		if hit.W == 0 {
			t.Fatalf("missing navigation for %s", page)
		}
		if hit.H != 3 {
			t.Fatalf("menu height changed for %s: %+v", page, hit)
		}
		value := ansi.Strip(ansi.Cut(rows[hit.Y+hit.H/2], hit.X, hit.X+hit.W))
		if !strings.Contains(value, pageTitle(page)) || strings.ContainsAny(value, "1234567") {
			t.Fatalf("wrong page label or unexpected numeric hint: %q", value)
		}
		nameX := strings.Index(value, pageTitle(page))
		left := ansi.StringWidth(value[:nameX])
		right := hit.W - left - ansi.StringWidth(pageTitle(page))
		if left-right > 1 || right-left > 1 {
			t.Fatalf("page name is not centered independently of its markers: %q", value)
		}
		if i > 0 && (hit.Y != previousY+previousHeight || hit.H != previousHeight) {
			t.Fatal("sidebar items are not contiguous and equal in height")
		}
		previousY, previousHeight = hit.Y, hit.H
	}
	plain := ansi.Strip(screen)
	if strings.Contains(plain, "1 Overview") || strings.Contains(plain, "LOCAL /") || strings.Contains(plain, "\n  Core") {
		t.Fatal("sidebar names or global frame still contain old chrome")
	}
}

func sidebarText(screen string) string {
	rows := strings.Split(screen, "\n")
	for i := range rows {
		rows[i] = ansi.Strip(ansi.Cut(rows[i], 0, sidebarWidth))
	}
	return strings.Join(rows[:len(rows)-1], "\n")
}

func TestDashboardFooterUsesContextAndDoesNotKeepRefreshNotice(t *testing.T) {
	o := dashboardOptions{Width: 120, Height: 32, Page: "overview", Message: "State refreshed", MessageTransient: true, Help: "↑↓ Navigate · Enter Open · Tab Content"}
	screen, _, _ := renderDashboard(o)
	plain := ansi.Strip(screen)
	if strings.Contains(plain, "State refreshed") || !strings.Contains(plain, "↑↓ Navigate") {
		t.Fatal("footer retained an idle refresh message or lost context")
	}
	o.Failure = "permission denied"
	o.Notifications = []notificationItem{{ID: 1, Kind: "error", Message: o.Failure}}
	screen, _, _ = renderDashboard(o)
	if !strings.Contains(ansi.Strip(screen), "Error") || !strings.Contains(ansi.Strip(screen), "permission denied") {
		t.Fatal("necessary failure feedback was hidden")
	}
}

func TestOverviewRowsSizeIndependentlyAndEmptyMetricsStayClear(t *testing.T) {
	o := dashboardOptions{Width: 160, Height: 45, Page: "overview", GuideDismissed: true}
	body, _, _ := renderDashboard(o)
	plain := ansi.Strip(body)
	if strings.Contains(plain, o.Locale.T("dashboard.field.connections")) || strings.Contains(plain, "You can explore") {
		t.Fatal("empty cards retained unavailable metric rows or setup filler")
	}
	// Traffic diagnostics grow the last card independently of the pairs above it.
	_, _, shortHeight := renderDashboard(o)
	o.Runtime = app.RuntimeSnapshot{State: "unavailable", Error: strings.Repeat("runtime diagnostic ", 50)}
	_, _, tallHeight := renderDashboard(o)
	if tallHeight <= shortHeight {
		t.Fatal("long diagnostic was not retained in the scrollable card")
	}
	short := overviewLayout(dashboardOptions{Height: 45, GuideDismissed: true}, 112)
	tall := overviewLayout(o, 112)
	if !slices.Equal(short.cards[:len(short.cards)-1], tall.cards[:len(tall.cards)-1]) {
		t.Fatal("traffic diagnostic changed an earlier card")
	}
}

func TestOverviewLayoutPreservesDetailsAndActionTargets(t *testing.T) {
	drift := strings.Repeat("The running service differs from the saved configuration. ", 8)
	snapshot := app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true, Applied: "work-profile",
		Version: "v1.20.0", RunningVersion: "v1.19.33",
		Service: service.Status{Active: true, State: "active"},
		Groups:  []mihomo.Group{{Name: "PROXY", Type: "Selector", Now: "DIRECT"}},
		Desired: app.DesiredState{Known: true, Running: false}, Drift: drift,
	}
	for _, width := range []int{112, 87, 72} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			o := dashboardOptions{Height: 45, GuideDismissed: true, Snapshot: snapshot, Runtime: liveRuntime(), RuntimeReceived: true}
			body, hits := overviewContent(o, width)
			rows := strings.Split(body, "\n")
			words := map[string]int{}
			for _, word := range strings.Fields(strings.ReplaceAll(ansi.Strip(body), "│", " ")) {
				words[word]++
			}
			for _, value := range []string{o.Locale.T("dashboard.field.connections"), "↑ Upload", "↓ Download", "1.0 MiB", "2.0 MiB", "2.0 KiB/s", "4.0 KiB/s", "16.0 MiB", "Unsupported", "Installed version: v1.20.0", "Running version: v1.19.33", "Restart to use the installed version", o.Locale.T("dashboard.core.stopRequested")} {
				for _, word := range strings.Fields(value) {
					if words[word] == 0 {
						t.Errorf("overview lost word %q from detail %q", word, value)
					}
					words[word]--
				}
			}
			assertCanvas(t, body, width, len(rows))
			assertRegions(t, hits, width, len(rows))
			for id, label := range map[string]string{"card.profiles": o.Locale.T("dashboard.overview.profilesAction"), "network.primaryConfig": o.Locale.T("dashboard.network.configure"), "network.tunConfig": o.Locale.T("dashboard.network.configure"), "card.proxies": o.Locale.T("dashboard.overview.proxiesAction")} {
				found := false
				for _, hit := range hits {
					if hit.ID == id {
						found = true
						visible := ansi.Strip(ansi.Cut(rows[hit.Y], hit.X, hit.X+hit.W))
						if strings.TrimSpace(strings.TrimPrefix(visible, " ›")) != label || hit.W != ansi.StringWidth(label)+4 {
							t.Errorf("%s target misses its rendered button: %q", id, visible)
						}
					}
				}
				if !found {
					t.Errorf("missing overview action %s", id)
				}
			}
			for i, hit := range hits {
				for _, other := range hits[i+1:] {
					if regionsOverlap(hit, other) {
						t.Errorf("overview action targets overlap: %+v and %+v", hit, other)
					}
				}
			}
		})
	}
	t.Run("uninstalled", func(t *testing.T) {
		o := dashboardOptions{Snapshot: app.Snapshot{ServiceError: "Service management is unsupported on macOS"}}
		card := ansi.Strip(strings.Join(overviewModeCard(o, 80).lines, "\n"))
		if !strings.Contains(card, o.Locale.T("dashboard.control.modeAfterInstall")) || strings.Contains(card, "Service unavailable") {
			t.Fatalf("uninstalled core was presented as a service failure: %s", card)
		}
	})
}

func TestInstalledCoreDoesNotRequireGuideOrSubscription(t *testing.T) {
	o := dashboardOptions{Width: 160, Height: 45, Page: "overview", Snapshot: app.Snapshot{Installed: true, Service: service.Status{State: "inactive"}}}
	value, targets, _ := renderDashboard(o)
	plain := ansi.Strip(value)
	if strings.Contains(plain, "Get Started") || !strings.Contains(plain, "No profile selected") || strings.Contains(plain, "Not applied") {
		t.Fatal("an installed core with no selection should show the empty subscription card")
	}
	o.Snapshot.ConfigReady = true
	value, targets, _ = renderDashboard(o)
	if !strings.Contains(ansi.Strip(value), "No profile selected") || !hasAction(targets, "core.toggle") {
		t.Fatal("built-in configuration must be ready for an explicit start")
	}
	o.Snapshot.HasRun = true
	value, targets, _ = renderDashboard(o)
	if strings.Contains(ansi.Strip(value), "Get Started") || hasAction(targets, "install") {
		t.Fatal("normal stop reopened installation guide")
	}
	o.Snapshot.HasRun = false
	o.Snapshot.Service.Active = true
	value, _, _ = renderDashboard(o)
	if strings.Contains(ansi.Strip(value), "Get Started") {
		t.Fatal("running core still shown in startup guide")
	}
}

func TestDashboardMouseCoordinatesFollowScrolling(t *testing.T) {
	o := dashboardOptions{Width: 60, Height: 20, Page: "overview", GuideSkipped: true}
	value, regions, total := renderDashboard(o)
	if total <= o.Height-7 {
		t.Fatal("short viewport should require scrolling")
	}
	lines := strings.Split(value, "\n")
	labels := map[string]string{"core.install": "Install core", "card.profiles": o.Locale.T("dashboard.overview.profilesAction")}
	for _, r := range regions {
		if label, ok := labels[r.ID]; ok {
			cells := ansi.Strip(ansi.Cut(lines[r.Y], r.X, r.X+r.W))
			if strings.TrimSpace(strings.TrimPrefix(cells, " ›")) != label || r.W != ansi.StringWidth(label)+4 {
				t.Fatalf("target does not cover actual button: %+v %q", r, cells)
			}
		}
	}
	o.Scroll = 10000
	value, regions, _ = renderDashboard(o)
	g := dashboardGeometry(o)
	scroll := max(0, total-g.height)
	body := overviewLayout(o, g.width)
	scrolledAway := 0
	for _, target := range body.targets {
		visible, ok := clipRegion(target, 0, scroll, g.width, g.height)
		if hasAction(regions, target.ID) != ok {
			t.Fatalf("scroll %d: action %s visibility disagrees with card geometry", scroll, target.ID)
		}
		if !ok {
			scrolledAway++
			continue
		}
		visible.X += g.x
		visible.Y += g.y - scroll
		for _, region := range regions {
			if region.ID == target.ID && region != visible {
				t.Fatalf("scrolled action target misses its visible button: got %+v, want %+v", region, visible)
			}
		}
	}
	if scrolledAway == 0 || hasAction(regions, "core.install") {
		t.Fatal("fixture did not scroll away the initial core action")
	}
	if !strings.Contains(ansi.Strip(value), o.Locale.T("dashboard.overview.traffic")) {
		t.Fatal("traffic card is not reachable at the bottom of the overview")
	}
	if !hasAction(regions, "menu") {
		t.Fatal("narrow navigation missing")
	}
	o.SidebarOpen = true
	value, regions, _ = renderDashboard(o)
	for _, id := range []string{"nav.overview", "nav.proxies", "nav.profiles"} {
		if !hasAction(regions, id) {
			t.Errorf("menu action missing: %s", id)
		}
	}
	if !hasAction(regions, "nav.settings") {
		t.Fatal("compact menu failed to show all destinations")
	}
	o.Focus = "nav.settings"
	value, regions, _ = renderDashboard(o)
	if !hasAction(regions, "nav.settings") || !hasAction(regions, "nav.overview") {
		t.Fatal("compact menu did not reveal the focused item")
	}
	assertCanvas(t, value, o.Width, o.Height)
	assertRegions(t, regions, o.Width, o.Height)
}

func TestSettingsOnlyOffersAvailableActions(t *testing.T) {
	o := dashboardOptions{Width: 120, Height: 40, Page: "settings"}
	o.Header = (&Model{page: "settings"}).pageHeader()
	_, regions, _ := renderDashboard(o)
	for _, id := range []string{"install", "network"} {
		if !hasAction(regions, id) {
			t.Errorf("missing %s", id)
		}
	}
	for _, id := range []string{"runtime-settings", autostartToggleTarget, "restart"} {
		if hasAction(regions, id) {
			t.Errorf("uninstalled core offers %s", id)
		}
	}
	o.Snapshot.Installed = true
	_, regions, _ = renderDashboard(o)
	for _, id := range []string{"install", "runtime-settings", autostartToggleTarget, "restart"} {
		if !hasAction(regions, id) {
			t.Errorf("installed settings missing %s", id)
		}
	}
}

func TestBodyTargetTranslationAndClip(t *testing.T) {
	o := dashboardOptions{Width: 80, Height: 24, Page: "proxies", Body: strings.Repeat("row\n", 30), BodyTargets: []hitRegion{{ID: "node.first", X: 3, Y: 0, W: 8, H: 1}, {ID: "node.last", X: 3, Y: 29, W: 8, H: 1}}}
	_, regions, _ := renderDashboard(o)
	g := dashboardGeometry(o)
	for _, r := range regions {
		if r.ID == "node.first" && (r.X != g.x+3 || r.Y != g.y) {
			t.Fatal(r)
		}
	}
	if !hasAction(regions, "node.first") || hasAction(regions, "node.last") {
		t.Fatal("first viewport targets wrong")
	}
	o.Scroll = 14
	_, regions, _ = renderDashboard(o)
	if hasAction(regions, "node.first") || !hasAction(regions, "node.last") {
		t.Fatal("scrolled viewport targets wrong")
	}
}

func TestCellWrappingOverlayAndFocus(t *testing.T) {
	source := "https://example.invalid/" + strings.Repeat("很长的路径", 20)
	for _, w := range []int{1, 2, 3, 20, 40} {
		assertCanvas(t, wrapText(source, w), w, 1000)
		if ansi.StringWidth(cellCrop(source, w)) > w {
			t.Fatal("crop exceeds cells")
		}
	}
	base := strings.Repeat("中文abcdef\n", 8)
	box := "┌──────┐\n│选择  │\n└──────┘"
	for _, xy := range [][2]int{{2, 2}, {-1, -1}, {18, 7}} {
		assertCanvas(t, overlay(base, box, xy[0], xy[1], 20, 8), 20, 8)
	}
	o := dashboardOptions{Width: 120, Height: 32, Page: "overview"}
	selected, _, _ := renderDashboard(o)
	o.Focus = "nav.overview"
	focused, _, _ := renderDashboard(o)
	if selected == focused || !strings.Contains(ansi.Strip(focused), "› Overview") {
		t.Fatal("focus is indistinguishable from selected navigation")
	}
	o.Dim = true
	dimmed, _, _ := renderDashboard(o)
	if !strings.Contains(ansi.Strip(dimmed), "Get Started") || !strings.Contains(ansi.Strip(dimmed), "Overview") {
		t.Fatal("dimmed backdrop lost underlying page")
	}
}

func TestShortCompactNavigationKeepsFocusedLastItemVisible(t *testing.T) {
	for _, size := range [][2]int{{80, 12}, {60, 10}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.width, m.height = size[0], size[1]
			m.page, m.sidebarOpen, m.focus = "settings", true, "nav.settings"
			m.layout()
			check := func(id, label string) {
				t.Helper()
				hit := findTarget(t, m, id)
				line := strings.Split(m.screen, "\n")[hit.Y+hit.H/2]
				visible := ansi.Strip(ansi.Cut(line, hit.X, hit.X+hit.W))
				if !strings.Contains(visible, "› "+label) {
					t.Fatalf("focused menu item is hidden: %q", visible)
				}
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			}
			check("nav.settings", "Settings")
			if m.sidebar.viewport.YOffset() > 0 && hasAction(m.targets, "nav.overview") {
				t.Fatal("short compact menu retained an offscreen item")
			}
			before := m.scroll
			m, _ = update(m, special(tea.KeyUp))
			if m.page != "logs" || m.focus != "nav.logs" || m.scroll != before {
				t.Fatal("arrow did not move back inside compact navigation")
			}
			check("nav.logs", "Logs")
		})
	}
}
