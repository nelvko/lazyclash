package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func terminalConfigModel(t *testing.T, backend *fakeBackend) Model {
	t.Helper()
	m := ready(backend)
	m.focus = "network.primaryConfig"
	cmd := m.openNetworkConfig("terminal")
	m, _ = update(m, actionCmd(t, cmd)())
	if !m.ports.open || m.ports.step != "edit" || m.ports.focus != "ports.mixed" {
		t.Fatalf("terminal settings did not open for editing: %s, %s", m.ports.step, m.ports.focus)
	}
	return m
}

func TestTerminalConfigReviewsOnlySharedPort(t *testing.T) {
	m := terminalConfigModel(t, &fakeBackend{snapshot: app.Snapshot{Installed: true}})
	m.ports.mixed.SetValue(m.ports.controller.Value())
	m, _ = update(m, special(tea.KeyEnter))
	if m.busy || m.ports.focus != "ports.mixed" || m.ports.mixed.Err == nil {
		t.Fatal("port conflict did not focus the visible mixed-port input")
	}
	m.ports.mixed.SetValue("18080")
	m, cmd := update(m, special(tea.KeyEnter))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.ports.step != "review" || m.ports.plan.Proposed != (app.RuntimeSettings{ControllerPort: 9097, MixedPort: 18080}) {
		t.Fatalf("wrong port proposal: %+v", m.ports.plan)
	}
	if strings.Contains(ansi.Strip(m.screen), "9097") {
		t.Fatal("terminal settings exposed the controller in its review")
	}
	m, _ = update(m, special(tea.KeyEscape))
	m, _ = update(m, special(tea.KeyEscape))
	if m.ports.open || m.page != "overview" || m.focus != "network.primaryConfig" {
		t.Fatal("cancel did not return to the overview configuration button")
	}
}

func TestTerminalConfigRejectsConcurrentControllerPortChange(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true}}
	b.prepareSettings = func(_ context.Context, proposed app.RuntimeSettings) (app.RuntimeSettingsPlan, error) {
		return app.RuntimeSettingsPlan{Current: app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9200}, Proposed: proposed}, nil
	}
	m := terminalConfigModel(t, b)
	m.ports.mixed.SetValue("18080")
	m, cmd := update(m, special(tea.KeyEnter))
	m, _ = update(m, actionCmd(t, cmd)())
	if m.ports.step != "edit" || m.ports.err == "" || hasAction(m.targets, "ports.apply") {
		t.Fatal("a hidden controller-port change could be submitted")
	}
}

func TestNetworkConfigFloatsCenteredWithVisibleBackground(t *testing.T) {
	for _, kind := range []string{"terminal", "tun"} {
		m := ready(&fakeBackend{snapshot: app.Snapshot{Installed: true}})
		cmd := m.openNetworkConfig(kind)
		if cmd != nil {
			m, _ = update(m, actionCmd(t, cmd)())
		}
		for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {40, 16}} {
			w, h := size[0], size[1]
			m.width, m.height = w, h
			base := strings.TrimSuffix(strings.Repeat(strings.Repeat(".", w)+"\n", h), "\n")
			screen, targets := m.renderPorts(base)
			lines := strings.Split(ansi.Strip(screen), "\n")
			top, bottom, left, right := -1, -1, -1, -1
			for y, line := range lines {
				if before, after, ok := strings.Cut(line, "┌"); ok {
					top, left = y, ansi.StringWidth(before)
					_, padding, found := strings.Cut(after, "┐")
					if !found {
						t.Fatal("missing dialog corner")
					}
					right = ansi.StringWidth(padding)
				}
				if strings.Contains(line, "└") {
					bottom = h - y - 1
				}
			}
			if left < 1 || top < 1 || right-left < 0 || right-left > 1 || bottom-top < 0 || bottom-top > 1 || lines[0] != strings.Repeat(".", w) {
				t.Fatalf("%s in %dx%d has incorrect margins L%d R%d T%d B%d\n%s", kind, w, h, left, right, top, bottom, strings.Join(lines, "\n"))
			}
			if !hasAction(targets, "ports.cancel") {
				t.Fatalf("%s in %dx%d has no footer action", kind, w, h)
			}
			for _, target := range targets {
				if target.X <= left || target.X+target.W > w-right-1 || target.Y <= top || target.Y+target.H > h-bottom-1 {
					t.Fatalf("hit target outside of centered modal: %+v", target)
				}
			}
		}
	}
}
