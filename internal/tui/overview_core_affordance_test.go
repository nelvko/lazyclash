package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestCoreCardGroupsIdentityAndExposesStatusAction(t *testing.T) {
	now := time.Now()
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, width := range []int{30, 72} {
			for _, plain := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/plain=%t", language, width, plain), func(t *testing.T) {
					o := dashboardOptions{Locale: i18n.New(language), PlainButtons: plain, Now: now, Snapshot: app.Snapshot{
						Installed: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
						Service: service.Status{Active: true, State: "active", PID: 42, StartedAt: now.Add(-time.Hour)},
					}}
					block := overviewCoreCard(o, width)
					assertCanvas(t, strings.Join(block.lines, "\n"), width, len(block.lines))
					assertRegions(t, block.targets, width, len(block.lines))
					if len(block.targets) != 1 || block.targets[0].ID != "core.toggle" || block.targets[0].H != 2 {
						t.Fatalf("status and hint must share one two-row control: %+v", block.targets)
					}
					hit := block.targets[0]
					face := strings.TrimSpace(ansi.Strip(ansi.Cut(block.lines[hit.Y], hit.X, hit.X+hit.W)))
					if !strings.HasPrefix(face, "[ ") || !strings.HasSuffix(face, " ]") || !strings.Contains(face, "● "+o.Locale.T("dashboard.core.toggle.on")) {
						t.Fatalf("status lacks visible button delimiters: %q", face)
					}
					hint := strings.TrimSpace(ansi.Strip(ansi.Cut(block.lines[hit.Y+1], hit.X, hit.X+hit.W)))
					if hint != o.Locale.T("dashboard.core.toggle.stopHint") {
						t.Fatalf("hint lies outside the clickable status control: %q", hint)
					}
					identityY, uptimeY := -1, -1
					for y, line := range block.lines {
						line = ansi.Strip(line)
						if strings.Contains(line, "Mihomo  v1.19.33") {
							identityY = y
						}
						if strings.Contains(line, o.Locale.T("dashboard.core.uptime", "01:00:00")) {
							uptimeY = y
						}
					}
					if width == 72 && (identityY != hit.Y || uptimeY != hit.Y+1) {
						t.Fatalf("wide card separated identity/status or uptime/hint: identity=%d uptime=%d control=%+v", identityY, uptimeY, hit)
					}
					if width == 30 && (identityY < 0 || uptimeY <= identityY || hit.Y <= uptimeY) {
						t.Fatalf("narrow card did not stack identity, uptime and control: identity=%d uptime=%d control=%+v", identityY, uptimeY, hit)
					}
				})
			}
		}
	}
}

func TestCoreCardActionHintOnlyAppearsForAvailableActions(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, tc := range []struct {
			name, state, problem, action, hint string
			active, busy                       bool
		}{
			{name: "running", state: "active", active: true, hint: "stopHint"},
			{name: "stopped", state: "inactive", hint: "startHint"},
			{name: "busy", state: "active", active: true, busy: true},
			{name: "starting", state: "activating"},
			{name: "stopping", state: "deactivating", active: true},
			{name: "restarting", state: "active", active: true, busy: true, action: "restart"},
			{name: "unavailable", state: "active", active: true, problem: "fixture service unavailable"},
			{name: "unknown", state: "unknown"},
		} {
			t.Run(language+"/"+tc.name, func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Busy: tc.busy, CoreAction: tc.action,
					Snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: tc.active, State: tc.state}, ServiceError: tc.problem}}
				block := overviewCoreCard(o, 72)
				plain := ansi.Strip(strings.Join(block.lines, "\n"))
				for _, hint := range []string{"startHint", "stopHint"} {
					if strings.Contains(plain, o.Locale.T("dashboard.core.toggle."+hint)) != (hint == tc.hint) {
						t.Fatalf("action hint does not match availability: %s\n%s", hint, plain)
					}
				}
				if len(block.targets) != 1 || block.targets[0].ID != "core.toggle" || block.targets[0].H != 2 {
					t.Fatalf("availability changed the status focus region: %+v", block.targets)
				}
			})
		}
	}
}

func TestCoreCardHoverOnlyHighlightsAvailableActions(t *testing.T) {
	underline := ansi.NewStyle().Underline(true).UnderlineColor(lipgloss.Color(focus)).String()
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, tc := range []struct {
			name, state, problem, action string
			active, busy, actionable     bool
		}{
			{name: "running", state: "active", active: true, actionable: true},
			{name: "stopped", state: "inactive", actionable: true},
			{name: "busy", state: "active", active: true, busy: true},
			{name: "starting", state: "activating"},
			{name: "stopping", state: "deactivating", active: true},
			{name: "restarting", state: "active", active: true, busy: true, action: "restart"},
			{name: "unavailable", state: "active", active: true, problem: "fixture service unavailable"},
			{name: "unknown", state: "unknown"},
		} {
			t.Run(language+"/"+tc.name, func(t *testing.T) {
				o := dashboardOptions{Locale: i18n.New(language), Busy: tc.busy, CoreAction: tc.action,
					Snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: tc.active, State: tc.state}, ServiceError: tc.problem}}
				idle := overviewCoreCard(o, 72)
				for _, focused := range []bool{false, true} {
					o.Hover, o.Focus = "core.toggle", ""
					if focused {
						o.Hover, o.Focus = "", "core.toggle"
					}
					card := overviewCoreCard(o, 72)
					if len(card.targets) != 1 || card.targets[0] != idle.targets[0] || ansi.Strip(strings.Join(card.lines, "\n")) != ansi.Strip(strings.Join(idle.lines, "\n")) {
						t.Fatal("hover or focus changed status text or its hit region")
					}
					highlighted := strings.Contains(card.lines[card.targets[0].Y], underline)
					if highlighted != (focused || tc.actionable) {
						t.Fatalf("highlighted=%t with keyboard focus=%t and actionable=%t", highlighted, focused, tc.actionable)
					}
				}
			})
		}
	}
}

func TestClickingCoreActionHintStartsOrStops(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, active := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/active=%t", language, active), func(t *testing.T) {
				m, b := coreToggleTestModel(t, active)
				defer m.stopRuntime()
				m.locale = i18n.New(language)
				m.layout()
				hit := findTarget(t, m, "core.toggle")
				if hit.H != 2 {
					t.Fatalf("hint is not part of the status target: %+v", hit)
				}
				action := "start"
				if active {
					action = "stop"
				}
				calls := 0
				b.control = func(_ context.Context, got string) error {
					calls++
					if got != action {
						t.Fatalf("hint requested %q, want %q", got, action)
					}
					return nil
				}
				m, cmd := update(m, tea.MouseClickMsg{X: hit.X + hit.W - 1, Y: hit.Y + 1, Button: tea.MouseLeft})
				if cmd == nil || !m.busy || m.focus != "core.toggle" || calls != 0 {
					t.Fatal("clicking the action hint did not queue the status action")
				}
				actionCmd(t, cmd)()
				if calls != 1 {
					t.Fatalf("hint queued %d service requests, want one", calls)
				}
			})
		}
	}
}
