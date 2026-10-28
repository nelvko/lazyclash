package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
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
	ports := []string{m.Controller, fmt.Sprintf("127.0.0.1:%d", m.Port)}
	for _, addr := range ports {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("local port %s is unavailable; no existing service was stopped", addr)
		}
		l.Close()
	}
	return nil
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
		if a.client(m).Ping(ctx) == nil {
			status, err := a.Service.Inspect(ctx)
			if err == nil && status.Active {
				return nil
			}
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
func (a *App) Control(ctx context.Context, action string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return errors.New("unsupported service action")
	}
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
		if action != "stop" && !status.Active {
			if err := a.checkPorts(m); err != nil {
				return err
			}
		}
		if action == "stop" && status.Active {
			// Remember the observed history even if its control API is unavailable;
			// an intentional stop must not bring back first-run setup.
			if err := a.markHasRunLocked(ctx); err != nil {
				return err
			}
		}
		if err := a.Service.Action(ctx, action); err != nil {
			// systemctl can be cancelled after its job was accepted. Restore the
			// stopped state with a fresh context rather than leaving a new daemon.
			if action != "stop" && !status.Active {
				c, cancel := cleanupContext()
				defer cancel()
				return errors.Join(err, a.Service.Action(c, "stop"))
			}
			return err
		}
		if action == "stop" {
			s, err := a.Service.Inspect(ctx)
			if err != nil {
				return err
			}
			if s.Active {
				return errors.New("service is still active")
			}
			return nil
		}
		if err := a.waitReady(ctx, m); err != nil {
			if !status.Active {
				c, cancel := cleanupContext()
				defer cancel()
				return errors.Join(err, a.Service.Action(c, "stop"))
			}
			return err
		}
		return a.markHasRunLocked(ctx)
	})
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
		return a.client(m).Select(ctx, group, node)
	})
}
func (a *App) Port(ctx context.Context) (int, error) {
	var port int
	err := a.locked(ctx, func() error { m, _, err := a.load(); port = m.Port; return err })
	return port, err
}
