package tui

import (
	"fmt"
	"strings"
	"testing"

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
		for _, page := range []string{"overview", "nodes", "profiles", "settings"} {
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
	plain := ansi.Strip(value)
	for _, part := range []string{"Get started", "Install your first proxy core", "Current subscription", "Selected proxy group", "Traffic statistics", "Kernel and environment", "Core not installed", "LOCAL /"} {
		if !strings.Contains(plain, part) {
			t.Errorf("missing %q", part)
		}
	}
	for _, id := range []string{"install", "import", "dismiss", "nav.overview", "nav.nodes", "nav.profiles", "nav.settings", "nav.rules"} {
		if !hasAction(targets, id) {
			t.Errorf("missing action %s", id)
		}
	}
	for _, part := range []string{"Logs", "sing-box"} {
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
	if !strings.Contains(ansi.Strip(value), "Kernel and environment") {
		t.Fatal("bottom card is not reachable by scrolling")
	}
	if !hasAction(regions, "menu") {
		t.Fatal("narrow navigation missing")
	}
	o.SidebarOpen = true
	value, regions, _ = renderDashboard(o)
	for _, id := range []string{"nav.overview", "nav.nodes", "nav.profiles", "nav.settings"} {
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
	o := dashboardOptions{Width: 80, Height: 24, Page: "nodes", Body: strings.Repeat("row\n", 30), BodyTargets: []hitRegion{{ID: "node.first", X: 3, Y: 0, W: 8, H: 1}, {ID: "node.last", X: 3, Y: 29, W: 8, H: 1}}}
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
	if selected == focused || !strings.Contains(ansi.Strip(focused), "› 1 Overview") {
		t.Fatal("focus is indistinguishable from selected navigation")
	}
	o.Dim = true
	dimmed, _, _ := renderDashboard(o)
	if !strings.Contains(ansi.Strip(dimmed), "Get started") || !strings.Contains(ansi.Strip(dimmed), "Overview") {
		t.Fatal("dimmed backdrop lost underlying page")
	}
}
