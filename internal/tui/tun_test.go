package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func hostTUNBackend() *fakeBackend {
	return &fakeBackend{snapshot: app.Snapshot{Installed: true, ConfigReady: true, HasRun: true,
		Service:      service.Status{Scope: service.System, State: "inactive"},
		Capabilities: app.Capabilities{Scope: "system", Runtime: app.Capability{Supported: true}, TUN: app.Capability{Supported: true}},
	}}
}

func TestHostTUNRequiresExplicitConfirmationAndRefreshesObservedState(t *testing.T) {
	b := hostTUNBackend()
	calls := 0
	b.tun = func(_ context.Context, enabled bool) error {
		calls++
		b.snapshot.TUN = enabled
		return nil
	}
	m := ready(b)
	defer m.stopRuntime()
	m.width, m.height = 120, 32
	if cmd := m.activate("tun.enable"); cmd != nil || m.form != "tun-confirm" || m.tunFocus != "tun.cancel" || calls != 0 || m.busy {
		t.Fatal("opening host TUN did not wait at the default cancellation")
	}
	m, _ = update(m, key("enter"))
	if m.form != "" || calls != 0 {
		t.Fatal("default confirmation accepted host TUN")
	}
	m.activate("tun.enable")
	m.layout()
	plain := ansi.Strip(m.screen)
	if !strings.Contains(plain, "entire host") || !strings.Contains(plain, "other users and services") {
		t.Fatal("host traffic scope was omitted from confirmation")
	}
	m, _ = update(m, key("q"))
	if m.form != "tun-confirm" || m.quitting || calls != 0 {
		t.Fatal("printable key escaped the confirmation")
	}
	m, _ = update(m, key("tab"))
	m, cmd := update(m, key("enter"))
	if m.form != "" || !m.busy || calls != 0 || m.snapshot.TUN {
		t.Fatal("confirmed TUN executed inline or changed observed state optimistically")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if calls != 1 || m.busy || !m.snapshot.TUN {
		t.Fatal("host TUN did not use the shared operation and refreshed snapshot")
	}
	cmd = m.activate("tun.disable")
	if !m.busy || calls != 1 || !m.snapshot.TUN {
		t.Fatal("TUN disable did not remain asynchronous")
	}
	m, _ = update(m, actionCmd(t, cmd)())
	if calls != 2 || m.busy || m.snapshot.TUN {
		t.Fatal("TUN disable did not observe its saved state")
	}
}

func TestHostTUNConfirmationOwnsMouseAndFitsBothLanguages(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, size := range [][2]int{{160, 45}, {80, 24}, {60, 20}, {120, 18}, {20, 7}} {
			t.Run(fmt.Sprintf("%s/%dx%d", language, size[0], size[1]), func(t *testing.T) {
				b := hostTUNBackend()
				calls := 0
				b.tun = func(context.Context, bool) error { calls++; return nil }
				m := ready(b)
				defer m.stopRuntime()
				m.locale = i18n.New(language)
				m.width, m.height = size[0], size[1]
				m.activate("tun.enable")
				m.layout()
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
				for _, target := range m.targets {
					if target.ID != "tun.cancel" && target.ID != "tun.confirm" {
						t.Fatalf("background target %q survived host confirmation", target.ID)
					}
				}
				m, _ = update(m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
				if m.form != "tun-confirm" || calls != 0 {
					t.Fatal("background click escaped host confirmation")
				}
				if size[0] < 24 {
					m, _ = update(m, key("tab"))
					m, _ = update(m, key("enter"))
					if calls != 0 || m.busy || m.form != "tun-confirm" {
						t.Fatal("unreadable confirmation accepted host TUN")
					}
				}
				m, _ = update(m, key("esc"))
				if m.form != "" || calls != 0 {
					t.Fatal("Escape did not cancel host TUN")
				}
			})
		}
	}
}

func TestSystemEnvironmentShowsScopeAndHostTUNWhileUserControlsStayUnavailable(t *testing.T) {
	b := hostTUNBackend()
	o := dashboardOptions{Snapshot: b.snapshot}
	block := overviewSystemCard(o, 64)
	plain := ansi.Strip(strings.Join(block.lines, "\n"))
	if !strings.Contains(plain, "system (root instance)") || !strings.Contains(plain, "systemd system") || !strings.Contains(plain, "Entire host") || len(block.targets) != 1 || block.targets[0].ID != "tun.enable" {
		t.Fatal("system environment omitted its scope or TUN control")
	}
	b.snapshot.Capabilities = app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}}
	m := ready(b)
	if cmd := m.activate("tun.enable"); cmd != nil || m.form != "" || m.busy || m.failure == "" {
		t.Fatal("user TUN opened privileged controls")
	}
}

func TestHostTUNRechecksAvailabilityAtConfirmation(t *testing.T) {
	for _, changed := range []string{"scope", "service"} {
		t.Run(changed, func(t *testing.T) {
			b := hostTUNBackend()
			calls := 0
			b.tun = func(context.Context, bool) error { calls++; return nil }
			m := ready(b)
			defer m.stopRuntime()
			m.width, m.height = 120, 32
			m.activate("tun.enable")
			if changed == "scope" {
				m.snapshot.Capabilities.TUN.Supported = false
			} else {
				m.snapshot.Service.State = "activating"
			}
			m, _ = update(m, key("tab"))
			m, _ = update(m, key("enter"))
			if calls != 0 || m.busy || m.failure == "" || m.form != "" {
				t.Fatal("stale host confirmation bypassed the current service boundary")
			}
		})
	}
}
