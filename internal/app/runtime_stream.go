package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type RuntimeSource string

const (
	RuntimeStatus      RuntimeSource = "status"
	RuntimeVersion     RuntimeSource = "version"
	RuntimeTraffic     RuntimeSource = "traffic"
	RuntimeMemory      RuntimeSource = "memory"
	RuntimeConnections RuntimeSource = "connections"
	RuntimeLogs        RuntimeSource = "logs"
)

// RuntimeEvent is one independently observed source. A source error makes that
// source unavailable without replacing the other sources. Instance changes
// invalidate all values from the previous owned core; Source identifies which
// payload is present. ObservedAt is the local frame receipt time.
type RuntimeEvent struct {
	Source       RuntimeSource
	Instance     uint64
	Installation string
	Connected    bool
	ObservedAt   time.Time
	Snapshot     RuntimeSnapshot
	Traffic      *mihomo.Traffic
	Memory       *uint64
	Connections  *mihomo.ConnectionSnapshot
	Log          *mihomo.LogEntry
	Err          error
}

type runtimeIdentity struct {
	installation, controller, secret string
	pid                              int
}

type runtimeInspection struct {
	snapshot RuntimeSnapshot
	client   mihomo.Client
	identity runtimeIdentity
}

type runtimeStreams struct {
	cancel context.CancelFunc
	done   <-chan struct{}
}

func (s *runtimeStreams) stop() {
	if s == nil {
		return
	}
	s.cancel()
	<-s.done
}

func runtimeRetryDelay(attempt int) time.Duration {
	return min(30*time.Second, time.Second<<min(5, max(0, attempt)))
}

// StreamRuntime owns one shared WebSocket per realtime endpoint and one service
// watcher. Each source reconnects independently, while an ownership, listener or
// PID change closes and joins every old reader before publishing the new
// instance. Version is cached for that instance rather than requested per frame.
//
// emit is called serially and must return promptly. The bounded queue applies
// backpressure rather than discarding traffic frames needed by chart history.
// Cancelling ctx releases readers, reconnect delays and the service watcher.
func (a *App) StreamRuntime(ctx context.Context, emit func(RuntimeEvent)) error {
	if emit == nil {
		return diagnostic.New("diagnostic.runtime.consumer", "a runtime consumer is required")
	}
	updates := make(chan RuntimeEvent, 64)
	watchCtx, stopWatcher := context.WithCancel(ctx)
	checks := make(chan runtimeInspection, 1)
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for watchCtx.Err() == nil {
			snapshot, client, identity := a.inspectRuntimeSession(watchCtx)
			select {
			case checks <- runtimeInspection{snapshot: snapshot, client: client, identity: identity}:
			case <-watchCtx.Done():
				return
			}
			select {
			case <-ticker.C:
			case <-watchCtx.Done():
				return
			}
		}
	}()
	var streams *runtimeStreams
	defer func() {
		stopWatcher()
		streams.stop()
		<-watcherDone
	}()
	var identity runtimeIdentity
	var snapshot RuntimeSnapshot
	var instance uint64
	first := true
	inspect := func(check runtimeInspection) {
		current, client, currentID := check.snapshot, check.client, check.identity
		changed := first || currentID != identity
		if changed {
			streams.stop()
			streams = nil
			instance++
			identity = currentID
		} else {
			current.RunningVersion = snapshot.RunningVersion
		}
		// Publish each completed inspection as an authority heartbeat. The TUI
		// can recover an inconclusive foreground read without restarting healthy
		// subscriptions or requesting another full application snapshot.
		snapshot = current
		emit(RuntimeEvent{Source: RuntimeStatus, Instance: instance, Installation: identity.installation, ObservedAt: current.ObservedAt, Snapshot: current})
		if changed && current.State == "ready" {
			streams = a.startRuntimeStreams(ctx, client, current, currentID, instance, updates)
		}
		first = false
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case check := <-checks:
			inspect(check)
		case event := <-updates:
			if event.Instance != instance || snapshot.State != "ready" {
				continue
			}
			if event.Source == RuntimeVersion && event.Err == nil {
				snapshot.RunningVersion = event.Snapshot.RunningVersion
			}
			event.Snapshot = snapshot
			event.Installation = identity.installation
			event.Snapshot.ObservedAt = event.ObservedAt
			emit(event)
		}
	}
}

// inspectRuntimeSession is the only periodic service inspection for all four
// readers. No controller response or WebSocket read happens under the lock.
func (a *App) inspectRuntimeSession(ctx context.Context) (RuntimeSnapshot, mihomo.Client, runtimeIdentity) {
	var snapshot RuntimeSnapshot
	var client mihomo.Client
	var installation string
	err := a.locked(ctx, func() error {
		// A foreground operation can legitimately hold the application lock
		// longer than an inspection timeout. Waiting for it does not invalidate
		// the independently flowing subscriptions. Bound only the inspection
		// itself; session cancellation still interrupts the lock wait.
		checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		var err error
		snapshot, client, installation, err = a.inspectRuntimeLocked(checkCtx)
		return err
	})
	if err != nil {
		snapshot = RuntimeSnapshot{State: "unavailable", ObservedAt: time.Now(),
			Error: "Managed service ownership is unavailable; retrying runtime monitoring", Diagnostic: err}
	}
	if snapshot.State != "ready" {
		return snapshot, mihomo.Client{}, runtimeIdentity{}
	}
	return snapshot, client, runtimeIdentity{installation: installation, controller: client.BaseURL, secret: client.Secret, pid: snapshot.Service.PID}
}

// Remember an authenticated running core only after confirming that the owned
// instance is still the one whose version answered. This happens once per
// instance, outside the metric delivery loop.
func (a *App) rememberRuntimeStarted(ctx context.Context, expected runtimeIdentity) error {
	return a.locked(ctx, func() error {
		current, client, installation, err := a.inspectRuntimeLocked(ctx)
		if err != nil {
			return err
		}
		identity := runtimeIdentity{installation: installation, controller: client.BaseURL, secret: client.Secret, pid: current.Service.PID}
		if current.State != "ready" || identity != expected {
			return diagnostic.New("diagnostic.runtime.changed", "Core changed while observing runtime; reconnecting")
		}
		return a.markHasRunLocked(ctx)
	})
}

func (a *App) startRuntimeStreams(ctx context.Context, client mihomo.Client, snapshot RuntimeSnapshot, identity runtimeIdentity, instance uint64, updates chan<- RuntimeEvent) *runtimeStreams {
	streamCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var wg sync.WaitGroup
	send := func(event RuntimeEvent) {
		event.Instance = instance
		event.ObservedAt = time.Now()
		event.Snapshot = snapshot
		select {
		case updates <- event:
		case <-streamCtx.Done():
		}
	}
	start := func(source RuntimeSource, read func(context.Context, mihomo.Client, func(RuntimeEvent)) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			attempt := 0
			for streamCtx.Err() == nil {
				connection := client
				var connectedAt time.Time
				connection.StreamReady = func() {
					connectedAt = time.Now()
					send(RuntimeEvent{Source: source, Connected: true})
				}
				err := read(streamCtx, connection, func(event RuntimeEvent) {
					attempt = 0
					send(event)
				})
				if streamCtx.Err() != nil {
					return
				}
				// A quiet log stream can be healthy without producing records. A
				// short accept/close loop still keeps its exponential backoff.
				if !connectedAt.IsZero() && time.Since(connectedAt) >= 30*time.Second {
					attempt = 0
				}
				if err == nil {
					err = errors.New("Mihomo runtime stream ended; reconnecting")
				}
				send(RuntimeEvent{Source: source, Err: err})
				timer := time.NewTimer(runtimeRetryDelay(attempt))
				attempt = min(5, attempt+1)
				select {
				case <-streamCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}()
	}
	start(RuntimeTraffic, func(ctx context.Context, client mihomo.Client, emit func(RuntimeEvent)) error {
		return client.StreamTraffic(ctx, func(value mihomo.Traffic) { emit(RuntimeEvent{Source: RuntimeTraffic, Traffic: &value}) })
	})
	start(RuntimeMemory, func(ctx context.Context, client mihomo.Client, emit func(RuntimeEvent)) error {
		return client.StreamMemory(ctx, func(value uint64) { emit(RuntimeEvent{Source: RuntimeMemory, Memory: &value}) })
	})
	start(RuntimeConnections, func(ctx context.Context, client mihomo.Client, emit func(RuntimeEvent)) error {
		return client.StreamConnections(ctx, func(value mihomo.ConnectionSnapshot) {
			emit(RuntimeEvent{Source: RuntimeConnections, Connections: &value})
		})
	})
	start(RuntimeLogs, func(ctx context.Context, client mihomo.Client, emit func(RuntimeEvent)) error {
		return client.StreamLogs(ctx, func(value mihomo.LogEntry) { emit(RuntimeEvent{Source: RuntimeLogs, Log: &value}) })
	})
	// This worker retries failed version handshakes, then stays finished for the
	// lifetime of the core instance. It never gates the metric readers.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for attempt := 0; streamCtx.Err() == nil; attempt++ {
			version, err := client.Version(streamCtx)
			if err == nil {
				err = a.rememberRuntimeStarted(streamCtx, identity)
			}
			if streamCtx.Err() != nil {
				return
			}
			event := RuntimeEvent{Source: RuntimeVersion, Instance: instance, ObservedAt: time.Now(), Snapshot: snapshot, Err: err}
			event.Snapshot.RunningVersion = version
			select {
			case updates <- event:
			case <-streamCtx.Done():
				return
			}
			if err == nil {
				return
			}
			timer := time.NewTimer(runtimeRetryDelay(attempt))
			select {
			case <-streamCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	go func() { wg.Wait(); close(done) }()
	return &runtimeStreams{cancel: cancel, done: done}
}
