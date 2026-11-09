package app

import (
	"context"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
)

type providerCore struct {
	RuntimeSnapshot
	client               mihomo.Client
	identity, generation string
}

func (a *App) inspectProviderCoreLocked(ctx context.Context) (providerCore, error) {
	var core providerCore
	var err error
	core.RuntimeSnapshot, core.client, core.identity, err = a.inspectRuntimeLocked(ctx)
	if err == nil && core.State == "ready" {
		_, core.generation, err = a.load()
	}
	return core, err
}

func (before providerCore) sameCore(current providerCore) bool {
	return before.identity == current.identity && before.generation == current.generation && before.Service.PID == current.Service.PID
}

// readProviders rechecks ownership, installation generation and PID after HTTP.
// No controller request holds the application lock.
func readProviders[T any](a *App, ctx context.Context, query func(mihomo.Client, context.Context) ([]T, error), changed error) (RuntimeSnapshot, []T, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var before providerCore
	err := a.locked(ctx, func() error {
		var err error
		before, err = a.inspectProviderCoreLocked(ctx)
		return err
	})
	s := before.RuntimeSnapshot
	if err != nil || before.State != "ready" {
		return s, nil, err
	}
	providers, queryErr := query(before.client, ctx)
	if ctx.Err() != nil {
		return s, nil, ctx.Err()
	}
	var result []T
	err = a.locked(ctx, func() error {
		current, err := a.inspectProviderCoreLocked(ctx)
		if err != nil {
			return err
		}
		s = current.RuntimeSnapshot
		if current.State != "ready" {
			return nil
		}
		if !before.sameCore(current) {
			queryErr = changed
		}
		if queryErr != nil {
			s.State, s.Error, s.Diagnostic = "unavailable", queryErr.Error(), queryErr
			return nil
		}
		result = providers
		return nil
	})
	return s, result, err
}

// updateProvider keeps discovery and PUT outside the application lock. A core
// change after discovery prevents PUT; a change during PUT rejects completion.
func updateProvider(a *App, ctx context.Context, discover func(mihomo.Client, context.Context) error, update func(mihomo.Client, context.Context) error, runtimeFailure func(RuntimeSnapshot) error, changed error) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var before providerCore
	err := a.locked(ctx, func() error {
		var err error
		before, err = a.inspectProviderCoreLocked(ctx)
		return err
	})
	if err != nil {
		return err
	}
	if before.State != "ready" {
		return runtimeFailure(before.RuntimeSnapshot)
	}
	if err := discover(before.client, ctx); err != nil {
		return err
	}
	check := func() error {
		current, err := a.inspectProviderCoreLocked(ctx)
		if err != nil {
			return err
		}
		if current.State != "ready" {
			return runtimeFailure(current.RuntimeSnapshot)
		}
		if !before.sameCore(current) {
			return changed
		}
		return nil
	}
	if err := a.locked(ctx, check); err != nil {
		return err
	}
	updateErr := update(before.client, ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return a.locked(ctx, func() error {
		if err := check(); err != nil {
			return err
		}
		return updateErr
	})
}
