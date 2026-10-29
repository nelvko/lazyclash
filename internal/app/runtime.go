package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

func (a *App) requireService() error {
	if a.Service == nil {
		if a.PlatformError != nil {
			return a.PlatformError
		}
		return errors.New("Linux systemd user service unavailable")
	}
	return nil
}

// registerService uses published installation paths. The installation use case
// owns rollback and commits only after this returns; no start or enable occurs.
func (a *App) registerService(ctx context.Context) error {
	if err := a.requireService(); err != nil {
		return err
	}
	bin, config, data := a.paths()
	return a.Service.Install(ctx, bin, config, data)
}

func (a *App) client(m manifest) mihomo.Client {
	return mihomo.Client{BaseURL: "http://" + m.Controller, Secret: m.Secret, HTTP: a.HTTP}
}

func (a *App) checkPorts(m manifest) error {
	if a.CheckPorts != nil {
		return a.CheckPorts(m.Controller, m.Port)
	}
	return checkRuntimePorts(context.Background(), m, manifestSettings(m), false)
}

func (a *App) probeProxy(ctx context.Context, port int) error {
	if a.ProbeProxy != nil {
		return a.ProbeProxy(ctx, port)
	}
	dialer := net.Dialer{Timeout: 200 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return errors.New("Mihomo mixed proxy listener is not ready")
	}
	return conn.Close()
}

func (a *App) waitReady(ctx context.Context, m manifest) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Mihomo API did not become ready: %w", err)
		}
		status, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		if status.State == "failed" {
			return fmt.Errorf("Mihomo service failed (result=%s, exit=%d); inspect status before retrying", status.Result, status.ExitCode)
		}
		if status.Active && a.client(m).Ping(ctx) == nil && a.probeProxy(ctx, m.Port) == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Mihomo API did not become ready: %w", ctx.Err())
		case <-tick.C:
		}
	}
}
func (a *App) restartReady(ctx context.Context, m manifest) error {
	bin, config, data := a.paths()
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return err
	}
	if err := a.Service.Action(ctx, "restart"); err != nil {
		return err
	}
	return a.waitReady(ctx, m)
}

// Control records intent before handing a job to systemd. Once submitted, a
// cancelled UI observation must never turn into an implicit stop request.
func (a *App) Control(ctx context.Context, action string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return errors.New("unsupported service action")
	}
	unlock, err := store.Lock(ctx, a.Home)
	if err != nil {
		return err
	}
	defer unlock()
	if err := a.requireService(); err != nil {
		return err
	}
	if action == "stop" {
		return a.stopLocked(ctx)
	}
	if err := a.recover(ctx); err != nil {
		return err
	}
	m, _, err := a.load()
	if err != nil {
		return err
	}
	bin, config, data := a.paths()
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return err
	}
	status, err := a.Service.Inspect(ctx)
	if err != nil {
		return err
	}
	if err := a.checkPersistent(ctx); err != nil {
		return err
	}
	configBytes, err := store.Read(config)
	if err != nil {
		return err
	}
	if err := a.validateConfig(ctx, m, configBytes); err != nil {
		return err
	}
	status, err = a.Service.Inspect(ctx)
	if err != nil {
		return err
	}
	if status.State == "deactivating" || status.State == "reloading" {
		return errors.New("service is changing state; wait before starting or restarting it")
	}
	if !status.Active && status.State != "activating" {
		if err := a.checkPorts(m); err != nil {
			return err
		}
	}
	r, old, err := a.loadRuntimeRecord(m)
	if err != nil {
		return err
	}
	if !r.Desired.Known {
		r.Desired.Autostart = status.Enabled
	}
	r.Desired.Known, r.Desired.Running = true, true
	r.Operation = OperationState{Action: action, State: "pending", UpdatedAt: time.Now().UTC()}
	raw, err := a.saveRuntimeRecord(r, old)
	if err != nil {
		return err
	}
	if err := a.upgradeService(ctx); err != nil {
		return a.finishOperation(r, raw, err)
	}
	if err := a.clearStop(); err != nil {
		return a.finishOperation(r, raw, err)
	}
	if err := a.Service.Action(ctx, action); err != nil {
		return a.finishOperation(r, raw, err)
	}
	if err := a.waitReady(ctx, m); err != nil {
		return a.finishOperation(r, raw, err)
	}
	return a.finishOperation(r, raw, a.markHasRunLocked(ctx))
}
func (a *App) Select(ctx context.Context, group, node string) error {
	return a.locked(ctx, func() error {
		if err := a.requireService(); err != nil {
			return err
		}
		m, _, err := a.load()
		if err != nil {
			return err
		}
		bin, config, data := a.paths()
		if err := a.Service.Verify(ctx, bin, config, data); err != nil {
			return err
		}
		status, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		if !status.Active {
			return errors.New("start the managed service before selecting a node")
		}
		raw, err := store.Read(config)
		if err != nil {
			return err
		}
		persistent, err := mihomo.ConfigSelectionPersistence(raw)
		if err != nil {
			return err
		}
		if !persistent {
			return errors.New("this legacy configuration disables selection persistence; reapply the current profile before selecting a node")
		}
		return a.client(m).Select(ctx, group, node)
	})
}
func (a *App) Port(ctx context.Context) (int, error) {
	var port int
	err := a.locked(ctx, func() error { m, _, err := a.load(); port = m.Port; return err })
	return port, err
}
