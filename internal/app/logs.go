package app

import (
	"context"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// ErrLogsNotReady distinguishes failure to establish the owned API from the
// expected end of a bounded observation window after a successful handshake.
var ErrLogsNotReady = diagnostic.New("diagnostic.logs.notReady", "live logs unavailable")

// StreamLogs observes the current owned core only. No system journal is enabled:
// keeping raw stdout/stderr discarded avoids persisting unredacted credentials.
func (a *App) StreamLogs(ctx context.Context, emit func(mihomo.LogEntry)) error {
	if emit == nil {
		return diagnostic.New("diagnostic.logs.consumer", "a log consumer is required")
	}
	var before RuntimeSnapshot
	var client mihomo.Client
	var identity string
	err := a.locked(ctx, func() error {
		var err error
		before, client, identity, err = a.inspectRuntimeLocked(ctx)
		return err
	})
	if err != nil {
		if ctx.Err() != nil {
			return diagnostic.New("diagnostic.logs.failure", "%w: %w", ErrLogsNotReady, ctx.Err())
		}
		return diagnostic.New("diagnostic.logs.installation", "%w: cannot verify the managed installation", ErrLogsNotReady)
	}
	if before.State != "ready" {
		detail := diagnostic.New("diagnostic.logs.ownership", "Core or managed service ownership is unavailable; check status or doctor")
		switch before.State {
		case "uninstalled":
			detail = diagnostic.New("diagnostic.logs.installFirst", "Mihomo is not installed; install and start it to view live logs")
		case "stopped":
			detail = diagnostic.New("diagnostic.logs.startFirst", "Core is stopped; start it to view live logs")
		case "unsupported":
			detail = diagnostic.New("diagnostic.logs.platform", "Log viewing requires a Linux systemd user service")
		}
		return diagnostic.New("diagnostic.logs.unavailable", "%w: %s", ErrLogsNotReady, detail)
	}
	if _, err := client.Version(ctx); err != nil {
		return diagnostic.New("diagnostic.logs.failure", "%w: %w", ErrLogsNotReady, err)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	check := func() error {
		return a.locked(streamCtx, func() error {
			current, _, currentID, err := a.inspectRuntimeLocked(streamCtx)
			if err != nil || current.State != "ready" {
				return diagnostic.New("diagnostic.logs.stopped", "Core stopped or service ownership became unavailable; reconnect logs")
			}
			if identity != currentID || before.Service.PID != current.Service.PID {
				return diagnostic.New("diagnostic.logs.changed", "Core changed while observing logs; reconnect to the current core")
			}
			return nil
		})
	}
	if err := check(); err != nil {
		return err
	}
	changed := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-ticker.C:
				if err := check(); err != nil {
					if streamCtx.Err() == nil {
						changed <- err
					}
					cancel()
					return
				}
			}
		}
	}()
	err = client.StreamLogs(streamCtx, emit)
	if streamCtx.Err() == nil {
		if checkErr := check(); checkErr != nil {
			err = checkErr
		}
	}
	cancel()
	<-done
	if ctx.Err() != nil {
		return ctx.Err()
	}
	select {
	case ownerErr := <-changed:
		return ownerErr
	default:
		return err
	}
}
