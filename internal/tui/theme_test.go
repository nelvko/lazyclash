package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// These are model fixtures, not running kernels, network requests or services.
// An explicit output directory exports the very same views for font rendering.
func themeSample(name string) Model {
	m := ready(&fakeBackend{})
	m.noColor, m.colorProfile = false, colorprofile.TrueColor
	switch name {
	case "overview-empty", "sidebar-focus", "sidebar-hover":
		if name == "sidebar-focus" {
			m.focus = "nav.proxies"
		} else if name == "sidebar-hover" {
			m.hover = "nav.proxies"
		}
	case "overview-running", "sidebar-connected":
		m.snapshot = app.Snapshot{Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.33", RunningVersion: "v1.19.33", Applied: "example", Profiles: []string{"default", "example"}, Groups: []mihomo.Group{{Name: "PROXY", Type: "Selector", Now: "Example node"}}, Service: service.Status{Active: true, State: "active"}, Mode: app.ModeStatus{Desired: "rule", Actual: "rule", Running: true}}
		m.snapshot.Capabilities = app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}}
		m.snapshot.Service.EnableState = "enabled"
		memory := uint64(48 << 20)
		m.runtime.received = true
		m.runtime.snapshot = app.RuntimeSnapshot{State: "ready", Traffic: &mihomo.Traffic{Up: 4096, Down: 128 << 10}, Memory: &memory, Connections: &mihomo.ConnectionSnapshot{UploadTotal: 16 << 20, DownloadTotal: 256 << 20}}
		if name == "sidebar-connected" {
			m.page, m.focus = "proxies", "nav.proxies"
		}
	case "overview-stopped":
		m.snapshot = app.Snapshot{Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.33", Applied: "example", Profiles: []string{"default", "example"}, Service: service.Status{State: "inactive"}, Mode: app.ModeStatus{Desired: "rule"}}
		m.snapshot.Capabilities = app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}}
		m.snapshot.Service.EnableState = "disabled"
		m.runtime.received = true
		m.runtime.snapshot = app.RuntimeSnapshot{State: "stopped", Service: m.snapshot.Service}
	case "overview-dismissed":
		m.snapshot.GuideDismissed = true
		m.focus = "nav.overview"
	case "help":
		m.helpOpen = true
	case "notification-success":
		m.message = "Routing mode saved"
	case "notification-error":
		m.failure = "Could not update the subscription: the server is temporarily unavailable. Your saved configuration is still available; check the connection and retry."
	case "notification-progress":
		m.busy = true
		m.message = "Fetching and validating configuration…"
	case "notification-stack", "notification-list", "notification-details", "notification-details-end", "notification-profile-error":
		m.locale = i18n.New(i18n.English)
		m.page, m.focus = "overview", "card.profiles"
		m.notification.items = []notificationItem{
			{ID: 1, Page: "settings", Context: "Save language", Kind: "done", Message: "Language saved. The new selection is active."},
			{ID: 2, Page: "profiles", Context: "Update subscription", Kind: "error", Message: "Subscription download timed out. Check your connection and retry.", Details: "Download failed: connection timeout.\nThe saved subscription remains available."},
			{ID: 3, Page: "overview", Context: "Start service", Kind: "error", Message: "Could not start Mihomo. Check the service and retry.", Details: "Start failed: the user service manager could not confirm that Mihomo is ready.\n" + strings.Repeat("A bounded notification viewport retains full diagnostic paragraphs.\n", 24) + "End of full diagnostic."},
		}
		m.notification.visible = true
		if name == "notification-list" {
			m.notification.panel.show(0)
		} else if name == "notification-details" || name == "notification-details-end" {
			m.notification.panel.show(3)
		} else if name == "notification-profile-error" {
			m.page, m.focus = "profiles", "profiles.add"
			m.profilesUI.url.SetValue("https://example.invalid/subscription")
			m.notification.items = m.notification.items[1:2]
		}
	case "notification-profile-validation":
		m.locale = i18n.New(i18n.English)
		m.page, m.focus = "profiles", "profiles.url"
		m.profilesUI.url.SetValue("1243")
		m.beginProfileURLImport()
	case "logs":
		m.snapshot = app.Snapshot{Installed: true, ConfigReady: true, Version: "v1.19.33", Service: service.Status{Active: true, State: "active"}}
		m.page, m.focus = "logs", "logs.follow"
		m.logs.paused = true
		m.logs.entries = []mihomo.LogEntry{
			{Level: "info", Message: "[TCP] example.invalid:443 uses PROXY[Example node]"},
			{Level: "warning", Message: "Download unavailable: [URL redacted]"},
			{Level: "gap", Message: "Capture paused; logs during this interval are not collected."},
		}
	default:
		m.openManagementWizard(false)
		switch name {
		case "version-input", "input-error":
			m.wizard.step, m.wizard.specific, m.wizard.focus = stepVersion, true, "version-input"
			if name == "input-error" {
				m.wizard.version.SetValue("not-a-version")
				m.wizard.version.Err = errors.New("Invalid stable version")
				m.wizard.err = "Enter a valid stable version, for example v1.19.33."
			}
		case "network", "network-tests":
			m.openNetwork(false)
			m.wizard.draft.Method = "accelerated"
			m.wizard.prefix.SetValue("https://accelerator.example.invalid/")
			m.wizard.versionTest = checkState{Status: "Failed", Detail: "Version query: HTTP 429 rate limit; retry later."}
			m.wizard.fileTest = checkState{Status: "Passed", Detail: "Release file probe passed (4 KiB)."}
			m.wizard.focus = "download-method"
			m.hover = "download-method"
		case "confirm":
			m.wizard.step, m.wizard.focus = stepConfirm, "install"
			m.wizard.plan = app.InstallPlan{Release: testRelease(), ActualPath: "/tmp/lazyclash-preview/installation/mihomo", Operation: "install", DiskKnown: true, FreeBytes: 1 << 30}
		case "installing", "disabled":
			m.wizard.step, m.wizard.focus = stepInstalling, "cancel"
			m.wizard.plan.Release = testRelease()
			m.wizard.progress = mihomo.Progress{Stage: "download", Downloaded: 9 << 20, Total: 16 << 20, BytesPerSecond: 2 << 20, Detail: "Downloading the selected release asset"}
			m.busy, m.job = true, "install"
			if name == "disabled" {
				m.wizard.cancelling = true
			}
		case "success", "failure":
			m.wizard.step, m.wizard.focus = stepResult, "done"
			m.wizard.result = app.InstallResult{InstalledVersion: "v1.19.33", ActualPath: "/tmp/lazyclash-preview/installation/mihomo", Operation: "install"}
			if name == "failure" {
				m.wizard.failedStage, m.wizard.err = "Verifying", "SHA256 mismatch: downloaded content does not match the expected digest."
				m.wizard.focus = "retry"
			}
		}
	}
	m.syncNotification()
	m.layout()
	if name == "network-tests" {
		m.wizard.scroll = 14
		m.layout()
	} else if name == "notification-details-end" {
		m.notification.panel.viewport.GotoBottom()
		m.layout()
	}
	return m
}

func TestMarianaPagesAcrossSizesAndProfiles(t *testing.T) {
	names := []string{"overview-empty", "overview-running", "overview-stopped", "sidebar-focus", "sidebar-hover", "core", "version-input", "input-error", "network", "confirm", "installing", "disabled", "success", "failure", "notification-success", "notification-error", "notification-progress", "notification-stack", "notification-list", "notification-details", "notification-profile-error", "notification-profile-validation"}
	for _, name := range names {
		for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {120, 18}, {20, 7}} {
			for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
				t.Run(fmt.Sprintf("%s/%dx%d/%s", name, size[0], size[1], profile), func(t *testing.T) {
					m := themeSample(name)
					m.width, m.height, m.colorProfile = size[0], size[1], profile
					m.layout()
					view := m.View().Content
					assertCanvas(t, view, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
					if ansi.Strip(view) != ansi.Strip(m.screen) {
						t.Fatal("color adaptation changed layout or text")
					}
					if strings.Contains(view, "private-proxy-token") {
						t.Fatal("theme exposed hidden input")
					}
					if profile == colorprofile.ASCII && strings.Contains(view, "\x1b") {
						t.Fatal("uncolored page retained styling")
					}
				})
			}
		}
	}
}

func TestMarianaSidebarCurrentHoverAndFocusAreIndependent(t *testing.T) {
	o := dashboardOptions{Width: 160, Height: 45, Page: "overview", Focus: "nav.proxies", Hover: "nav.rules"}
	screen, _, _ := renderDashboard(o)
	sidebar := sidebarText(screen)
	if strings.Contains(sidebar, "Overview •") || !strings.Contains(sidebar, "› Proxies") {
		t.Fatal("colored navigation repeated the current marker or lost keyboard focus")
	}
	for _, row := range strings.Split(screen, "\n") {
		line := ansi.Cut(row, 0, sidebarWidth)
		if strings.Contains(line, "\x1b[4m") {
			t.Fatal("sidebar added underline emphasis")
		}
	}
	if !strings.Contains(screen, themed("› ", focus, sidebarBackground, false)) {
		t.Fatal("keyboard focus lost its separate orange marker")
	}
	currentLabel := lipgloss.NewStyle().Bold(true).Render("Overview")
	if !strings.Contains(screen, themed(currentLabel, selectionForeground, bg, false)) {
		t.Fatal("current page lost its body background or bold label")
	}
	labelTail := func(label string) string {
		left := (sidebarWidth - ansi.StringWidth(label)) / 2
		return cellPad(label, sidebarWidth-1-left)
	}
	if !strings.Contains(screen, themed(labelTail("Rules"), text, sidebarBackground, false)) {
		t.Fatal("persistent hover lost its text emphasis or changed the sidebar background")
	}
	o.Hover = o.Focus
	hoveredFocus, _, _ := renderDashboard(o)
	if !strings.Contains(hoveredFocus, themed(labelTail("Proxies"), text, sidebarBackground, false)) || !strings.Contains(hoveredFocus, themed("› ", focus, sidebarBackground, false)) {
		t.Fatal("hovering the keyboard focus replaced its independent text or marker colors")
	}
	o.Hover = "nav.rules"
	o.PlainButtons = true
	plain, _, _ := renderDashboard(o)
	if !strings.Contains(sidebarText(plain), "Overview •") || !strings.Contains(sidebarText(plain), "› Proxies") {
		t.Fatal("low-color navigation lost independent current and focus text markers")
	}
	o.Width, o.Height, o.SidebarOpen, o.PlainButtons = 80, 24, true, false
	popup, _, _ := renderDashboard(o)
	if !strings.Contains(popup, themed(labelTail("Rules"), text, hoverBackground, false)) {
		t.Fatal("compact menu lost its hover surface")
	}
}

func TestThemedHeaderHitRegionsFollowRenderedButtons(t *testing.T) {
	for _, page := range []string{"connections", "rules"} {
		m := ready(&fakeBackend{})
		m.page = page
		m.width, m.height = 120, 32
		m.focus = map[string]string{"connections": "conn.retry", "rules": "rules.retry"}[page]
		m.layout()
		target := findTarget(t, m, m.focus)
		if target.Y >= dashboardGeometry(dashboardOptions{Width: m.width, Height: m.height, Page: page}).y {
			t.Fatalf("%s refresh action is outside the fixed header", page)
		}
		line := strings.Split(m.screen, "\n")[target.Y]
		painted := ansi.Strip(ansi.Cut(line, target.X, target.X+target.W))
		if ansi.StringWidth(painted) != target.W || (!strings.Contains(painted, "Refresh") && !strings.Contains(painted, "↻")) {
			t.Fatalf("%s refresh hit does not match the painted button: %q, %+v", page, painted, target)
		}
	}
}

func TestExportMarianaViews(t *testing.T) {
	dir := os.Getenv("LAZYCLASH_THEME_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set LAZYCLASH_THEME_PREVIEW_DIR explicitly to export model views")
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("preview directory must be absolute")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		name    string
		w, h    int
		profile colorprofile.Profile
	}{
		{"overview-empty", 160, 45, colorprofile.TrueColor},
		{"overview-empty", 216, 55, colorprofile.TrueColor},
		{"overview-empty", 120, 32, colorprofile.TrueColor},
		{"notification-success", 120, 32, colorprofile.TrueColor},
		{"notification-error", 120, 32, colorprofile.TrueColor},
		{"notification-error", 60, 20, colorprofile.TrueColor},
		{"notification-progress", 80, 24, colorprofile.TrueColor},
		{"notification-error", 80, 24, colorprofile.ASCII},
		{"notification-stack", 160, 45, colorprofile.TrueColor},
		{"notification-stack", 120, 32, colorprofile.TrueColor},
		{"notification-stack", 80, 24, colorprofile.TrueColor},
		{"notification-stack", 60, 20, colorprofile.TrueColor},
		{"notification-stack", 120, 18, colorprofile.TrueColor},
		{"notification-stack", 80, 24, colorprofile.ASCII},
		{"notification-list", 120, 32, colorprofile.TrueColor},
		{"notification-list", 60, 20, colorprofile.TrueColor},
		{"notification-details", 120, 32, colorprofile.TrueColor},
		{"notification-details-end", 120, 32, colorprofile.TrueColor},
		{"notification-profile-error", 120, 32, colorprofile.TrueColor},
		{"notification-profile-validation", 120, 32, colorprofile.TrueColor},
		{"notification-profile-validation", 60, 20, colorprofile.TrueColor},
		{"overview-running", 160, 45, colorprofile.TrueColor},
		{"overview-running", 216, 55, colorprofile.TrueColor},
		{"overview-running", 120, 32, colorprofile.TrueColor},
		{"overview-running", 80, 24, colorprofile.TrueColor},
		{"sidebar-connected", 160, 45, colorprofile.TrueColor},
		{"sidebar-connected", 80, 24, colorprofile.TrueColor},
		{"sidebar-hover", 160, 45, colorprofile.TrueColor},
		{"overview-stopped", 160, 45, colorprofile.TrueColor},
		{"overview-stopped", 120, 32, colorprofile.TrueColor},
		{"overview-dismissed", 160, 45, colorprofile.TrueColor},
		{"overview-dismissed", 120, 32, colorprofile.TrueColor},
		{"overview-dismissed", 80, 24, colorprofile.TrueColor},
		{"logs", 120, 32, colorprofile.TrueColor},
		{"help", 80, 24, colorprofile.TrueColor},
		{"core", 120, 32, colorprofile.TrueColor},
		{"version-input", 80, 24, colorprofile.TrueColor},
		{"input-error", 80, 24, colorprofile.TrueColor},
		{"network", 120, 32, colorprofile.TrueColor},
		{"network-tests", 120, 32, colorprofile.TrueColor},
		{"confirm", 120, 32, colorprofile.TrueColor},
		{"installing", 80, 24, colorprofile.TrueColor},
		{"success", 80, 24, colorprofile.TrueColor},
		{"failure", 80, 24, colorprofile.TrueColor},
		{"disabled", 60, 20, colorprofile.TrueColor},
		{"core", 120, 18, colorprofile.TrueColor},
		{"core", 120, 32, colorprofile.ANSI256},
		{"core", 80, 24, colorprofile.ASCII},
	} {
		m := themeSample(sample.name)
		m, _ = update(m, tea.WindowSizeMsg{Width: sample.w, Height: sample.h})
		m.colorProfile = sample.profile
		filename := fmt.Sprintf("%s-%dx%d-%s.ansi", sample.name, sample.w, sample.h, sample.profile)
		if err := os.WriteFile(filepath.Join(dir, filename), []byte(m.View().Content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
