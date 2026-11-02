package app

import (
	"context"
	"errors"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"os"
	"sync"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// RuntimeSnapshot is one bounded observation of the owned service. Nil metrics
// mean unavailable, never zero. Totals belong to the current core's counters;
// lazyclash does not integrate rates or persist historical traffic.
type RuntimeSnapshot struct {
	State                 string
	Service               service.Status
	RunningVersion        string
	ObservedAt            time.Time
	Traffic               *mihomo.Traffic
	Connections           *mihomo.ConnectionSnapshot
	Memory                *uint64
	Error                 string
	TrafficError          string
	ConnectionsError      string
	MemoryError           string
	Diagnostic            error `json:"-"`
	TrafficDiagnostic     error `json:"-"`
	ConnectionsDiagnostic error `json:"-"`
	MemoryDiagnostic      error `json:"-"`
}

// inspectRuntimeLocked checks ownership before exposing the local controller.
// The caller holds the application lock; no metric stream is opened here.
func (a *App) inspectRuntimeLocked(ctx context.Context) (RuntimeSnapshot, mihomo.Client, string, error) {
	s := RuntimeSnapshot{State: "uninstalled", ObservedAt: time.Now()}
	m, _, err := a.load()
	if errors.Is(err, os.ErrNotExist) {
		return s, mihomo.Client{}, "", nil
	}
	if err != nil {
		return s, mihomo.Client{}, "", err
	}
	if err := a.requireService(); err != nil {
		s.State, s.Error = "unsupported", err.Error()
		s.Diagnostic = err
		return s, mihomo.Client{}, "", nil
	}
	bin, config, data := a.paths()
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		s.State, s.Error = "unavailable", err.Error()
		s.Diagnostic = err
		return s, mihomo.Client{}, "", nil
	}
	s.Service, err = a.Service.Inspect(ctx)
	if err != nil {
		s.State, s.Error = "unavailable", err.Error()
		s.Diagnostic = err
		return s, mihomo.Client{}, "", nil
	}
	s.State = "stopped"
	if !s.Service.Active {
		return s, mihomo.Client{}, "", nil
	}
	s.State = "ready"
	return s, a.client(m), m.ID, nil
}

func (a *App) Runtime(ctx context.Context) (RuntimeSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var s RuntimeSnapshot
	var client mihomo.Client
	var identity string
	err := a.locked(ctx, func() error {
		var err error
		s, client, identity, err = a.inspectRuntimeLocked(ctx)
		return err
	})
	if err != nil || s.State != "ready" {
		return s, err
	}
	// Each source can fail independently. In particular /memory's first frame
	// is artificial zero, so its client waits for the following real sample.
	// Network sampling must not hold the lock needed to stop/restart the core.
	var wg sync.WaitGroup
	var versionErr error
	wg.Add(4)
	go func() {
		defer wg.Done()
		s.RunningVersion, versionErr = client.Version(ctx)
	}()
	go func() {
		defer wg.Done()
		value, err := client.Traffic(ctx)
		if err != nil {
			s.TrafficError = err.Error()
			s.TrafficDiagnostic = err
		} else {
			s.Traffic = &value
		}
	}()
	go func() {
		defer wg.Done()
		value, err := client.Connections(ctx)
		if err != nil {
			s.ConnectionsError = err.Error()
			s.ConnectionsDiagnostic = err
		} else {
			s.Connections = &value
		}
	}()
	go func() {
		defer wg.Done()
		value, err := client.Memory(ctx)
		if err != nil {
			s.MemoryError = err.Error()
			s.MemoryDiagnostic = err
		} else {
			s.Memory = &value
		}
	}()
	wg.Wait()
	s.ObservedAt = time.Now()
	if ctx.Err() != nil {
		return s, ctx.Err()
	}
	// A stop, restart or ownership change can happen while samples are in flight.
	// Check again under the same lock as service actions before publishing data.
	err = a.locked(ctx, func() error {
		current, _, currentID, err := a.inspectRuntimeLocked(ctx)
		if err != nil {
			return err
		}
		if current.State != "ready" {
			s = current
			return nil
		}
		if currentID != identity || (s.Service.PID > 0 && current.Service.PID > 0 && s.Service.PID != current.Service.PID) {
			s = RuntimeSnapshot{State: "unavailable", Service: current.Service, ObservedAt: time.Now(), Error: "Core changed while sampling; reconnecting on the next refresh"}
			return nil
		}
		s.Service = current.Service
		if versionErr != nil {
			s = RuntimeSnapshot{State: "unavailable", Service: current.Service, ObservedAt: time.Now(), Error: versionErr.Error(), Diagnostic: versionErr}
			return nil
		}
		if s.Traffic == nil && s.Connections == nil && s.Memory == nil {
			s.State, s.Error = "unavailable", "Mihomo runtime data is unavailable; retry or check the controller"
		}
		// This also records starts performed outside lazyclash. Observations never
		// write traffic counters or historical usage to disk.
		return a.markHasRunLocked(ctx)
	})
	return s, err
}

func runtimeError(s RuntimeSnapshot) error {
	if s.Error != "" {
		if s.Diagnostic != nil && s.Diagnostic.Error() == s.Error {
			return s.Diagnostic
		}
		return errors.New(s.Error)
	}
	if s.State == "uninstalled" {
		return diagnostic.New("diagnostic.runtime.notInstalled", "Mihomo is not installed")
	}
	return diagnostic.New("diagnostic.runtime.notRunning", "Mihomo is not running; start it before viewing connections")
}

func (a *App) Connections(ctx context.Context) (mihomo.ConnectionSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var s RuntimeSnapshot
	var client mihomo.Client
	err := a.locked(ctx, func() error {
		var err error
		s, client, _, err = a.inspectRuntimeLocked(ctx)
		return err
	})
	if err != nil {
		return mihomo.ConnectionSnapshot{}, err
	}
	if s.State != "ready" {
		return mihomo.ConnectionSnapshot{}, runtimeError(s)
	}
	return client.Connections(ctx)
}

// CloseConnection serializes the ownership check and exact-ID close against
// local service/configuration changes. There is deliberately no close-all use
// case. A disappearing connection is reported without closing another one.
func (a *App) CloseConnection(ctx context.Context, id string) error {
	if !mihomo.ValidConnectionID(id) {
		return diagnostic.New("diagnostic.runtime.connectionID", "invalid connection ID")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return a.locked(ctx, func() error {
		s, client, _, err := a.inspectRuntimeLocked(ctx)
		if err != nil {
			return err
		}
		if s.State != "ready" {
			return runtimeError(s)
		}
		connections, err := client.Connections(ctx)
		if err != nil {
			return err
		}
		for _, connection := range connections.Connections {
			if connection.ID == id {
				return client.CloseConnection(ctx, id)
			}
		}
		return diagnostic.New("diagnostic.runtime.connectionGone", "connection is no longer active; refresh the list")
	})
}
