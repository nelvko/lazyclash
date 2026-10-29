package tui

import (
	"fmt"
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
	for _, part := range []string{"Get started", "Install your first proxy core", "Current profile", "Selected proxy group", "Traffic statistics", "Kernel information", "System information", "Routing mode", "TUN", "System proxy", "Core not installed", "Mihomo · Not installed"} {
		if !strings.Contains(plain, part) {
			t.Errorf("missing %q", part)
		}
	}
	for _, id := range []string{"install", "import", "dismiss", "nav.overview", "nav.proxies", "nav.profiles", "nav.connections", "nav.rules", "nav.logs", "nav.settings"} {
		if !hasAction(allTargets, id) {
			t.Errorf("missing action %s", id)
		}
	}
	for _, part := range []string{"sing-box"} {
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
	if strings.Contains(ansi.Strip(value), "Get started") || hasAction(targets, "dismiss") {
		t.Fatal("dismissed guide still visible")
	}
	if !hasAction(targets, "nav.settings") {
		t.Fatal("dismiss removed permanent navigation")
	}
	if !hasAction(targets, "card.import") || !hasAction(targets, "card.install") {
		t.Fatal("dismiss removed the profile or core card action")
	}
}

func TestOverviewContentFillsAreaBesideNavigationAndReservesUsableRows(t *testing.T) {
	for _, width := range []int{160, 197, 240, 320} {
		g := dashboardGeometry(dashboardOptions{Width: width, Height: 45})
		left, right := g.x-sidebarWidth, width-g.x-g.width
		if left-right > 1 || right-left > 1 || g.width != width-sidebarWidth-5 || g.y != 5 || g.height != 38 {
			t.Fatalf("Overview does not fill the area beside the navigation: %+v", g)
		}
	}
	g := dashboardGeometry(dashboardOptions{Width: 80, Height: 24})
	if g.sidebar || g.x != 2 || g.height != 17 {
		t.Fatalf("compact frame wasted content space: %+v", g)
	}
	g = dashboardGeometry(dashboardOptions{Width: 240, Height: 45, Page: "proxies"})
	if g.x != 30 {
		t.Fatalf("Overview sizing moved another page: %+v", g)
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
		value := ansi.Strip(ansi.Cut(rows[hit.Y+hit.H/2], hit.X, hit.X+hit.W))
		if !strings.Contains(value, pageTitle(page)) || strings.ContainsAny(value, "1234567") {
			t.Fatalf("wrong page label or unexpected numeric hint: %q", value)
		}
		nameX := strings.Index(value, pageTitle(page))
		left := ansi.StringWidth(value[:nameX])
		right := hit.W - left - ansi.StringWidth(pageTitle(page))
		if left-right > 1 || right-left > 1 {
			t.Fatalf("page name is not centered: %q", value)
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
	return strings.Join(rows[:len(rows)-2], "\n")
}

func TestSidebarMetadataReflectsSavedConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot app.Snapshot
		fields   map[string]string
		absent   []string
	}{
		{
			name: "saved configuration differs from running core",
			snapshot: app.Snapshot{
				Installed: true, ConfigReady: true, Applied: "work-profile",
				Version: "v1.20.0", RunningVersion: "v1.19.33",
				Mode:    app.ModeStatus{Desired: "rule", Actual: "global", Running: true},
				Service: service.Status{Active: true, State: "active"},
			},
			fields: map[string]string{"Profile": "work-profile", "Saved mode": "rule", "Installed": "v1.20.0"},
			absent: []string{"v1.19.33", "global"},
		},
		{
			name:     "default profile and unavailable metadata",
			snapshot: app.Snapshot{Installed: true, ConfigReady: true},
			fields:   map[string]string{"Profile": "default", "Saved mode": "Unknown", "Installed": "Unknown"},
		},
		{
			name:     "configuration unavailable",
			snapshot: app.Snapshot{Installed: true},
			fields:   map[string]string{"Profile": "Not configured"},
			absent:   []string{"default"},
		},
		{
			name: "uninstalled core has no fabricated configuration",
			snapshot: app.Snapshot{
				Applied: "stale-profile", Version: "stale-version",
				Mode: app.ModeStatus{Desired: "stale-mode"},
			},
			fields: map[string]string{"Mihomo": "not installed"},
			absent: []string{"stale-profile", "stale-version", "stale-mode", "Saved mode", "default"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			screen, _, _ := renderDashboard(dashboardOptions{Width: 160, Height: 45, Page: "overview", Snapshot: tc.snapshot})
			metadata := sidebarText(screen)
			for label, value := range tc.fields {
				found := false
				for _, row := range strings.Split(metadata, "\n") {
					found = found || strings.Contains(row, label) && strings.Contains(strings.ToLower(row), strings.ToLower(value))
				}
				if !found {
					t.Errorf("missing sidebar field %s = %s:\n%s", label, value, metadata)
				}
			}
			for _, value := range tc.absent {
				if strings.Contains(strings.ToLower(metadata), strings.ToLower(value)) {
					t.Errorf("sidebar exposes unexpected value %q:\n%s", value, metadata)
				}
			}
		})
	}
}

func TestSidebarMetadataPreservesNavigationAndStaysOutOfPopup(t *testing.T) {
	snapshot := app.Snapshot{Installed: true, ConfigReady: true, Applied: "work-profile", Version: "v1.20.0", Mode: app.ModeStatus{Desired: "rule"}}
	for _, size := range [][2]int{{160, 45}, {120, 28}, {80, 45}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			o := dashboardOptions{Width: size[0], Height: size[1], Page: "overview", SidebarOpen: true, Snapshot: snapshot}
			screen, targets, _ := renderDashboard(o)
			metadata := sidebarText(screen)
			wantMetadata := size == [2]int{160, 45}
			metadataY := len(strings.Split(metadata, "\n"))
			for y, row := range strings.Split(metadata, "\n") {
				if strings.Contains(row, "work-profile") {
					metadataY = y
					break
				}
			}
			for _, label := range []string{"work-profile", "Saved mode", "Installed"} {
				if strings.Contains(metadata, label) != wantMetadata {
					t.Fatalf("sidebar metadata visibility is wrong for %q:\n%s", label, metadata)
				}
			}
			for _, page := range navPages() {
				var hit hitRegion
				for _, target := range targets {
					if target.ID == "nav."+page {
						hit = target
						break
					}
				}
				if hit.H != 3 {
					t.Fatalf("sidebar metadata hid or compressed navigation for %s: %+v", page, hit)
				}
				if wantMetadata && hit.Y+hit.H > metadataY {
					t.Fatalf("sidebar metadata overlaps navigation for %s", page)
				}
				row := strings.Split(screen, "\n")[hit.Y+hit.H/2]
				if !strings.Contains(ansi.Strip(ansi.Cut(row, hit.X, hit.X+hit.W)), pageTitle(page)) {
					t.Fatalf("sidebar metadata covered navigation for %s", page)
				}
			}
		})
	}
}

func TestDashboardFooterUsesContextAndDoesNotKeepRefreshNotice(t *testing.T) {
	o := dashboardOptions{Width: 120, Height: 32, Page: "overview", Message: "State refreshed", Help: "↑↓ Navigate · Enter Open · Tab Content"}
	screen, _, _ := renderDashboard(o)
	plain := ansi.Strip(screen)
	if strings.Contains(plain, "State refreshed") || !strings.Contains(plain, "↑↓ Navigate") {
		t.Fatal("footer retained an idle refresh message or lost context")
	}
	o.Failure = "permission denied"
	screen, _, _ = renderDashboard(o)
	if !strings.Contains(ansi.Strip(screen), "Error: permission denied") {
		t.Fatal("necessary failure feedback was hidden")
	}
}

func TestOverviewRowsSizeIndependentlyAndEmptyMetricsStayClear(t *testing.T) {
	o := dashboardOptions{Width: 160, Height: 45, Page: "overview", GuideDismissed: true}
	body, _, _ := renderDashboard(o)
	plain := ansi.Strip(body)
	if strings.Contains(plain, "Connections         --") || strings.Contains(plain, "You can explore") {
		t.Fatal("empty cards retained unavailable metric rows or setup filler")
	}
	// The first row has no dependency on the second row's potentially long
	// runtime details; a lengthy error must not stretch both pairs of cards.
	_, _, shortHeight := renderDashboard(o)
	o.Runtime = app.RuntimeSnapshot{State: "unavailable", Error: strings.Repeat("runtime diagnostic ", 50)}
	_, _, tallHeight := renderDashboard(o)
	if tallHeight <= shortHeight {
		t.Fatal("long diagnostic was not retained in the scrollable card")
	}
	shortBody, _ := overviewContent(dashboardOptions{Height: 45, GuideDismissed: true}, 112)
	tallBody, _ := overviewContent(o, 112)
	firstTraffic := func(value string) int {
		for i, row := range strings.Split(ansi.Strip(value), "\n") {
			if strings.Contains(row, "Traffic statistics") {
				return i
			}
		}
		return -1
	}
	if firstTraffic(shortBody) != firstTraffic(tallBody) {
		t.Fatal("a second-row diagnostic stretched the first card row")
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
			for _, value := range []string{"Connections", "Upload / download", "Since core start", "Core memory", "1.0 MiB", "2.0 MiB", "2.0 KiB/s", "4.0 KiB/s", "16.0 MiB", "Not supported by the user service.", "Installed version:", "v1.20.0", "Running version:", "v1.19.33", "Desired state:", "Stopped", strings.TrimSpace(drift)} {
				for _, word := range strings.Fields(value) {
					if words[word] == 0 {
						t.Errorf("overview lost word %q from detail %q", word, value)
					}
					words[word]--
				}
			}
			assertCanvas(t, body, width, len(rows))
			assertRegions(t, hits, width, len(rows))
			for id, label := range map[string]string{"card.import": "Import config", "card.proxies": "Open Proxies", "card.install": "Manage core"} {
				found := false
				for _, hit := range hits {
					if hit.ID == id {
						found = true
						visible := ansi.Strip(ansi.Cut(rows[hit.Y], hit.X, hit.X+hit.W))
						if !strings.Contains(visible, label) || !strings.Contains(visible, "[") {
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
		group := ansi.Strip(strings.Join(overviewCards(o)[1], "\n"))
		if !strings.Contains(group, "Core not installed") || strings.Contains(group, "Service unavailable") {
			t.Fatalf("uninstalled core was presented as a service failure: %s", group)
		}
	})
}

func TestGuideStagesAndNormalStop(t *testing.T) {
	o := dashboardOptions{Width: 160, Height: 45, Page: "overview", Snapshot: app.Snapshot{Installed: true}}
	value, targets, _ := renderDashboard(o)
	if !strings.Contains(ansi.Strip(value), "Choose your first configuration") || !hasAction(targets, "start") {
		t.Fatal("configuration stage missing")
	}
	o.Snapshot.ConfigReady = true
	value, targets, _ = renderDashboard(o)
	if !strings.Contains(ansi.Strip(value), "Start your proxy core") || !hasAction(targets, "start") {
		t.Fatal("start stage missing")
	}
	o.Snapshot.HasRun = true
	value, targets, _ = renderDashboard(o)
	if strings.Contains(ansi.Strip(value), "Get started") || hasAction(targets, "install") {
		t.Fatal("normal stop reopened installation guide")
	}
	o.Snapshot.HasRun = false
	o.Snapshot.Service.Active = true
	value, _, _ = renderDashboard(o)
	if strings.Contains(ansi.Strip(value), "Get started") {
		t.Fatal("running core still shown in startup guide")
	}
}

func TestDashboardMouseCoordinatesFollowScrolling(t *testing.T) {
	o := dashboardOptions{Width: 60, Height: 20, Page: "overview"}
	value, regions, total := renderDashboard(o)
	if total <= o.Height-7 {
		t.Fatal("short viewport should require scrolling")
	}
	lines := strings.Split(value, "\n")
	for _, r := range regions {
		if r.ID == "install" || r.ID == "import" || r.ID == "dismiss" {
			cells := ansi.Strip(ansi.Cut(lines[r.Y], r.X, r.X+r.W))
			if !strings.Contains(cells, "[") {
				t.Fatalf("target does not cover actual button: %+v %q", r, cells)
			}
		}
	}
	o.Scroll = 10000
	value, regions, _ = renderDashboard(o)
	if hasAction(regions, "install") || hasAction(regions, "dismiss") {
		t.Fatal("scrolled-away actions retained mouse targets")
	}
	if !strings.Contains(ansi.Strip(value), "Kernel information") {
		t.Fatal("bottom card is not reachable by scrolling")
	}
	if !hasAction(regions, "menu") {
		t.Fatal("narrow navigation missing")
	}
	o.SidebarOpen = true
	value, regions, _ = renderDashboard(o)
	for _, id := range []string{"nav.overview", "nav.proxies", "nav.profiles", "nav.settings"} {
		if !hasAction(regions, id) {
			t.Errorf("menu action missing: %s", id)
		}
	}
	assertCanvas(t, value, o.Width, o.Height)
	assertRegions(t, regions, o.Width, o.Height)
}

func TestSettingsOnlyOffersAvailableActions(t *testing.T) {
	o := dashboardOptions{Width: 120, Height: 40, Page: "settings"}
	_, regions, _ := renderDashboard(o)
	for _, id := range []string{"install", "network", "import"} {
		if !hasAction(regions, id) {
			t.Errorf("missing %s", id)
		}
	}
	for _, id := range []string{"start", "stop", "restart"} {
		if hasAction(regions, id) {
			t.Errorf("uninstalled core offers %s", id)
		}
	}
	o.Snapshot.Installed = true
	_, regions, _ = renderDashboard(o)
	if !hasAction(regions, "install") || hasAction(regions, "stop") || !hasAction(regions, "start") || !hasAction(regions, "restart") {
		t.Fatal("stopped actions incorrect")
	}
	o.Snapshot.Service.Active = true
	_, regions, _ = renderDashboard(o)
	if hasAction(regions, "start") || !hasAction(regions, "stop") || !hasAction(regions, "restart") {
		t.Fatal("running actions incorrect")
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
	if !strings.Contains(ansi.Strip(dimmed), "Get started") || !strings.Contains(ansi.Strip(dimmed), "Overview") {
		t.Fatal("dimmed backdrop lost underlying page")
	}
}

func TestShortCompactNavigationKeepsFocusedLastItemVisible(t *testing.T) {
	for _, size := range [][2]int{{80, 12}, {60, 10}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.width, m.height = size[0], size[1]
			m.sidebarOpen, m.focus = true, "nav.settings"
			m.layout()
			check := func(id, label string) {
				t.Helper()
				hit := findTarget(t, m, id)
				line := strings.Split(m.screen, "\n")[hit.Y]
				visible := ansi.Strip(ansi.Cut(line, hit.X, hit.X+hit.W))
				if !strings.Contains(visible, "› "+label) {
					t.Fatalf("focused menu item is hidden: %q", visible)
				}
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
			}
			check("nav.settings", "Settings")
			if size[1] == 12 {
				for _, page := range navPages() {
					findTarget(t, m, "nav."+page)
				}
			}
			before := m.scroll
			m, _ = update(m, special(tea.KeyUp))
			if m.focus != "nav.logs" || m.scroll != before {
				t.Fatal("arrow did not move back inside compact navigation")
			}
			check("nav.logs", "Logs")
		})
	}
}
