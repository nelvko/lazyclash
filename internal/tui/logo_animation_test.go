package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSidebarLogoIsClickableWithoutBecomingATabStop(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height = 120, 24
	m.layout()
	logoHit := findTarget(t, m, "logo")
	if logoHit.Y != dashboardGeometry(m.dashboardOptions()).headerY || logoHit.W != sidebarWidth-1 {
		t.Fatalf("unexpected logo target: %+v", logoHit)
	}
	for _, target := range m.focusTargets() {
		if target.ID == "logo" {
			t.Fatal("logo should be mouse-only and not a tab stop")
		}
	}
	if !strings.Contains(ansi.Strip(m.screen), logo) {
		t.Fatal("static logo missing from sidebar")
	}
}

func TestSidebarLogoPulseAdvancesAndIgnoresStaleTicks(t *testing.T) {
	m := ready(&fakeBackend{})
	m.width, m.height = 120, 24
	m.layout()
	logoHit := findTarget(t, m, "logo")
	before := m.logoGeneration
	m, _ = update(m, tea.MouseClickMsg{X: logoHit.X + 1, Y: logoHit.Y, Button: tea.MouseLeft})
	if m.logoGeneration != before+1 || m.logoFrame != 1 {
		t.Fatalf("logo click did not start pulse: generation=%d frame=%d", m.logoGeneration, m.logoFrame)
	}
	if !strings.Contains(ansi.Strip(m.screen), "la!!yClash") {
		t.Fatalf("first pulse frame missing from screen: %q", ansi.Strip(m.screen))
	}
	generation := m.logoGeneration
	m, _ = update(m, logoPulseMsg{generation: generation + 1})
	if m.logoFrame != 1 {
		t.Fatal("stale logo pulse changed the active animation")
	}
	for expected := 2; expected < len(logoFrames); expected++ {
		m, _ = update(m, logoPulseMsg{generation: generation})
		if m.logoFrame != expected {
			t.Fatalf("pulse frame mismatch: got %d want %d", m.logoFrame, expected)
		}
	}
	if cmd := m.advanceLogoPulse(logoPulseMsg{generation: generation}); cmd != nil {
		t.Fatal("settled animation scheduled another tick")
	}
	m.layout()
	if m.logoFrame != 0 {
		t.Fatalf("pulse did not settle: frame=%d", m.logoFrame)
	}
	if !strings.Contains(ansi.Strip(m.screen), logo) || strings.TrimSpace(ansi.Strip(ansi.Cut(strings.Split(m.screen, "\n")[0], 0, sidebarWidth))) != "" {
		t.Fatal("settled logo retained a wake frame or dream bubble")
	}
	if cmd := m.advanceLogoPulse(logoPulseMsg{generation: generation}); cmd != nil || m.logoFrame != 0 {
		t.Fatal("late tick restarted the settled animation")
	}
}

func TestSidebarLogoFramesKeepTheWordmarkAndNavigationFixed(t *testing.T) {
	centers := []string{"💤", "!!", "..", "z ", " Z", "💤", "💤", "💤", "💤", "💤", "💤", "💤", "💤"}
	bubbles := []string{"", "", "", "", "", "", "z", "Z", "Z", "", "z", "Z", ""}
	offsets := []int{0, 0, 0, 0, 0, 0, 0, 1, 2, 0, 0, 1, 0}
	for _, size := range [][2]int{{100, 20}, {120, 24}, {160, 45}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			o := dashboardOptions{Width: size[0], Height: size[1], Page: "overview", Focus: "nav.overview"}
			baseline, _, _ := renderDashboard(o)
			baseRows := strings.Split(ansi.Strip(baseline), "\n")
			headerY := dashboardGeometry(o).headerY
			logoX := strings.Index(baseRows[headerY], "la")
			if logoX < 0 {
				t.Fatal("baseline sidebar logo is missing")
			}
			for index, center := range centers {
				o.LogoFrame = index
				screen, hits, _ := renderDashboard(o)
				assertCanvas(t, screen, o.Width, o.Height)
				assertRegions(t, hits, o.Width, o.Height)
				rows := strings.Split(ansi.Strip(screen), "\n")
				wordmark := ansi.Cut(rows[headerY], logoX, logoX+10)
				if want := "la" + center + "yClash"; wordmark != want {
					t.Fatalf("frame %d moved or widened the logo: got %q want %q", index, wordmark, want)
				}
				for row := range rows {
					if row == headerY {
						if ansi.Cut(rows[row], 0, logoX) != ansi.Cut(baseRows[row], 0, logoX) || ansi.Cut(rows[row], logoX+10, o.Width) != ansi.Cut(baseRows[row], logoX+10, o.Width) {
							t.Fatalf("frame %d altered cells outside the fixed logo", index)
						}
						continue
					}
					if headerY > 0 && row == headerY-1 {
						bubbleX := logoX + 2 + offsets[index]
						if bubbles[index] == "" {
							if rows[row] != baseRows[row] {
								t.Fatalf("frame %d retained a bubble in its quiet phase", index)
							}
						} else if ansi.Cut(rows[row], bubbleX, bubbleX+1) != bubbles[index] || strings.TrimSpace(ansi.Cut(rows[row], 0, sidebarWidth)) != bubbles[index] {
							t.Fatalf("frame %d bubble escaped its origin above the emoji: %q", index, rows[row])
						}
						if ansi.Cut(rows[row], sidebarWidth, o.Width) != ansi.Cut(baseRows[row], sidebarWidth, o.Width) {
							t.Fatalf("frame %d painted into the page header", index)
						}
						continue
					}
					if rows[row] != baseRows[row] {
						t.Fatalf("frame %d painted into row %d outside the logo chrome", index, row)
					}
				}
			}
		})
	}
}

func TestSidebarLogoPulseLastsOnePointSixSecondsAndRestartsOnClick(t *testing.T) {
	var duration time.Duration
	for _, frame := range logoFrames {
		duration += frame.duration
	}
	if duration != 1600*time.Millisecond {
		t.Fatalf("wake and dream sequence lasts %s, want 1.6s", duration)
	}
	m := ready(&fakeBackend{})
	m.width, m.height = 120, 24
	m.layout()
	hit := findTarget(t, m, "logo")
	page, focus, scroll := m.page, m.focus, m.scroll
	m, _ = click(m, hit)
	oldGeneration := m.logoGeneration
	m, _ = update(m, logoPulseMsg{generation: oldGeneration})
	m, _ = click(m, hit)
	if m.logoFrame != 1 || m.logoGeneration != oldGeneration+1 {
		t.Fatal("second click did not restart the wake phase with a new generation")
	}
	if m.page != page || m.focus != focus || m.scroll != scroll {
		t.Fatal("logo click changed page, focus or content scroll")
	}
	m, _ = update(m, logoPulseMsg{generation: oldGeneration})
	if m.logoFrame != 1 {
		t.Fatal("previous playback's tick advanced the restarted animation")
	}
}

func TestSidebarLogoPulseStopsWhenHiddenOrBlocked(t *testing.T) {
	for _, name := range []string{"compact", "short", "busy", "quitting", "help", "ports", "wizard", "network", "form", "notifications", "menu", "canceled"} {
		t.Run(name, func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.width, m.height = 120, 24
			m.layout()
			if cmd := m.startLogoPulse(); cmd == nil {
				t.Fatal("visible logo did not schedule its first frame")
			}
			generation := m.logoGeneration
			switch name {
			case "compact":
				m.width = 80
			case "short":
				m.height = 18
			case "busy":
				m.busy = true
			case "quitting":
				m.quitting = true
			case "help":
				m.helpOpen = true
			case "ports":
				m.ports.open = true
			case "wizard":
				m.wizard.open = true
			case "network":
				m.wizard.networkOpen = true
			case "form":
				m.form = "source"
			case "notifications":
				m.notification.panel.open = true
			case "menu":
				m.menu.open = true
			case "canceled":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				m.ctx = ctx
			}
			if cmd := m.advanceLogoPulse(logoPulseMsg{generation: generation}); cmd != nil || m.logoFrame != 0 || m.logoGeneration == generation {
				t.Fatalf("blocked animation continued: frame=%d generation=%d cmd=%v", m.logoFrame, m.logoGeneration, cmd != nil)
			}
			if cmd := m.startLogoPulse(); cmd != nil || m.logoFrame != 0 {
				t.Fatal("blocked logo accepted a new animation")
			}
			if cmd := m.advanceLogoPulse(logoPulseMsg{generation: generation}); cmd != nil || m.logoFrame != 0 {
				t.Fatal("old tick restarted a blocked logo")
			}
		})
	}
}

func TestSidebarLogoPulseCancelsOnResizeAndOverlayBeforeItsNextTick(t *testing.T) {
	for _, name := range []string{"resize", "overlay"} {
		t.Run(name, func(t *testing.T) {
			m := ready(&fakeBackend{})
			m.width, m.height = 120, 24
			m.layout()
			m, _ = click(m, findTarget(t, m, "logo"))
			generation := m.logoGeneration
			if name == "resize" {
				m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
			} else {
				m.helpOpen = true
				m, _ = update(m, struct{}{})
			}
			if m.logoFrame != 0 || m.logoGeneration == generation {
				t.Fatal("hidden logo kept its pending playback")
			}
			m.helpOpen = false
			m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 24})
			m, _ = update(m, logoPulseMsg{generation: generation})
			if m.logoFrame != 0 {
				t.Fatal("returning to the sidebar revived a canceled playback")
			}
		})
	}
}
