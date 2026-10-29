package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
)

type settingsFixture struct {
	current           app.RuntimeSettings
	active            bool
	proposed          app.RuntimeSettings
	prepared, applied int
	err               error
}

func (f *settingsFixture) GetRuntimeSettings(context.Context) (app.RuntimeSettings, error) {
	return f.current, f.err
}
func (f *settingsFixture) PrepareRuntimeSettings(_ context.Context, value app.RuntimeSettings) (app.RuntimeSettingsPlan, error) {
	f.prepared++
	f.proposed = value
	return app.RuntimeSettingsPlan{Current: f.current, Proposed: value, Active: f.active, Changed: value != f.current, ControllerHost: "127.0.0.1"}, f.err
}
func (f *settingsFixture) ApplyRuntimeSettings(_ context.Context, p app.RuntimeSettingsPlan) (app.RuntimeSettingsResult, error) {
	f.applied++
	return app.RuntimeSettingsResult{Settings: p.Proposed, Changed: p.Changed, Restarted: p.Active && p.Changed}, f.err
}

func TestRuntimeSettingsCLIReviewsBeforeRestarting(t *testing.T) {
	for _, args := range [][]string{
		{"ports", "--mixed-port", "18080", "--dry-run"},
		{"ports", "--mixed-port", "18080"},
		{"ports", "--mixed-port", "18080", "--restart"},
	} {
		f := &settingsFixture{current: app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}, active: true}
		cmd := runtimeSettingsCommand(func() runtimeSettingsBackend { return f })
		var out, diagnostic bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&diagnostic)
		cmd.SetArgs(args)
		err := cmd.Execute()
		ack := args[len(args)-1] == "--restart"
		dry := args[len(args)-1] == "--dry-run"
		if (err == nil) != (ack || dry) || f.applied != map[bool]int{true: 1, false: 0}[ack] {
			t.Fatalf("args=%v applied=%d error=%v", args, f.applied, err)
		}
		if f.proposed.MixedPort != 18080 || f.proposed.ControllerPort != 9097 {
			t.Fatal("partial update reset the other setting")
		}
		if !json.Valid(out.Bytes()) {
			t.Fatalf("review/result was not JSON: %q", out.String())
		}
		if !ack && !dry && !strings.Contains(err.Error(), "--restart") {
			t.Fatal("running-core change did not explain its acknowledgement")
		}
	}
}

func TestRuntimeSettingsCLINoOpAndStoppedRemainThin(t *testing.T) {
	for _, active := range []bool{false, true} {
		f := &settingsFixture{current: app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}, active: active}
		cmd := runtimeSettingsCommand(func() runtimeSettingsBackend { return f })
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"ports", "--mixed-port", "7897"})
		if err := cmd.Execute(); err != nil || f.applied != 1 {
			t.Fatal("no-op required a restart acknowledgement", err)
		}
	}
	f := &settingsFixture{current: app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}}
	cmd := runtimeSettingsCommand(func() runtimeSettingsBackend { return f })
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"ports", "--controller-port", "19090"})
	if err := cmd.Execute(); err != nil || f.applied != 1 || f.proposed.MixedPort != 7897 {
		t.Fatal("stopped settings update was not passed to the shared use case", err)
	}
}

func TestRuntimeSettingsCLIShowAndFailureDoNotApply(t *testing.T) {
	for _, args := range [][]string{nil, {"show"}, {"ports"}, {"ports", "--mixed-port", "18080"}} {
		f := &settingsFixture{current: app.RuntimeSettings{MixedPort: 7897, ControllerPort: 9097}}
		if len(args) > 1 {
			f.err = errors.New("preflight rejected")
		}
		cmd := runtimeSettingsCommand(func() runtimeSettingsBackend { return f })
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		err := cmd.Execute()
		if f.applied != 0 || (err != nil) != (len(args) > 0 && args[0] == "ports") {
			t.Fatalf("args=%v applied=%d error=%v", args, f.applied, err)
		}
	}
}
