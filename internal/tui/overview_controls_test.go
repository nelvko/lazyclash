package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestOverviewRuntimeControlsWaitForObservedState(t *testing.T) {
	for _, id := range []string{"core.start", "core.stop", "core.restart", "mode.rule", "mode.global", "mode.direct"} {
		t.Run(id, func(t *testing.T) {
			b := &fakeBackend{snapshot: app.Snapshot{
				Installed: true, ConfigReady: true, HasRun: true,
				Service:        service.Status{Active: id != "core.start", State: "active"},
				Mode:           app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
				RunningVersion: "v1.19.33",
				Capabilities:   app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
			}}
			if id == "core.start" {
				b.snapshot.Service.State = "inactive"
			}
			if id == "mode.rule" {
				b.snapshot.Mode.Desired, b.snapshot.Mode.Actual = "direct", "direct"
			}
			calls := 0
			b.control = func(_ context.Context, action string) error {
				calls++
				if action != strings.TrimPrefix(id, "core.") {
					t.Fatalf("unexpected lifecycle action %q for %s", action, id)
				}
				b.snapshot.Service.Active = action != "stop"
				b.snapshot.Service.State = "inactive"
				if b.snapshot.Service.Active {
					b.snapshot.Service.State = "active"
				}
				b.snapshot.RunningVersion = "v1.20.0"
				return nil
			}
			b.mode = func(_ context.Context, mode string) error {
				calls++
				if "mode."+mode != id {
					t.Fatalf("unexpected routing mode %q for %s", mode, id)
				}
				b.snapshot.Mode.Desired, b.snapshot.Mode.Actual = mode, mode
				return nil
			}
			m := ready(b)
			defer m.stopRuntime()
			m.width, m.height = 160, 60
			m.layout()
			beforeService, beforeMode, beforeVersion := m.snapshot.Service, m.snapshot.Mode, m.snapshot.RunningVersion
			m, cmd := click(m, findTarget(t, m, id))
			if calls != 0 || !m.busy || m.snapshot.Service != beforeService || m.snapshot.Mode != beforeMode || m.snapshot.RunningVersion != beforeVersion {
				t.Fatal("overview control ran inline or changed observed state optimistically")
			}
			result := actionCmd(t, cmd)()
			if calls != 1 || m.snapshot.Service != beforeService || m.snapshot.Mode != beforeMode || m.snapshot.RunningVersion != beforeVersion {
				t.Fatal("backend result changed the UI before its refreshed snapshot was received")
			}
			m, _ = update(m, result)
			if m.busy || m.snapshot.Service != b.snapshot.Service || m.snapshot.Mode != b.snapshot.Mode || m.snapshot.RunningVersion != b.snapshot.RunningVersion {
				t.Fatal("overview control did not use the refreshed application snapshot")
			}
		})
	}
}

func TestOverviewModeKeepsObservedStateWhenRefreshFails(t *testing.T) {
	b := &fakeBackend{snapshot: app.Snapshot{Installed: true, Service: service.Status{Active: true, State: "active"}, Mode: app.ModeStatus{Desired: "rule", Actual: "rule", Running: true}}}
	b.mode = func(_ context.Context, mode string) error {
		b.snapshot.Mode.Desired, b.snapshot.Mode.Actual = mode, mode
		b.snapshotErr = errors.New("status refresh unavailable")
		return nil
	}
	m := ready(b)
	defer m.stopRuntime()
	if cmd := m.activate("mode.invalid"); cmd != nil {
		t.Fatal("invalid routing mode started an application operation")
	}
	cmd := m.activate("mode.global")
	m, _ = update(m, actionCmd(t, cmd)())
	if m.busy || m.snapshot.Mode.Desired != "rule" || m.snapshot.Mode.Actual != "rule" || !strings.Contains(m.failure, "status refresh unavailable") {
		t.Fatal("failed observation produced an optimistic routing mode")
	}
}

func TestOverviewControlStatesRemainExplicit(t *testing.T) {
	o := dashboardOptions{Snapshot: app.Snapshot{
		Installed: true, Service: service.Status{Active: true, State: "active"},
		Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
		Mode:         app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
	}}
	for _, index := range []int{1, 2} {
		block := overviewControlCard(o, index, 56)
		plain := ansi.Strip(strings.Join(block.lines, "\n"))
		if len(block.targets) != 0 || !strings.Contains(plain, "Unavailable") {
			t.Fatal("unsupported TUN or system proxy control appeared actionable")
		}
		if index == 2 && (!strings.Contains(plain, "Unmanaged") || strings.Contains(plain, "[ Off")) {
			t.Fatal("unmanaged system proxy was presented as switched off")
		}
	}
	o.Busy = true
	for _, block := range []dashboardBlock{overviewControlCard(o, 0, 56), overviewModeCard(o, 56)} {
		if len(block.targets) != 0 {
			t.Fatal("busy runtime control retained an actionable target")
		}
	}
	o.Busy = false
	for _, mode := range []app.ModeStatus{
		{Desired: "rule", Running: true},
		{Desired: "rule", Actual: "global", Running: true, Error: "Mode observation unavailable"},
	} {
		o.Snapshot.Mode = mode
		plain := ansi.Strip(strings.Join(overviewModeCard(o, 56).lines, "\n"))
		if !strings.Contains(plain, "Saved mode: Rule") || !strings.Contains(plain, "Running mode: Unknown") || strings.Contains(plain, "Running mode: Rule") || strings.Contains(plain, "Running mode: Global") {
			t.Fatal("saved routing preference was presented as an observed running mode")
		}
	}
}

func TestRuntimeControlGuardsPreserveRecoveryStop(t *testing.T) {
	for _, state := range []struct {
		name, serviceState, serviceError string
		busy, quitting, uninstalled      bool
		unsupported, recovery            bool
	}{
		{name: "busy", busy: true},
		{name: "quitting", quitting: true},
		{name: "uninstalled", uninstalled: true},
		{name: "unsupported service", serviceState: "unsupported"},
		{name: "unsupported capability", unsupported: true},
		{name: "service observation failed", serviceError: "user manager unavailable", recovery: true},
		{name: "starting", serviceState: "activating", recovery: true},
		{name: "stopping", serviceState: "deactivating", recovery: true},
		{name: "reloading", serviceState: "reloading", recovery: true},
	} {
		for _, id := range []string{"core.start", "core.stop", "core.restart", "mode.global", "mode.invalid"} {
			t.Run(state.name+"/"+id, func(t *testing.T) {
				calls := 0
				b := &fakeBackend{snapshot: app.Snapshot{Installed: !state.uninstalled, Service: service.Status{Active: true, State: state.serviceState}, ServiceError: state.serviceError}}
				b.snapshot.Capabilities = app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: !state.unsupported}}
				b.control = func(context.Context, string) error { calls++; return nil }
				b.mode = func(context.Context, string) error { calls++; return nil }
				m := ready(b)
				defer m.stopRuntime()
				m.busy, m.quitting = state.busy, state.quitting
				if state.busy {
					m.opCtx, m.cancel = context.WithCancel(context.Background())
					defer m.cancel()
				}
				m.job, m.message = "existing operation", "Working"
				beforeID := m.id
				cmd := m.activate(id)
				if state.recovery && id == "core.stop" {
					if cmd == nil {
						t.Fatal("recovery Stop was disabled by a service error or transition")
					}
					actionCmd(t, cmd)()
					if calls != 1 {
						t.Fatal("recovery Stop did not reach the application")
					}
					return
				}
				if cmd != nil || calls != 0 || m.id != beforeID {
					t.Fatal("blocked control started a new application operation")
				}
				if state.busy && (!m.busy || m.job != "existing operation" || m.message != "Working" || m.opCtx.Err() != nil) {
					t.Fatal("blocked control disturbed the current operation")
				}
			})
		}
	}
}
