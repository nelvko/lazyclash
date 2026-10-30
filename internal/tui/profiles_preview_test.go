package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/service"
)

// Synthetic metadata exercises the actual view, without contacting sources or
// changing a service. Previews intentionally contain no real subscription data.
func profilesPreview(detail bool) Model {
	now := time.Now()
	snapshot := app.Snapshot{
		Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.33",
		Applied: "Everyday", ProfilePending: true, Service: service.Status{Active: true, State: "active"},
		Profiles: []string{"Everyday", "Home", "Travel"},
		ProfileDetails: []app.ProfileInfo{
			{ID: "default", Name: "default", Core: "mihomo", Validated: true},
			{ID: "Everyday", Name: "Everyday", Core: "mihomo", Refreshable: true, Kind: "remote", Origin: "subscription.example", UpdatedAt: now.Add(-2 * time.Hour), Validated: true, Pending: true, HasOverrides: true},
			{ID: "Home", Name: "Home", Core: "mihomo", Refreshable: true, Kind: "local", Origin: "Local file", UpdatedAt: now.Add(-3 * 24 * time.Hour), Validated: true},
			{ID: "Travel", Name: "Travel", Core: "mihomo", Kind: "imported", Origin: "Imported file", UpdatedAt: now.Add(-20 * time.Minute), Validated: true},
		},
	}
	m := ready(&fakeBackend{snapshot: snapshot})
	m.page, m.focus = "profiles", "profile.1"
	m.noColor, m.colorProfile = false, colorprofile.TrueColor
	if detail {
		m.profilesUI.detail, m.focus = "Everyday", "profiles.apply.1"
	}
	return m
}

func TestProfilesCanvasAcrossSizesAndColors(t *testing.T) {
	for _, size := range [][2]int{{216, 55}, {160, 45}, {120, 32}, {80, 24}, {60, 20}, {30, 12}, {20, 7}, {8, 4}} {
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ASCII} {
			for _, detail := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/%s/details=%v", size[0], size[1], profile, detail), func(t *testing.T) {
					m := profilesPreview(detail)
					m.profilesUI.url.SetValue("https://private-user:private-password@example.invalid/private-path?token=private-query")
					m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					m.colorProfile = profile
					view := m.View().Content
					assertCanvas(t, view, size[0], size[1])
					assertRegions(t, m.targets, size[0], size[1])
					if profile == colorprofile.ASCII && strings.Contains(view, "\x1b") {
						t.Fatal("NO_COLOR retained styling")
					}
					if ansi.Strip(view) != ansi.Strip(m.screen) {
						t.Fatal("color adaptation changed the Profiles geometry")
					}
				})
			}
		}
	}
}

func TestExportProfilesViews(t *testing.T) {
	dir := os.Getenv("LAZYCLASH_PROFILES_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set LAZYCLASH_PROFILES_PREVIEW_DIR to export synthetic Profiles views")
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("preview directory must be absolute")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}} {
		for _, detail := range []bool{false, true} {
			m := profilesPreview(detail)
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			name := fmt.Sprintf("profiles-%dx%d-details-%v.ansi", size[0], size[1], detail)
			if err := os.WriteFile(filepath.Join(dir, name), []byte(m.View().Content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, size := range [][2]int{{80, 24}, {40, 16}, {24, 7}} {
		m := profilesPreview(false)
		m.openForm()
		m.input.SetValue("~/configs/con")
		m.paths = pathCompletion{active: true, query: m.input.Value(), index: 1, matches: []pathMatch{
			{path: "~/configs/config folder/", label: "config folder/", directory: true},
			{path: "~/configs/config.yaml", label: "config.yaml"},
			{path: "~/configs/config.yml", label: "config.yml"},
		}}
		m.width, m.height = size[0], size[1]
		m.layout()
		name := fmt.Sprintf("profiles-import-%dx%d.ansi", size[0], size[1])
		if err := os.WriteFile(filepath.Join(dir, name), []byte(m.View().Content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
