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

func coreCardAction(t *testing.T, block dashboardBlock) (hitRegion, string) {
	t.Helper()
	if len(block.targets) != 1 || block.targets[0].ID != "core.toggle" || block.targets[0].H != 1 {
		t.Fatalf("expected one single-row core action, got %+v", block.targets)
	}
	hit := block.targets[0]
	return hit, ansi.Strip(ansi.Cut(block.lines[hit.Y], hit.X, hit.X+hit.W))
}

func TestCoreCardPlacesActionInHeadingAndGroupsReadOnlyDetails(t *testing.T) {
	now := time.Now()
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{16, 30, 72} {
			for _, plain := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/plain=%t", language, width, plain), func(t *testing.T) {
					o := dashboardOptions{Locale: i18n.New(language), PlainButtons: plain, Now: now, Snapshot: app.Snapshot{
						Installed: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
						Service: service.Status{Active: true, State: "active", PID: 42, StartedAt: now.Add(-time.Hour)},
					}}
					block := overviewCoreCard(o, width)
					assertCanvas(t, strings.Join(block.lines, "\n"), width, len(block.lines))
					assertRegions(t, block.targets, width, len(block.lines))
					hit, face := coreCardAction(t, block)
					if strings.Trim(face, " []") != "● ON" {
						t.Fatalf("click target misses the running switch: %q", face)
					}
					title := o.Locale.T("dashboard.overview.service")
					if width == 16 {
						if hit.Y != 2 || strings.TrimSpace(ansi.Strip(block.lines[hit.Y-1])) != title {
							t.Fatalf("narrow heading failed to move the complete action below %q: %+v", title, hit)
						}
						return
					}
					if !strings.Contains(ansi.Strip(ansi.Cut(block.lines[hit.Y], 0, hit.X)), title) || hit.X+hit.W != width-2 {
						t.Fatalf("heading action not at the right edge: %+v / %q", hit, ansi.Strip(block.lines[hit.Y]))
					}
					full, _, _ := headerCoreState(o)
					want := []string{full, o.Locale.T("dashboard.core.startedHours", 1)}
					var body []string
					for _, line := range block.lines[hit.Y+1:] {
						line = strings.TrimRight(ansi.Strip(line), " ")
						if line != "" {
							body = append(body, line)
						}
					}
					if len(body) != len(want) {
						t.Fatalf("unexpected card body: %q", body)
					}
					for i, value := range want {
						if body[i] != "  "+value {
							t.Fatalf("body row %d = %q, want left-aligned %q", i, body[i], value)
						}
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
					{state: "active", active: true, want: "● ON"},
					{state: "inactive", want: "OFF ○"},
					{state: "failed", want: "OFF ○"},
					{state: "active", active: true, busy: true},
					{state: "activating"},
					{state: "deactivating", active: true},
					{state: "active", active: true, busy: true, action: "restart"},
					{state: "active", active: true, problem: "fixture service unavailable"},
					{state: "unknown"},
				} {
					o := dashboardOptions{Locale: i18n.New(language), Busy: tc.busy, CoreAction: tc.action, PlainButtons: plain,
						Snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: tc.active, State: tc.state}, ServiceError: tc.problem}}
					value := tc.want
					if value == "" {
						value = "—"
					}
					idle := overviewCoreCard(o, 72)
					hit, face := coreCardAction(t, idle)
					if strings.Trim(face, " []") != value {
						t.Fatalf("%s action = %q, want %q", tc.state, face, value)
					}
					if slot.ID == "" {
						slot = hit
					} else if hit != slot {
						t.Fatalf("%s moved the action: %+v, want %+v", tc.state, hit, slot)
					}
					o.Focus = "core.toggle"
					focusedHit, focused := coreCardAction(t, overviewCoreCard(o, 72))
					if focusedHit != hit || !strings.HasPrefix(strings.TrimSpace(focused), "›") || strings.Trim(focused, " []›") != value {
						t.Fatalf("%s lost its visible focus or action: %q / %+v", tc.state, focused, focusedHit)
					}

					o.Focus, o.Hover = "", "core.toggle"
					hover := overviewCoreCard(o, 72)
					hoverHit, hoverFace := coreCardAction(t, hover)
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

func TestCoreCardMouseOnlyActivatesTheSwitch(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, active := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/active=%t", language, active), func(t *testing.T) {
				m, b := coreToggleTestModel(t, active)
				defer m.stopRuntime()
				m.locale = i18n.New(language)
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
				fields := []string{state}
				if active {
					fields = append(fields, m.tr("dashboard.core.startedHours", 1))
				}
				g := dashboardGeometry(m.navigationOptions())
				for _, field := range fields {
					found := false
					for y, row := range strings.Split(m.screen, "\n") {
						if y <= hit.Y {
							continue
						}
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
