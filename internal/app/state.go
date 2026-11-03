package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
)

// DesiredState records explicit user intent. Unknown means an installation
// predates this record; observing a process never invents a user's intent.
type DesiredState struct {
	Known     bool `json:"known"`
	Running   bool `json:"running"`
	Autostart bool `json:"autostart"`
}

type OperationState struct {
	Action    string    `json:"action"`
	State     string    `json:"state"` // pending, complete, failed
	UpdatedAt time.Time `json:"updated_at"`
}

type runtimeRecord struct {
	Installation string         `json:"installation"`
	Service      string         `json:"service"`
	Desired      DesiredState   `json:"desired"`
	Operation    OperationState `json:"operation"`
}

// The independent stop marker survives a damaged manifest or runtime record.
// It authorizes no process action: the exact unit is verified before writing it.
func (a *App) stopIntent() ([]byte, error) {
	path := filepath.Join(a.Home, "stop-intent.json")
	raw, err := store.Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	expected, _ := json.Marshal([]string{a.Name, a.Home})
	if !bytes.Equal(raw, expected) {
		return nil, diagnostic.New("diagnostic.state.stopIntentChanged", "stop intent changed or belongs to another installation path; preserving it")
	}
	return raw, nil
}

func (a *App) rememberStop() error {
	raw, err := a.stopIntent()
	if err != nil || raw != nil {
		return err
	}
	raw, _ = json.Marshal([]string{a.Name, a.Home})
	return store.Replace(filepath.Join(a.Home, "stop-intent.json"), raw, nil, 0600)
}

func (a *App) clearStop() error {
	raw, err := a.stopIntent()
	if err != nil || raw == nil {
		return err
	}
	return store.Remove(filepath.Join(a.Home, "stop-intent.json"), raw)
}

func (a *App) loadRuntimeRecord(m manifest) (runtimeRecord, []byte, error) {
	r := runtimeRecord{Installation: m.ID, Service: a.Name}
	raw, err := store.Read(filepath.Join(a.Home, "runtime-state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return r, nil, nil
	}
	if err != nil {
		return r, nil, err
	}
	if json.Unmarshal(raw, &r) != nil || r.Installation != m.ID || r.Service != a.Name || !r.Desired.Known {
		return runtimeRecord{}, nil, diagnostic.New("diagnostic.state.runtimeInvalid", "runtime state is invalid or belongs to another installation; preserving it")
	}
	switch r.Operation.State {
	case "pending", "complete", "failed":
	default:
		return runtimeRecord{}, nil, diagnostic.New("diagnostic.state.operationInvalid", "runtime operation state is invalid; preserving it")
	}
	return r, raw, nil
}

func (a *App) saveRuntimeRecord(r runtimeRecord, old []byte) ([]byte, error) {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := store.Replace(filepath.Join(a.Home, "runtime-state.json"), raw, old, 0600); err != nil {
		return nil, err
	}
	return raw, nil
}

func (a *App) finishOperation(r runtimeRecord, raw []byte, cause error) error {
	r.Operation.State = "complete"
	if cause != nil {
		r.Operation.State = "failed"
		// Cancellation can happen after systemd accepted the job. Preserve the
		// unknown outcome, and let the next observation show its actual result.
		if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
			r.Operation.State = "pending"
		}
	}
	r.Operation.UpdatedAt = time.Now().UTC()
	_, err := a.saveRuntimeRecord(r, raw)
	return errors.Join(cause, err)
}

// Stop authorization depends on the owned unit, not on whether the next-start
// configuration is valid. Disk corruption or failed rollback must not prevent
// the user from stopping an already running, positively identified service.
func (a *App) stopLocked(ctx context.Context) error {
	bin, config, data := a.paths()
	if err := a.verifyForStop(ctx, bin, config, data); err != nil {
		return err
	}
	markerErr := a.rememberStop()
	m, _, stateErr := a.load()
	var r runtimeRecord
	var raw []byte
	if stateErr == nil {
		var old []byte
		r, old, stateErr = a.loadRuntimeRecord(m)
		if stateErr == nil {
			if !r.Desired.Known {
				s, err := a.Service.Inspect(ctx)
				stateErr = err
				r.Desired.Autostart = s.Enabled
			}
			if stateErr == nil {
				r.Desired.Known, r.Desired.Running = true, false
				r.Operation = OperationState{Action: "stop", State: "pending", UpdatedAt: time.Now().UTC()}
				raw, stateErr = a.saveRuntimeRecord(r, old)
			}
		}
	}
	stopErr := a.Service.Action(ctx, "stop")
	if stopErr == nil {
		s, err := a.Service.Inspect(ctx)
		stopErr = err
		if err == nil && (s.Active || s.PID != 0 || (s.State != "inactive" && s.State != "failed" && s.State != "not-found")) {
			stopErr = diagnostic.New("diagnostic.runtime.stopIncomplete", "service has not fully stopped; inspect status and retry Stop")
		}
	}
	if stateErr != nil || markerErr != nil {
		return errors.Join(diagnostic.New("diagnostic.runtime.stopInspection", "stop was requested, but saved runtime state needs inspection"), stateErr, markerErr, stopErr)
	}
	if stopErr != nil {
		return a.finishOperation(r, raw, stopErr)
	}
	// Saved stop intent suppresses restarting the previous generation. Recovery
	// errors are reported after stopping, preserving their files for inspection.
	return a.finishOperation(r, raw, errors.Join(a.markHasRunLocked(ctx), a.recover(ctx)))
}

// checkPersistent is a Linux integration capability; test
// runtimes can provide just the lifecycle contract. No other platform is
// implemented or silently substituted.
func (a *App) checkPersistent(ctx context.Context) error {
	if s, ok := a.Service.(interface{ CheckPersistent(context.Context) error }); ok {
		return s.CheckPersistent(ctx)
	}
	return nil
}

func runtimeDrift(d DesiredState, s service.Status, apiError string) string {
	if !d.Known {
		return ""
	}
	if d.Running && (!s.Active || apiError != "") {
		return "Running was requested, but the owned service is not ready; inspect the service failure and retry Start"
	}
	if !d.Running && (s.Active || s.PID != 0 || s.State == "activating" || s.State == "deactivating") {
		return "Stopped was requested, but the service is running or changing state; retry Stop"
	}
	if d.Autostart != s.Enabled {
		return "The saved autostart preference differs from systemd; apply Enable or Disable again"
	}
	return ""
}

// SetAutostart changes boot activation independently of the current process.
// It never starts/stops Mihomo and never changes logind's authorization policy.
func (a *App) SetAutostart(ctx context.Context, enabled bool) error {
	return a.locked(ctx, func() error {
		if err := a.requireService(); err != nil {
			return err
		}
		svc, ok := a.Service.(interface {
			SetEnabled(context.Context, bool) error
		})
		if !ok {
			return diagnostic.New("diagnostic.runtime.autostartUnsupported", "this runtime cannot configure autostart")
		}
		m, _, err := a.load()
		if err != nil {
			return err
		}
		bin, config, data := a.paths()
		if err := a.Service.Verify(ctx, bin, config, data); err != nil {
			return err
		}
		if enabled {
			if err := a.checkPersistent(ctx); err != nil {
				return err
			}
		}
		s, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		r, old, err := a.loadRuntimeRecord(m)
		if err != nil {
			return err
		}
		if !r.Desired.Known {
			r.Desired.Running = s.Active
		}
		r.Desired.Known, r.Desired.Autostart = true, enabled
		r.Operation = OperationState{Action: "disable", State: "pending", UpdatedAt: time.Now().UTC()}
		if enabled {
			r.Operation.Action = "enable"
		}
		raw, err := a.saveRuntimeRecord(r, old)
		if err != nil {
			return err
		}
		if err := svc.SetEnabled(ctx, enabled); err != nil {
			return a.finishOperation(r, raw, err)
		}
		s, err = a.Service.Inspect(ctx)
		if err == nil && s.Enabled != enabled {
			err = diagnostic.New("diagnostic.state.autostartUnchanged", "systemd autostart did not change to %t", enabled)
		}
		return a.finishOperation(r, raw, err)
	})
}
