package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestOverviewModeArrowsBrowseAndActivationAppliesOnce(t *testing.T) {
	for _, tc := range []struct {
		width, height int
		language      string
		activate      tea.KeyPressMsg
	}{{160, 45, i18n.Chinese, special(tea.KeyEnter)}, {20, 16, i18n.English, special(tea.KeySpace)}} {
		t.Run(fmt.Sprintf("%dx%d", tc.width, tc.height), func(t *testing.T) {
			b := &fakeBackend{snapshot: settingsTestSnapshot()}
			b.snapshot.Service.Active, b.snapshot.Service.State = true, "active"
			b.snapshot.Mode.Actual, b.snapshot.Mode.Running = "rule", true
			var calls []string
			b.mode = func(_ context.Context, mode string) error {
				calls = append(calls, mode)
				b.snapshot.Mode.Desired, b.snapshot.Mode.Actual = mode, mode
				return nil
			}
			m := ready(b)
			directionCleanup(t, &m)
			m.locale, m.width, m.height, m.focus = i18n.New(tc.language), tc.width, tc.height, "mode.rule"
			m.layout()
			for _, step := range []struct {
				key  tea.KeyPressMsg
				want string
			}{
				{special(tea.KeyLeft), "mode.direct"}, {key("l"), "mode.rule"},
				{key("h"), "mode.direct"}, {special(tea.KeyRight), "mode.rule"},
				{special(tea.KeyRight), "mode.global"},
			} {
				m, _ = update(m, step.key)
				if m.focus != step.want || !hasAction(m.targets, step.want) || m.busy || m.snapshot.Mode.Desired != "rule" || len(calls) != 0 {
					t.Fatalf("key %s: focus=%q, want visible %q; saved=%s, calls=%v", step.key, m.focus, step.want, m.snapshot.Mode.Desired, calls)
				}
			}
			if got := m.contextHelp(); got != m.tr("navigation.overview.modeChoice") {
				t.Fatalf("mode focus has unrelated hint %q", got)
			}
			m, _ = update(m, special(tea.KeyTab))
			if m.focus != "mode.direct" {
				t.Fatalf("Tab skipped the next mode, focus=%q", m.focus)
			}
			m, _ = update(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			m, _ = update(m, key("?"))
			m, _ = update(m, special(tea.KeyLeft))
			if !m.helpOpen || m.focus != "mode.global" {
				t.Fatal("Left moved mode focus behind the help overlay")
			}
			m, _ = update(m, special(tea.KeyEscape))
			m, cmd := update(m, tc.activate)
			if !m.busy || cmd == nil || len(calls) != 0 || m.snapshot.Mode.Desired != "rule" {
				t.Fatal("activating must wait for the existing mode action")
			}
			m, _ = update(m, actionCmd(t, cmd)())
			if m.busy || len(calls) != 1 || calls[0] != "global" || m.snapshot.Mode.Desired != "global" {
				t.Fatalf("activation: busy=%t, mode=%s, calls=%v", m.busy, m.snapshot.Mode.Desired, calls)
			}
		})
	}
}

func TestOverviewPagingKeepsFocusVisibleAndEnterDoesNotToggleCore(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {120, 18}, {60, 18}} {
		for _, initial := range []string{"overview.card.dashboard.control.core", "core.toggle"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], initial), func(t *testing.T) {
				m := themeSample("overview-running")
				m.width, m.height, m.focus = size[0], size[1], initial
				m.layout()
				for range 2 {
					m, _ = update(m, special(tea.KeyPgDown))
				}
				g := dashboardGeometry(m.dashboardOptions())
				visible := false
				for _, card := range overviewCardRegionsFromModel(m) {
					if card.ID == m.focus && card.Y < m.scroll+g.height && card.Y+card.H > m.scroll {
						visible = true
					}
				}
				if !visible || !strings.HasPrefix(m.focus, "overview.card.") {
					t.Fatalf("paging left focus outside the visible cards: %s at scroll %d", m.focus, m.scroll)
				}
				m, _ = update(m, special(tea.KeyEnter))
				if m.busy || m.focus == "core.toggle" {
					t.Fatalf("Enter after paging returned to core control: focus=%s busy=%t", m.focus, m.busy)
				}
				m, _ = update(m, special(tea.KeyPgUp))
				if m.busy {
					t.Fatal("paging initiated an operation")
				}
			})
		}
	}
}
