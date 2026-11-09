package app

import (
	"context"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type ProxyProviderSnapshot struct {
	State      string
	Providers  []mihomo.ProxyProvider
	ObservedAt time.Time
	Error      string
	Diagnostic error `json:"-"`
}

func (a *App) ProxyProviders(ctx context.Context) (ProxyProviderSnapshot, error) {
	s, providers, err := readProviders(a, ctx, mihomo.Client.ProxyProviders, diagnostic.New("diagnostic.runtime.proxyProvidersChanged", "core changed while loading proxy providers; reopen the provider list"))
	return ProxyProviderSnapshot{State: s.State, Providers: providers, ObservedAt: s.ObservedAt, Error: s.Error, Diagnostic: s.Diagnostic}, err
}

// UpdateProxyProvider updates a loaded HTTP or File source without changing the
// saved Profile or controlling the service. Inline/Compatible sources are read only.
func (a *App) UpdateProxyProvider(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return diagnostic.New("diagnostic.api.proxyProviderName", "proxy provider name must not be empty")
	}
	discover := func(client mihomo.Client, ctx context.Context) error {
		providers, err := client.ProxyProviders(ctx)
		if err != nil {
			return err
		}
		for _, provider := range providers {
			if provider.Name != name {
				continue
			}
			if !strings.EqualFold(provider.Type, "Proxy") || (!strings.EqualFold(provider.VehicleType, "HTTP") && !strings.EqualFold(provider.VehicleType, "File")) {
				return diagnostic.New("diagnostic.api.proxyProviderReadOnly", "this proxy provider has no supported external source to update")
			}
			return nil
		}
		return diagnostic.New("diagnostic.api.proxyProviderUnavailable", "proxy provider is no longer available; reopen the provider list")
	}
	return updateProvider(a, ctx, discover, func(client mihomo.Client, ctx context.Context) error { return client.UpdateProxyProvider(ctx, name) }, proxyProviderRuntimeError, diagnostic.New("diagnostic.runtime.proxyProviderChanged", "core changed during the proxy provider update; reopen and retry"))
}

func proxyProviderRuntimeError(s RuntimeSnapshot) error {
	if s.State == "stopped" {
		return diagnostic.New("diagnostic.runtime.proxyProviderNotRunning", "Mihomo is not running; start it before updating proxy providers")
	}
	return runtimeError(s)
}
