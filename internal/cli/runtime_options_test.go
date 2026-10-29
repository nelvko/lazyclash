package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
)

type lifecycleFixture struct {
	action         string
	enabled        bool
	autostartCalls int
	mode           app.ModeStatus
	selectedMode   string
	tun            bool
	tunCalls       int
	removed        int
	err            error
}

func (f *lifecycleFixture) Control(_ context.Context, action string) error {
	f.action = action
	return f.err
}
func (f *lifecycleFixture) SetAutostart(_ context.Context, enabled bool) error {
	f.enabled = enabled
	f.autostartCalls++
	return f.err
}
func (f *lifecycleFixture) GetMode(context.Context) (app.ModeStatus, error) { return f.mode, f.err }
func (f *lifecycleFixture) SetMode(_ context.Context, mode string) error {
	f.selectedMode = mode
	return f.err
}
func (f *lifecycleFixture) SetTUN(_ context.Context, enabled bool) error {
	f.tun = enabled
	f.tunCalls++
	return f.err
}
func (f *lifecycleFixture) RemoveCore(context.Context) error { f.removed++; return f.err }

func TestUninstallUsesSharedRemovalWithoutStoppingOrDisabling(t *testing.T) {
	f := &lifecycleFixture{err: errors.New("stop the core first")}
	cmd := uninstallCommand(func() removalBackend { return f })
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); !errors.Is(err, f.err) || f.removed != 1 || f.action != "" || f.autostartCalls != 0 {
		t.Fatal("uninstall hid preconditions or changed lifecycle automatically", err)
	}
}

func TestServiceCommandsUseSharedLifecycleWithoutImplicitStart(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart", "enable", "disable"} {
		t.Run(action, func(t *testing.T) {
			f := &lifecycleFixture{}
			cmd := serviceCommand(func() serviceBackend { return f })
			cmd.SetArgs([]string{action})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if action == "enable" || action == "disable" {
				if f.action != "" || f.autostartCalls != 1 || f.enabled != (action == "enable") {
					t.Fatal("autostart changed the current process")
				}
			} else if f.action != action || f.autostartCalls != 0 {
				t.Fatal("lifecycle command bypassed the shared use case")
			}
		})
	}
}

func TestModeCommandShowsDesiredAndActualAndPropagatesFailure(t *testing.T) {
	f := &lifecycleFixture{mode: app.ModeStatus{Desired: "rule", Actual: "global", Running: true}}
	cmd := newModeCommand(func() runtimeOptionsBackend { return f })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"rule"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if f.selectedMode != "rule" || !strings.Contains(out.String(), `"desired": "rule"`) || !strings.Contains(out.String(), `"actual": "global"`) {
		t.Fatal("mode command hid desired/actual state")
	}
	f.err = errors.New("apply rejected")
	cmd = newModeCommand(func() runtimeOptionsBackend { return f })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"direct"})
	if err := cmd.Execute(); !errors.Is(err, f.err) {
		t.Fatal("mode failure was hidden", err)
	}
}

func TestTUNCommandRejectsInvalidInputAndSurfacesCapabilityFailure(t *testing.T) {
	f := &lifecycleFixture{err: errors.New("user service cannot enable TUN")}
	for _, value := range []string{"on", "off", "invalid"} {
		cmd := newTUNCommand(func() runtimeOptionsBackend { return f })
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{value})
		before := f.tunCalls
		if err := cmd.Execute(); err == nil {
			t.Fatal("invalid or unsupported TUN request succeeded")
		}
		if value == "invalid" && f.tunCalls != before {
			t.Fatal("invalid TUN value reached the use case")
		}
		if value != "invalid" && (f.tunCalls != before+1 || f.tun != (value == "on")) {
			t.Fatal("TUN command failed to use the shared use case")
		}
	}
}
