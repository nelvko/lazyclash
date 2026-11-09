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

func (a *App) RuleProviders(ctx context.Context) (RuleProviderSnapshot, error) {
	s, providers, err := readProviders(a, ctx, mihomo.Client.RuleProviders, diagnostic.New("diagnostic.runtime.ruleProvidersChanged", "core changed while loading rule providers; refresh to read its current providers"))
	return RuleProviderSnapshot{State: s.State, Providers: providers, ObservedAt: s.ObservedAt, Error: s.Error, Diagnostic: s.Diagnostic}, err
}

func (a *App) UpdateRuleProvider(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return diagnostic.New("diagnostic.api.ruleProviderName", "rule provider name must not be empty")
	}
	discover := func(client mihomo.Client, ctx context.Context) error {
		providers, err := client.RuleProviders(ctx)
		if err != nil {
			return err
		}
		for _, provider := range providers {
			if provider.Name != name {
				continue
			}
			if strings.EqualFold(provider.VehicleType, "Inline") {
				return diagnostic.New("diagnostic.api.ruleProviderInline", "inline rule providers have no external source to update")
			}
			return nil
		}
		return diagnostic.New("diagnostic.api.ruleProviderUnavailable", "rule provider is no longer available; refresh the provider list")
	}
	return updateProvider(a, ctx, discover, func(client mihomo.Client, ctx context.Context) error { return client.UpdateRuleProvider(ctx, name) }, ruleProviderRuntimeError, diagnostic.New("diagnostic.runtime.ruleProviderChanged", "core changed during the rule provider update; refresh and retry"))
}

func ruleProviderRuntimeError(s RuntimeSnapshot) error {
	if s.State == "stopped" {
		return diagnostic.New("diagnostic.runtime.ruleProviderNotRunning", "Mihomo is not running; start it before updating rule providers")
	}
	return runtimeError(s)
}
