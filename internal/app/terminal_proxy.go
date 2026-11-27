package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/nelvko/lazyclash/internal/diagnostic"

	"github.com/nelvko/lazyclash/internal/shellintegration"
)

// TerminalProxyPort checks the IPv4 loopback binding and source rules for the
// managed shell endpoint. Clients supply credentials separately when required.
func (s RuntimeSettings) TerminalProxyPort() int {
	if s.ProxyDialHost() != "127.0.0.1" {
		return 0
	}
	loopback := netip.AddrFrom4([4]byte{127, 0, 0, 1})
	contains := func(networks string) bool {
		for _, network := range strings.Fields(networks) {
			if prefix, err := netip.ParsePrefix(network); err == nil && prefix.Contains(loopback) {
				return true
			}
		}
		return false
	}
	if !contains(s.LANAllowedIPs) || contains(s.LANDisallowedIPs) {
		return 0
	}
	return s.MixedPort
}

func terminalLoopbackUnavailable() error {
	return diagnostic.New("diagnostic.settings.terminalLoopback", "the mixed proxy binding or access rules exclude 127.0.0.1")
}

func (a *App) terminalEndpoint(port int) shellintegration.Endpoint {
	hash := sha256.Sum256([]byte(a.Home + "\x00" + a.Name))
	return shellintegration.Endpoint{Namespace: hex.EncodeToString(hash[:]), Host: "127.0.0.1", Port: port}
}

func (a *App) terminalProxyStatus() shellintegration.Status {
	m, err := shellintegration.New()
	if err != nil {
		return shellintegration.Status{State: "unsupported", Detail: err.Error()}
	}
	s, err := m.Current()
	if err != nil {
		s.Detail = err.Error()
		s.State = "error"
		if errors.Is(err, shellintegration.ErrInstallPending) {
			s.State = "incomplete" // Enabling can resume this interrupted installation.
		}
	}
	return s
}

// SetTerminalProxy saves intent for the calling shell. Only that shell's hook
// can apply it or restore the original environment after the TUI returns.
func (a *App) SetTerminalProxy(ctx context.Context, enabled bool) error {
	m, err := shellintegration.New()
	if err != nil {
		return err
	}
	s, err := m.Current()
	// An interrupted startup-file transaction can be resumed by the next enable.
	if err != nil && !(enabled && errors.Is(err, shellintegration.ErrInstallPending)) {
		return err
	}
	if s.State == "unsupported" {
		return fmt.Errorf("terminal proxy: %s", s.Detail)
	}
	if !enabled {
		if err := ctx.Err(); err != nil {
			return err
		}
		return m.Set(false, a.terminalEndpoint(0))
	}
	if !s.Installed && !s.Loaded {
		// Validate before changing startup files, then observe again after the
		// installation: another management process may have changed the port.
		if err := a.locked(ctx, func() error { _, err := a.terminalEndpointLocked(ctx); return err }); err != nil {
			return err
		}
		if err := m.Install(s.Shell); err != nil {
			return err
		}
	}
	return a.locked(ctx, func() error {
		endpoint, err := a.terminalEndpointLocked(ctx)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return m.Set(true, endpoint)
	})
}

func (a *App) terminalEndpointLocked(ctx context.Context) (shellintegration.Endpoint, error) {
	var endpoint shellintegration.Endpoint
	if err := a.requireService(); err != nil {
		return endpoint, err
	}
	manifest, _, err := a.load()
	if err != nil {
		return endpoint, err
	}
	bin, config, data := a.paths()
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return endpoint, err
	}
	status, err := a.Service.Inspect(ctx)
	if err != nil {
		return endpoint, err
	}
	if !status.Active {
		return endpoint, fmt.Errorf("start Mihomo before enabling terminal proxy")
	}
	cfg, err := a.client(manifest).Configuration(ctx)
	if err != nil {
		return endpoint, err
	}
	if cfg.MixedPort == nil || *cfg.MixedPort == 0 {
		return endpoint, fmt.Errorf("Mihomo did not confirm an active mixed proxy port")
	}
	runtime := manifestSettings(manifest)
	runtime.MixedPort = *cfg.MixedPort
	if runtime.TerminalProxyPort() == 0 {
		return endpoint, terminalLoopbackUnavailable()
	}
	endpoint = a.terminalEndpoint(*cfg.MixedPort)
	return endpoint, a.probeProxy(ctx, endpoint.Port)
}

func (a *App) publishTerminalEndpoint(port int) error {
	m, err := shellintegration.New()
	if err != nil {
		return err
	}
	return m.PublishEndpoint(a.terminalEndpoint(port))
}
