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

func TestCoreCardKeepsManagementAndStatusAsSeparateTargets(t *testing.T) {
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
					stops := block.overviewFocusRegions()
					if len(stops) != 2 {
						t.Fatalf("expected management and status controls: %+v", stops)
					}
					hit := stops[1]
					if hit.ID != "core.toggle" || strings.TrimSpace(link) != o.Locale.T("dashboard.core.manageAction") ||
						hit.X != 2 || manage.X+manage.W != width-2 || hit.Y <= manage.Y {
						t.Fatalf("missing left-aligned status under the management heading: %+v", block.targets)
					}
					body := ansi.Strip(strings.Join(block.lines[manage.Y+1:hit.Y], "\n"))
					if !strings.Contains(body, "Mihomo") {
						t.Fatal("Mihomo identity missing above status")
					}
					if width >= 36 {
						_, face := coreCardTarget(t, block, "core.toggle")
						want := "● " + o.Locale.T("dashboard.status.running")
						if plain {
							want = "[" + want + "]"
						}
						if strings.TrimSpace(face) != want || !strings.Contains(block.lines[hit.Y+1], o.Locale.T("dashboard.core.uptimeHours", 1)) {
							t.Fatalf("expected actionable state above uptime: %q", face)
						}
					}
				})
			}
		}
	}
}

func TestCoreStatusOnlyOffersAnActionForConfirmedServiceStates(t *testing.T) {
	for _, tc := range []struct {
		state                    string
		active, busy, actionable bool
	}{
		{state: "active", active: true, actionable: true}, {state: "inactive", actionable: true}, {state: "failed", actionable: true},
		{state: "active", active: true, busy: true}, {state: "activating"}, {state: "deactivating", active: true}, {state: "unknown"},
	} {
		o := dashboardOptions{Busy: tc.busy, Focus: "core.toggle", Snapshot: app.Snapshot{
			Installed: true, Service: service.Status{Active: tc.active, State: tc.state},
		}}
		card := overviewCoreCard(o, 72)
		label, _, _ := headerCoreState(o)
		if got := hasAction(card.targets, "core.toggle"); got != tc.actionable || !strings.Contains(ansi.Strip(strings.Join(card.lines, "\n")), label) {
			t.Fatalf("%s: action=%t, label=%q", tc.state, got, label)
		}
		if tc.actionable {
			hit, focused := coreCardTarget(t, card, "core.toggle")
			o.Focus, o.Hover = "", "core.toggle"
			hover, face := coreCardTarget(t, overviewCoreCard(o, 72), "core.toggle")
			if hover != hit || !strings.Contains(focused, "›") || strings.Contains(face, "›") {
				t.Fatalf("%s: focus/hover moved the status", tc.state)
			}
		}
	}
}

func TestCoreCardMouseOnlyActivatesTheStatusSwitch(t *testing.T) {
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

				fields := []string{"Mihomo"}
				if active {
					fields = append(fields, m.tr("dashboard.core.uptimeHours", 1))
				}
				g := dashboardGeometry(m.navigationOptions())
				for _, field := range fields {
					found := false
					for y, row := range strings.Split(m.screen, "\n") {
						body := ansi.Strip(ansi.Cut(row, g.x, g.x+g.width))
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

func TestWrappedCoreSwitchHitRegionsMatchPaintedLines(t *testing.T) {
	m, _ := coreToggleTestModel(t, true)
	defer m.stopRuntime()
	m.noColor, m.width, m.height = true, 20, 40
	m.layout()
	var rows []hitRegion
	for _, h := range m.targets {
		if h.ID == "core.toggle" {
			rows = append(rows, h)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("expected a wrapped core switch at 20 columns: %+v", rows)
	}
	want := []string{"›[●", m.tr("dashboard.status.running") + "]"}
	lines := strings.Split(m.screen, "\n")
	for i, h := range rows {
		line := ansi.Strip(lines[h.Y])
		face := ansi.Cut(line, h.X, h.X+h.W)
		if strings.TrimSpace(face) != want[i] || !strings.HasSuffix(face, want[i]) {
			t.Fatalf("row %d hit region extends beyond its painted face: %q", i, face)
		}
		after := h.X + h.W
		if ansi.Cut(line, after, after+1) != " " {
			t.Fatalf("row %d has no adjacent blank cell", i)
		}
		outside, _ := update(m, tea.MouseClickMsg{X: after, Y: h.Y, Button: tea.MouseLeft})
		if outside.busy {
			t.Fatalf("clicking the blank cell after row %d activated the switch", i)
		}
		inside, _ := update(m, tea.MouseClickMsg{X: after - 1, Y: h.Y, Button: tea.MouseLeft})
		if !inside.busy {
			t.Fatalf("clicking the last painted cell of row %d missed the switch", i)
		}
	}
	next, _ := update(m, special(tea.KeyTab))
	back, _ := update(next, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if next.focus == "core.toggle" || back.focus != "core.toggle" {
		t.Fatalf("wrapped switch should occupy one Tab stop, got %q then %q", next.focus, back.focus)
	}
}
