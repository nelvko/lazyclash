package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

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
				for _, width := range []int{28, 60} {
					d, _, _ := m.networkTUNDocument(width)
					raw := ansi.Strip(strings.Join(d.lines, "\n"))
					if !strings.Contains(ansi.Strip(d.lines[0]), m.tr(observation.state)) ||
						!strings.Contains(ansi.Strip(d.lines[0]), m.tr("network.tunConfig.runtime")) {
						t.Fatalf("form omitted the observed state:\n%s", raw)
					}
					if len(d.targets) != 1 || d.targets[0].ID != "tun.policy" {
						t.Fatalf("expected one editable startup policy: %+v", d.targets)
					}
					hit := d.targets[0]
					face := ansi.Strip(cellSlice(d.lines[hit.Y], hit.X, hit.X+hit.W))
					if !strings.Contains(face, "[x] "+m.tr("network.tunConfig.enable")) ||
						!strings.Contains(ansi.Strip(d.lines[hit.Y]), m.tr("network.tunConfig.policy")) ||
						strings.Contains(raw, "…") {
						t.Fatalf("%d-cell TUN form lost its startup control or cropped a value:\n%s", width, raw)
					}
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
