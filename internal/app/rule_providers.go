package app

import (
	"context"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type RuleProviderSnapshot struct {
	State      string
	Providers  []mihomo.RuleProvider
	ObservedAt time.Time
	Error      string
	Diagnostic error `json:"-"`
}

type ruleProviderCore struct {
	RuntimeSnapshot
	client               mihomo.Client
	identity, generation string
}

func (a *App) inspectRuleProviderCoreLocked(ctx context.Context) (ruleProviderCore, error) {
	var core ruleProviderCore
	var err error
	core.RuntimeSnapshot, core.client, core.identity, err = a.inspectRuntimeLocked(ctx)
	if err == nil && core.State == "ready" {
		_, core.generation, err = a.load()
	}
	return core, err
}

func (before ruleProviderCore) sameCore(current ruleProviderCore) bool {
	return before.identity == current.identity && before.generation == current.generation && before.Service.PID == current.Service.PID
}

func (a *App) RuleProviders(ctx context.Context) (RuleProviderSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var before ruleProviderCore
	err := a.locked(ctx, func() error {
		var err error
		before, err = a.inspectRuleProviderCoreLocked(ctx)
		return err
	})
	s := RuleProviderSnapshot{State: before.State, ObservedAt: before.ObservedAt, Error: before.Error, Diagnostic: before.Diagnostic}
	if err != nil || before.State != "ready" {
		return s, err
	}
	providers, queryErr := before.client.RuleProviders(ctx)
	if ctx.Err() != nil {
		return s, ctx.Err()
	}
	err = a.locked(ctx, func() error {
		current, err := a.inspectRuleProviderCoreLocked(ctx)
		if err != nil {
			return err
		}
		s.State, s.ObservedAt, s.Error, s.Diagnostic = current.State, current.ObservedAt, current.Error, current.Diagnostic
		if current.State != "ready" {
			return nil
		}
		if !before.sameCore(current) {
			queryErr = diagnostic.New("diagnostic.runtime.ruleProvidersChanged", "core changed while loading rule providers; refresh to read its current providers")
		}
		if queryErr != nil {
			s.State, s.Error, s.Diagnostic = "unavailable", queryErr.Error(), queryErr
			return nil
		}
		s.Providers = providers
		return nil
	})
	return s, err
}

// UpdateRuleProvider performs one explicit update, with all HTTP outside the
// application lock so a slow provider cannot prevent stopping the service.
// Recheck after discovery before sending PUT, then reject stale completion.
func (a *App) UpdateRuleProvider(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return diagnostic.New("diagnostic.api.ruleProviderName", "rule provider name must not be empty")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var before ruleProviderCore
	err := a.locked(ctx, func() error {
		var err error
		before, err = a.inspectRuleProviderCoreLocked(ctx)
		return err
	})
	if err != nil {
		return err
	}
	if before.State != "ready" {
		return ruleProviderRuntimeError(before.RuntimeSnapshot)
	}
	providers, err := before.client.RuleProviders(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, provider := range providers {
		if provider.Name != name {
			continue
		}
		if strings.EqualFold(provider.VehicleType, "Inline") {
			return diagnostic.New("diagnostic.api.ruleProviderInline", "inline rule providers have no external source to update")
		}
		found = true
		break
	}
	if !found {
		return diagnostic.New("diagnostic.api.ruleProviderUnavailable", "rule provider is no longer available; refresh the provider list")
	}
	check := func() error {
		current, err := a.inspectRuleProviderCoreLocked(ctx)
		if err != nil {
			return err
		}
		if current.State != "ready" {
			return ruleProviderRuntimeError(current.RuntimeSnapshot)
		}
		if !before.sameCore(current) {
			return diagnostic.New("diagnostic.runtime.ruleProviderChanged", "core changed during the rule provider update; refresh and retry")
		}
		return nil
	}
	if err := a.locked(ctx, check); err != nil {
		return err
	}
	updateErr := before.client.UpdateRuleProvider(ctx, name)
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

func ruleProviderRuntimeError(s RuntimeSnapshot) error {
	if s.State == "stopped" {
		return diagnostic.New("diagnostic.runtime.ruleProviderNotRunning", "Mihomo is not running; start it before updating rule providers")
	}
	return runtimeError(s)
}
