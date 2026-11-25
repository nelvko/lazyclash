package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestNetworkTUNSliderKeepsItsTrackWhenPolicyLabelWraps(t *testing.T) {
	m := ready(hostTUNBackend())
	defer m.stopRuntime()
	m.openNetworkConfig("tun")
	m.colorProfile, m.noColor = colorprofile.TrueColor, false
	m.switchStyle = app.SwitchStyleSlider
	m.ports.focus = "tun.policy"
	label := m.tr("network.tunConfig.policy")
	for _, width := range []int{5, 6, 8, 20, 80} {
		doc, _, _ := m.networkTUNDocument(width)
		hit := findTarget(t, Model{targets: doc.targets}, "tun.policy")
		row := doc.lines[hit.Y]
		wantWidth := min(width, switchSlotWidth)
		if hit.W != wantWidth || hit.H != 1 {
			t.Fatalf("width %d: policy hit = %+v, want %d cells", width, hit, wantWidth)
		}
		face := ansi.Strip(ansi.Cut(row, hit.X, hit.X+hit.W))
		if width < switchSlotWidth {
			if strings.TrimSpace(face) != "OFF" {
				t.Fatalf("width %d: truncated slider lost its state: %q", width, face)
			}
		} else {
			want := renderToggle(boolToggle(false), toggleOptions{Style: app.SwitchStyleSlider, Surface: raised, Focused: true})
			if face != "      " || !strings.Contains(row, want) {
				t.Fatalf("width %d: missing painted slider: %q", width, row)
			}
		}
		if width >= hit.X+hit.W+2+ansi.StringWidth(label) {
			if !strings.HasSuffix(ansi.Strip(row), "  "+label) {
				t.Fatalf("width %d: missing policy label after slider", width)
			}
		} else if ansi.StringWidth(row) != hit.X+hit.W {
			t.Fatalf("width %d: narrow policy row does not end at its switch", width)
		}
	}
}

func TestNetworkTUNDraftRequiresSaveAndHostConfirmation(t *testing.T) {
	b := hostTUNBackend()
	var changes []bool
	b.tun = func(_ context.Context, enabled bool) error {
		changes = append(changes, enabled)
		b.snapshot.TUN = enabled
		return nil
	}
	m := ready(b)
	defer m.stopRuntime()
	m.width, m.height, m.focus = 120, 32, "network.tunConfig"
	open := func() {
		t.Helper()
		m.openNetworkConfig("tun")
		m.layout()
		if !m.ports.open || m.ports.tunEnabled != m.snapshot.TUN {
			t.Fatal("TUN settings did not open with the saved policy")
		}
	}

	open()
	m, _ = click(m, findTarget(t, m, "tun.policy"))
	m, _ = update(m, key("esc"))
	if m.ports.open || m.focus != "network.tunConfig" || len(changes) != 0 {
		t.Fatal("cancelling a TUN edit applied it or lost the originating control")
	}
	open()
	m, _ = click(m, findTarget(t, m, "tun.save"))
	if m.ports.open || m.form != "" || m.busy || len(changes) != 0 {
		t.Fatal("saving the original setting should only dismiss the dialog")
	}
	open()
	m, _ = click(m, findTarget(t, m, "tun.policy"))
	m, _ = click(m, findTarget(t, m, "tun.save"))
	if m.ports.open || m.form != "tun-confirm" || m.tunFocus != "tun.cancel" || m.busy || m.snapshot.TUN {
		t.Fatal("enabling TUN bypassed the confirmation or changed observed state")
	}
	m, _ = update(m, key("esc"))
	if m.focus != "network.tunConfig" || m.form != "" || len(changes) != 0 {
		t.Fatal("cancelling host confirmation applied the policy or lost focus")
	}
	open()
	m, _ = click(m, findTarget(t, m, "tun.policy"))
	m, _ = click(m, findTarget(t, m, "tun.save"))
	m, cmd := click(m, findTarget(t, m, "tun.confirm"))
	m, _ = update(m, inlineOperationMsg(t, cmd))
	if len(changes) != 1 || !changes[0] || !m.snapshot.TUN {
		t.Fatal("confirmed host policy was not applied exactly once")
	}
	open()
	m, _ = click(m, findTarget(t, m, "tun.policy"))
	if m.ports.tunEnabled {
		t.Fatal("clicking the enabled policy did not edit the draft")
	}
	m, cmd = click(m, findTarget(t, m, "tun.save"))
	if !m.busy || !m.snapshot.TUN {
		t.Fatal("disabling did not start the asynchronous TUN update")
	}
	m, _ = update(m, inlineOperationMsg(t, cmd))
	if m.form != "" || m.ports.open || m.focus != "network.tunConfig" || len(changes) != 2 || changes[1] || m.snapshot.TUN {
		t.Fatalf("disabling: form=%q, open=%t, focus=%q, writes=%v, saved=%t", m.form, m.ports.open, m.focus, changes, m.snapshot.TUN)
	}
}

func TestNetworkTUNSaveAfterConcurrentPolicyChange(t *testing.T) {
	for _, edit := range []bool{false, true} {
		m := ready(hostTUNBackend())
		m.openNetworkConfig("tun")
		if edit {
			m.networkTUNActivate("tun.policy")
		}
		m.snapshot.TUN = true
		if cmd := m.networkTUNActivate("tun.save"); cmd != nil || m.busy || m.ports.open || m.form != "" || !m.snapshot.TUN {
			t.Fatalf("saving draft edited=%t after an external change requested a write", edit)
		}
		m.stopRuntime()
	}
}

func TestNetworkTUNShowsObservedStateAndRestrictsEditing(t *testing.T) {
	for _, lang := range []string{i18n.English, i18n.Chinese} {
		t.Run(lang, func(t *testing.T) {
			b := hostTUNBackend()
			m := ready(b)
			defer m.stopRuntime()
			m.locale = i18n.New(lang)
			m.focus = "network.tunConfig"
			m.snapshot.TUN, m.snapshot.Service.Active, m.snapshot.Service.State = true, true, "active"
			actual := false
			m.snapshot.TUNActual = &actual
			m.openNetworkConfig("tun")
			for _, observation := range []struct{ apiError, state string }{
				{"", "dashboard.control.off"}, {"failed to read", "dashboard.status.unknown"},
			} {
				m.snapshot.APIError = observation.apiError
				d, _, _ := m.networkTUNDocument(76)
				plain := ansi.Strip(strings.Join(d.lines, "\n"))
				if !strings.Contains(plain, m.tr("network.tunConfig.runtime")+": "+m.tr(observation.state)) ||
					!strings.Contains(plain, slantedFrames[2]+"  "+m.tr("network.tunConfig.policy")) {
					t.Fatalf("form confused the observed and saved states:\n%s", plain)
				}
			}
			m.snapshot.Capabilities.TUN.Supported = false
			d, buttons, _ := m.networkTUNDocument(76)
			if len(d.targets) != 0 || len(buttons) != 1 || buttons[0].ID != "ports.cancel" {
				t.Fatal("ordinary user service exposed TUN configuration controls")
			}
			m.networkTUNActivate("tun.policy")
			if cmd := m.networkTUNActivate("tun.save"); cmd != nil || !m.ports.open || !m.ports.tunEnabled || m.form != "" || m.busy {
				t.Fatal("direct activation bypassed the host-only scope")
			}
		})
	}
}
