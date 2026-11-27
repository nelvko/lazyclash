package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func coreCardTarget(t *testing.T, block dashboardBlock, id string) (hitRegion, string) {
	t.Helper()
	for _, hit := range block.targets {
		if hit.ID == id {
			if hit.H != 1 {
				t.Fatalf("%s should occupy a single row: %+v", id, hit)
			}
			return hit, ansi.Strip(ansi.Cut(block.lines[hit.Y], hit.X, hit.X+hit.W))
		}
	}
	t.Fatalf("missing %s: %+v", id, block.targets)
	return hitRegion{}, ""
}

func TestCoreCardSeparatesManagementFromRightAlignedLifecycleButton(t *testing.T) {
	now := time.Now()
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{16, 36, 72} {
			for _, plain := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/plain=%t", language, width, plain), func(t *testing.T) {
					o := dashboardOptions{Locale: i18n.New(language), PlainButtons: plain, Now: now, Snapshot: app.Snapshot{
						Installed: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
						Service: service.Status{Active: true, State: "active", PID: 42, StartedAt: now.Add(-time.Hour)},
					}}
					block := overviewCoreCard(o, width)
					assertCanvas(t, strings.Join(block.lines, "\n"), width, len(block.lines))
					assertRegions(t, block.targets, width, len(block.lines))
					manage, link := coreCardTarget(t, block, "core.manage")
					hit, face := coreCardTarget(t, block, "core.toggle")
					label := o.Locale.T("dashboard.settings.stop")
					if len(block.targets) != 2 || strings.TrimLeft(link, " ") != o.Locale.T("dashboard.core.manageAction") || strings.Trim(face, " []") != label || hit.X+hit.W != width-2 {
						t.Fatalf("expected management link and right-aligned stop button: %+v %q %q", block.targets, link, face)
					}
					if plain && !strings.Contains(face, "[") {
						t.Fatalf("monochrome stop lost button boundary: %q", face)
					}
					if !plain {
						painted := ansi.Cut(block.lines[hit.Y], hit.X+2, hit.X+hit.W)
						if !strings.Contains(painted, themed(ansi.Strip(painted), text, buttonSecondaryBackground, false)) {
							t.Fatalf("stop lost its neutral action background: %q", painted)
						}
					}
					title := o.Locale.T("dashboard.overview.service")
					state, _, _ := headerCoreState(o)
					if width == 16 {
						if manage.X+manage.W != width-2 || hit.Y <= manage.Y || strings.Contains(ansi.Strip(block.lines[hit.Y]), state) ||
							!strings.Contains(ansi.Strip(strings.Join(block.lines[manage.Y+1:hit.Y], "\n")), state) {
							t.Fatalf("wrapped button is not below the complete state:\n%s", ansi.Strip(strings.Join(block.lines, "\n")))
						}
						return
					}
					if !strings.Contains(ansi.Strip(block.lines[manage.Y]), title) || manage.X+manage.W != hit.X+hit.W {
						t.Fatalf("right action edges differ: management=%+v stop=%+v", manage, hit)
					}
					if !strings.Contains(ansi.Strip(strings.Join(block.lines[manage.Y+1:hit.Y], "\n")), "Mihomo") ||
						strings.TrimSpace(ansi.Strip(ansi.Cut(block.lines[hit.Y], 0, hit.X))) != state {
						t.Fatalf("missing identity or state to the left of Stop:\n%s", ansi.Strip(strings.Join(block.lines, "\n")))
					}
					if got := strings.TrimSpace(ansi.Strip(block.lines[hit.Y+1])); got != o.Locale.T("dashboard.core.uptimeHours", 1) {
						t.Fatalf("fallback uptime is not below the state: %q", got)
					}
				})
			}
		}
	}
}

func TestCoreCardActionsKeepTheirSizeAndFocusAcrossLifecycle(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, plain := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/plain=%t", language, plain), func(t *testing.T) {
				var slot hitRegion
				for _, tc := range []struct {
					state, problem, action, want string
					active, busy                 bool
				}{
					{state: "active", active: true, want: "stop"},
					{state: "inactive", want: "start"},
					{state: "failed", want: "start"},
					{state: "active", active: true, busy: true},
					{state: "activating"},
					{state: "deactivating", active: true},
					{state: "active", active: true, busy: true, action: "restart"},
					{state: "active", active: true, problem: "fixture service unavailable"},
					{state: "unknown"},
				} {
					o := dashboardOptions{Locale: i18n.New(language), Busy: tc.busy, CoreAction: tc.action, PlainButtons: plain,
						Snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: tc.active, State: tc.state}, ServiceError: tc.problem}}
					value := "—"
					if tc.want != "" {
						value = o.Locale.T("dashboard.settings." + tc.want)
					}
					idle := overviewCoreCard(o, 72)
					hit, face := coreCardTarget(t, idle, "core.toggle")
					if strings.Trim(face, " []") != value {
						t.Fatalf("%s action = %q, want %q", tc.state, face, value)
					}
					if slot.ID == "" {
						slot = hit
					} else if hit.X != slot.X || hit.W != slot.W {
						t.Fatalf("%s moved the action: %+v, want %+v", tc.state, hit, slot)
					}
					o.Focus = "core.toggle"
					focusedHit, focused := coreCardTarget(t, overviewCoreCard(o, 72), "core.toggle")
					if focusedHit != hit || !strings.Contains(focused, "›") || strings.Trim(focused, " []›") != value {
						t.Fatalf("%s lost its visible focus or action: %q / %+v", tc.state, focused, focusedHit)
					}

					o.Focus, o.Hover = "", "core.toggle"
					hover := overviewCoreCard(o, 72)
					hoverHit, hoverFace := coreCardTarget(t, hover, "core.toggle")
					if hoverHit != hit || hoverFace != face {
						t.Fatalf("%s hover changed action text or target", tc.state)
					}
					if styled := idle.lines[hit.Y] != hover.lines[hit.Y]; styled != (tc.want != "") {
						t.Fatalf("%s hover feedback=%t for action %q", tc.state, styled, tc.want)
					}
				}
			})
		}
	}
}

func TestCoreCardMouseOnlyActivatesTheStartStopButton(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, active := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/active=%t", language, active), func(t *testing.T) {
				m, b := coreToggleTestModel(t, active)
				defer m.stopRuntime()
				m.locale = i18n.New(language)
				m.width = 80 // Exercise the read-only uptime beside the other card details.
				m.snapshot.Service.StartedAt = time.Now().Add(-time.Hour)
				m.layout()
				hit := findTarget(t, m, "core.toggle")
				calls := 0
				action := "start"
				if active {
					action = "stop"
				}
				b.control = func(_ context.Context, got string) error {
					calls++
					if got != action {
						t.Fatalf("clicked button requested %q, want %q", got, action)
					}
					return nil
				}

				state, _, _ := headerCoreState(m.navigationOptions())
				fields := []string{"Mihomo", state}
				if active {
					fields = append(fields, m.tr("dashboard.core.uptimeHours", 1))
				}
				g := dashboardGeometry(m.navigationOptions())
				for _, field := range fields {
					found := false
					for y, row := range strings.Split(m.screen, "\n") {
						body := ansi.Strip(ansi.Cut(row, g.x, hit.X))
						prefix, _, ok := strings.Cut(body, field)
						if !ok {
							continue
						}
						found = true
						m, _ = update(m, tea.MouseClickMsg{X: g.x + ansi.StringWidth(prefix), Y: y, Button: tea.MouseLeft})
						if m.busy || calls != 0 || b.reads != 0 {
							t.Fatalf("clicking read-only %q issued a service operation", field)
						}
						break
					}
					if !found {
						t.Fatalf("missing read-only field %q", field)
					}
				}
				m, cmd := update(m, tea.MouseClickMsg{X: hit.X + hit.W - 1, Y: hit.Y, Button: tea.MouseLeft})
				if cmd == nil || !m.busy || m.focus != "core.toggle" || calls != 0 {
					t.Fatal("clicking the button did not queue the action")
				}
				actionCmd(t, cmd)()
				if calls != 1 {
					t.Fatalf("button issued %d service requests, want one", calls)
				}
			})
		}
	}
}
