package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
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
			m.focus = "nav.nodes"
		} else if name == "sidebar-hover" {
			m.hover = "nav.nodes"
		}
	case "overview-running":
		m.snapshot = app.Snapshot{Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.33", RunningVersion: "v1.19.33", Applied: "example", Profiles: []string{"default", "example"}, Groups: []mihomo.Group{{Name: "PROXY", Type: "Selector", Now: "Example node"}}, Service: service.Status{Active: true, State: "active"}}
		memory := uint64(48 << 20)
		m.runtime.received = true
		m.runtime.snapshot = app.RuntimeSnapshot{State: "ready", Traffic: &mihomo.Traffic{Up: 4096, Down: 128 << 10}, Memory: &memory, Connections: &mihomo.ConnectionSnapshot{UploadTotal: 16 << 20, DownloadTotal: 256 << 20}}
	default:
		m.openWizard(false)
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
			m.wizard.draft.Source, m.wizard.draft.ProxyMode = "prefix", "custom"
			m.wizard.prefix.SetValue("https://accelerator.example.invalid/")
			m.wizard.proxy.SetValue("http://127.0.0.1:7890")
			m.wizard.versionTest = checkState{Status: "Failed", Detail: "Version query: HTTP 429 rate limit; retry later."}
			m.wizard.fileTest = checkState{Status: "Passed", Detail: "Release file probe passed (4 KiB)."}
			m.wizard.focus = "source"
			m.hover = "proxy-mode"
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
	m.layout()
	if name == "network-tests" {
		m.wizard.scroll = 14
		m.layout()
	}
	return m
}

func TestMarianaPagesAcrossSizesAndProfiles(t *testing.T) {
	names := []string{"overview-empty", "overview-running", "sidebar-focus", "sidebar-hover", "core", "version-input", "input-error", "network", "confirm", "installing", "disabled", "success", "failure"}
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
	o := dashboardOptions{Width: 160, Height: 45, Page: "overview", Focus: "nav.nodes", Hover: "nav.rules"}
	screen, _, _ := renderDashboard(o)
	plain := ansi.Strip(screen)
	if !strings.Contains(plain, "1 Overview •") || !strings.Contains(plain, "› 2 Nodes") {
		t.Fatal("current page and keyboard focus lost their independent text markers")
	}
	if !strings.Contains(screen, themed(" "+cellPad("6 Rules", 18)+" ", text, hoverBackground, false)) {
		t.Fatal("hover did not use its own surface")
	}
}

func TestThemedToolbarHitRegionsFollowRenderedButtons(t *testing.T) {
	for _, page := range []string{"connections", "rules"} {
		m := ready(&fakeBackend{})
		m.page = page
		m.focus = map[string]string{"connections": "conn.retry", "rules": "rules.retry"}[page]
		body, targets := m.pageBody(80)
		for _, target := range targets {
			if target.ID == m.focus && target.W != ansi.StringWidth(strings.Split(body, "\n")[target.Y]) {
				t.Fatalf("%s focused button hit width does not match its rendered cells", page)
			}
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
		{"overview-running", 120, 32, colorprofile.TrueColor},
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
