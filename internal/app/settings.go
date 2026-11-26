package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

// RuntimeSettings owns the listeners independently of the applied profile.
type RuntimeSettings struct {
	mihomo.ListenerOptions
	secretRevision string // Detect concurrent rotations without exposing credentials.
	ControllerHost string `json:"controller_host,omitempty"` // Empty selects 127.0.0.1.
	NewSecret      string `json:"-"`                         // Write-only. Empty preserves the existing credential.
	MixedPort      int    `json:"mixed_port"`
	ControllerPort int    `json:"controller_port"`
	ProxyUserCount int    `json:"proxy_user_count,omitempty"`
}

type RuntimeSettingsPlan struct {
	Current        RuntimeSettings `json:"current"`
	Proposed       RuntimeSettings `json:"proposed"`
	ControllerHost string          `json:"controller_host"`
	Active         bool            `json:"active"`
	Changed        bool            `json:"changed"`
	AppliedProfile string          `json:"applied_profile"`
	SecretChanged  bool            `json:"secret_changed"`
	replacement    func() string
	proxyAuth      *proxyAuthEdit
	guard, seal    string
}

type RuntimeSettingsResult struct {
	Settings   RuntimeSettings `json:"settings"`
	Controller string          `json:"controller"`
	Restarted  bool            `json:"restarted"`
	Changed    bool            `json:"changed"`
}

func manifestSettings(m manifest) RuntimeSettings {
	host, port, _ := net.SplitHostPort(m.Controller)
	if host == "127.0.0.1" {
		host = ""
	}
	controllerPort, _ := strconv.Atoi(port)
	return RuntimeSettings{secretRevision: m.SecretRevision, ListenerOptions: m.Listeners, ControllerHost: host, MixedPort: m.Port, ControllerPort: controllerPort, ProxyUserCount: len(m.proxyUsers)}
}

func (a *App) GetRuntimeSettings(ctx context.Context) (RuntimeSettings, error) {
	var settings RuntimeSettings
	err := a.locked(ctx, func() error {
		m, _, err := a.load()
		if err != nil {
			return err
		}
		settings = manifestSettings(m)
		return nil
	})
	return settings, err
}

func (s RuntimeSettings) controller() string {
	host := s.ControllerHost
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(s.ControllerPort))
}

// Only the reviewed plan retains a write-only replacement. Ordinary formatting
// of the public plan and its JSON representation cannot reveal it.
func (p RuntimeSettingsPlan) replacementSecret() string {
	if p.replacement == nil {
		return ""
	}
	return p.replacement()
}

func runtimeSettingsSeal(plan RuntimeSettingsPlan) string {
	raw, _ := json.Marshal(plan)
	return digest(append(raw, []byte(plan.guard+digest([]byte(plan.replacementSecret())))...))
}

func (a *App) PrepareRuntimeSettings(ctx context.Context, proposed RuntimeSettings) (RuntimeSettingsPlan, error) {
	return a.prepareRuntimeSettings(ctx, nil, proposed, nil)
}

// PrepareRuntimeSettingsEdit rejects drafts based on superseded listener settings.
func (a *App) PrepareRuntimeSettingsEdit(ctx context.Context, original, proposed RuntimeSettings) (RuntimeSettingsPlan, error) {
	return a.prepareRuntimeSettings(ctx, &original, proposed, nil)
}

func (a *App) prepareRuntimeSettings(ctx context.Context, original *RuntimeSettings, proposed RuntimeSettings, auth *proxyAuthEdit) (RuntimeSettingsPlan, error) {
	var plan RuntimeSettingsPlan
	err := a.locked(ctx, func() error {
		var err error
		plan, _, _, _, err = a.prepareRuntimeSettingsLocked(ctx, original, proposed, auth)
		return err
	})
	return plan, err
}

func (a *App) prepareRuntimeSettingsLocked(ctx context.Context, original *RuntimeSettings, proposed RuntimeSettings, auth *proxyAuthEdit) (RuntimeSettingsPlan, manifest, string, []byte, error) {
	var plan RuntimeSettingsPlan

	if err := a.requireService(); err != nil {
		return plan, manifest{}, "", nil, err
	}
	m, target, err := a.load()
	if err != nil {
		return plan, m, target, nil, err
	}
	if original != nil && *original != manifestSettings(m) {
		return plan, m, target, nil, diagnostic.New("diagnostic.settings.editChanged", "listener settings changed while editing; reopen the editor")
	}
	users := m.managedProxyUsers()
	authChanged := false
	if auth != nil {
		proposed = manifestSettings(m)
		users, err = auth.resolve(m.proxyUsers, target)
		if err != nil {
			return plan, m, target, nil, err
		}
		authChanged = !m.ManagedProxyAuth || !slices.Equal(users, m.proxyUsers)
	}
	proposed.ProxyUserCount = len(m.proxyUsers)
	proposed.preserveHiddenPorts(m.Listeners)
	bin, config, data := a.paths()
	if err := a.Service.Verify(ctx, bin, config, data); err != nil {
		return plan, m, target, nil, err
	}
	status, err := a.Service.Inspect(ctx)
	if err != nil {
		return plan, m, target, nil, err
	}
	if status.State == "activating" || status.State == "deactivating" || status.State == "reloading" {
		return plan, m, target, nil, diagnostic.New("diagnostic.settings.serviceChanging", "service is changing state; wait and review the port settings again")
	}
	controller, err := mihomo.ResolveController(ctx, proposed.controller())
	if err != nil {
		return plan, m, target, nil, err
	}
	host, _, _ := net.SplitHostPort(controller)
	proposed.ControllerHost = host
	if host == "127.0.0.1" {
		proposed.ControllerHost = ""
	}
	if proposed.HTTPSAddress != "" {
		proposed.HTTPSAddress, err = mihomo.ResolveController(ctx, proposed.HTTPSAddress)
		if err != nil {
			return plan, m, target, nil, err
		}
	}
	secret := m.Secret
	if proposed.NewSecret != "" {
		secret = proposed.NewSecret
	}
	proposed.NewSecret = ""
	proposed.secretRevision = m.SecretRevision
	plan = RuntimeSettingsPlan{Current: manifestSettings(m), Proposed: proposed, ControllerHost: host, Active: status.Active, AppliedProfile: m.selectedProfile(), SecretChanged: secret != m.Secret}
	if plan.SecretChanged {
		plan.replacement = func() string { return secret }
	}
	plan.proxyAuth = auth
	plan.Changed = plan.Current != plan.Proposed || plan.SecretChanged || authChanged
	// Read the actual applied generation, not the saved source: a raw profile
	// may have been edited since the user last applied it.
	raw, err := store.Read(config)
	if err != nil {
		return plan, m, target, nil, err
	}
	effective, err := mihomo.EffectiveListeners(raw, controller, secret, proposed.MixedPort, proposed.ListenerOptions, users)
	if err != nil {
		return plan, m, target, nil, err
	}
	if users == nil && proposed.AllowLAN && !m.Listeners.AllowLAN {
		// A loopback generation omits inherited proxy accounts. Restore them
		// from the applied immutable recipe, even if a newer download exists.
		next := m
		next.Controller, next.Secret, next.Port, next.Listeners = controller, secret, proposed.MixedPort, proposed.ListenerOptions
		effective, err = a.buildAppliedRecipe(next)
		if err != nil {
			return plan, m, target, nil, err
		}
	} else if m.TUN {
		effective, err = mihomo.WithTUN(effective, true, a.Name)
		if err != nil {
			return plan, m, target, nil, err
		}
	}
	switch {
	case users != nil:
		plan.Proposed.ProxyUserCount = len(users)
	case proposed.AllowLAN != m.Listeners.AllowLAN:
		effectiveUsers, err := mihomo.ConfigProxyUsers(effective)
		if err != nil {
			return plan, m, target, nil, err
		}
		plan.Proposed.ProxyUserCount = len(effectiveUsers)
	}
	if plan.Changed {
		if err := a.checkListenerFiles(ctx, m, proposed); err != nil {
			return plan, m, target, nil, err
		}
		if err := checkRuntimePorts(ctx, m, proposed, status.Active); err != nil {
			return plan, m, target, nil, err
		}
		if err := a.validateConfig(ctx, m, effective); err != nil {
			return plan, m, target, nil, err
		}
	}
	guard, err := a.generationGuard(target)
	if err != nil {
		return plan, m, target, nil, err
	}
	identity, _ := json.Marshal([]any{a.Home, a.Name, target, guard, m.BinarySHA, status.Active, status.State, status.PID})
	plan.guard = digest(identity)
	plan.seal = runtimeSettingsSeal(plan)
	return plan, m, target, effective, ctx.Err()
}

// Preflight leaves sockets owned by the running service alone. Apply repeats
// the full check after stopping, including when widening a loopback binding.
func checkRuntimePorts(ctx context.Context, current manifest, next RuntimeSettings, active bool) error {
	owned := current.Listeners.Endpoints(current.Controller, current.Port)
	for _, endpoint := range next.Endpoints(next.controller(), next.MixedPort) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if active && slices.ContainsFunc(owned, endpoint.Conflicts) {
			continue
		}
		var closer io.Closer
		var err error
		if endpoint.Network == "udp" {
			closer, err = net.ListenPacket("udp", endpoint.Address)
		} else {
			closer, err = net.Listen("tcp", endpoint.Address)
		}
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				return diagnostic.New("diagnostic.settings.portPermission", "permission denied binding local %s listener %s", endpoint.Network, endpoint.Address)
			}
			return diagnostic.New("diagnostic.settings.portUnavailable", "local %s port %s is unavailable; no other service was stopped", endpoint.Network, endpoint.Address)
		}
		if err := closer.Close(); err != nil {
			return diagnostic.New("diagnostic.settings.probeCleanup", "port check could not release its temporary socket")
		}
	}
	return ctx.Err()
}

// ApplyRuntimeSettings accepts only a reviewed, unchanged plan. The persistent
// generation pointer and manifest move together; the existing apply journal
// restores the previous ports and prior running state after errors or crashes.
func (a *App) ApplyRuntimeSettings(ctx context.Context, plan RuntimeSettingsPlan) (RuntimeSettingsResult, error) {
	var result RuntimeSettingsResult
	err := a.locked(ctx, func() error {
		if plan.Current.NewSecret != "" || plan.Proposed.NewSecret != "" || plan.seal == "" || plan.seal != runtimeSettingsSeal(plan) {
			return diagnostic.New("diagnostic.settings.planChanged", "port settings plan changed; review the settings again")
		}
		proposed := plan.Proposed
		proposed.NewSecret = plan.replacementSecret()
		fresh, m, old, config, err := a.prepareRuntimeSettingsLocked(ctx, nil, proposed, plan.proxyAuth)
		if err != nil {
			return err
		}
		if fresh.guard != plan.guard {
			return diagnostic.New("diagnostic.settings.reviewChanged", "installation or service changed since review; review the port settings again")
		}
		controller := net.JoinHostPort(fresh.ControllerHost, strconv.Itoa(fresh.Proposed.ControllerPort))
		if !fresh.Changed {
			result = RuntimeSettingsResult{Settings: fresh.Current, Controller: controller}
			return nil
		}
		oldManifest := m
		m.Controller, m.Port, m.Listeners = controller, fresh.Proposed.MixedPort, fresh.Proposed.ListenerOptions
		if fresh.proxyAuth != nil {
			m.ManagedProxyAuth = true
		}
		if fresh.SecretChanged {
			m.Secret = fresh.replacementSecret()
			m.SecretRevision = randomID()
			fresh.Proposed.secretRevision = m.SecretRevision
		}
		base := filepath.Join(a.Home, "installation")
		next, err := a.writeGeneration(base, m, config)
		if err != nil {
			return err
		}
		keep := false
		defer func() {
			if !keep {
				_ = os.RemoveAll(filepath.Join(base, next))
			}
		}()
		oldGuard, err := a.generationGuard(old)
		if err != nil {
			return err
		}
		newGuard, err := a.generationGuard(next)
		if err != nil {
			return err
		}
		// Validation may be slow; inspect again immediately before authorizing
		// the stop. A different running process needs a new user review.
		status, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		identity, _ := json.Marshal([]any{a.Home, a.Name, old, oldGuard, m.BinarySHA, status.Active, status.State, status.PID})
		if digest(identity) != fresh.guard {
			return diagnostic.New("diagnostic.settings.checkChanged", "service changed while checking the settings; review them again")
		}
		bin, activeConfig, data := a.paths()
		if err := a.Service.Verify(ctx, bin, activeConfig, data); err != nil {
			return err
		}
		p := pending{Old: old, New: next, Active: fresh.Active, OldGuard: oldGuard, NewGuard: newGuard}
		raw, _ := json.Marshal(p)
		journal := filepath.Join(a.Home, "apply-pending.json")
		if err := store.Replace(journal, raw, nil, 0600); err != nil {
			return err
		}
		rollback := func(cause error) error {
			cleanup, cancel := cleanupContext()
			defer cancel()
			recoveryErr := a.recover(cleanup)
			keep = recoveryErr != nil
			return errors.Join(cause, recoveryErr)
		}
		if fresh.Active {
			if err := a.Service.Action(ctx, "stop"); err != nil {
				return rollback(err)
			}
			stopped, err := a.Service.Inspect(ctx)
			if err != nil || stopped.Active {
				return rollback(errors.Join(diagnostic.New("diagnostic.settings.stopRequired", "managed service did not stop before changing ports"), err))
			}
		}
		if err := a.checkListenerFiles(ctx, oldManifest, fresh.Proposed); err != nil {
			return rollback(err)
		}
		if err := checkRuntimePorts(ctx, m, fresh.Proposed, false); err != nil {
			return rollback(err)
		}
		if err := store.Switch(filepath.Join(base, "current"), next, old); err != nil {
			return rollback(err)
		}
		if fresh.Active {
			if err := a.restartReady(ctx, m); err != nil {
				return rollback(err)
			}
			if err := a.client(m).ConfirmProxyPorts(ctx, changedProxyPorts(oldManifest, m)); err != nil {
				return rollback(err)
			}
		}
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		if err := os.Remove(journal); err != nil {
			return rollback(err)
		}
		keep = true
		result = RuntimeSettingsResult{Settings: fresh.Proposed, Controller: controller, Restarted: fresh.Active, Changed: true}
		if publishErr := a.publishTerminalEndpoint(result.Settings.MixedPort); publishErr != nil {
			return diagnostic.New("diagnostic.settings.endpointPublish", "ports were saved, but terminal proxy endpoint could not be published: %w", publishErr)
		}
		return nil
	})
	return result, err
}

func changedProxyPorts(old, next manifest) map[string]int {
	changes := make(map[string]int)
	for _, port := range []struct {
		key      string
		from, to int
	}{
		{"mixed-port", old.Port, next.Port},
		{"port", old.Listeners.HTTPPort, next.Listeners.HTTPPort},
		{"socks-port", old.Listeners.SOCKSPort, next.Listeners.SOCKSPort},
		{"redir-port", old.Listeners.RedirPort, next.Listeners.RedirPort},
		{"tproxy-port", old.Listeners.TProxyPort, next.Listeners.TProxyPort},
	} {
		if port.from != port.to {
			changes[port.key] = port.to
		}
	}
	return changes
}

func (s *RuntimeSettings) preserveHiddenPorts(installed mihomo.ListenerOptions) {
	s.CoreOS = installed.CoreOS
	if !installed.SupportsRedirect() {
		s.RedirPort = installed.RedirPort
	}
	if !installed.SupportsTProxy() {
		s.TProxyPort = installed.TProxyPort
	}
}

// generationGuard protects both settings metadata and bytes. Recovery checks
// both generations before moving the pointer, preserving any later user edits.
func (a *App) generationGuard(target string) (string, error) {
	if !generationName.MatchString(target) {
		return "", diagnostic.New("diagnostic.settings.generationInvalid", "invalid configuration generation")
	}
	dir := filepath.Join(a.Home, "installation", target)
	if err := existingDir(dir); err != nil {
		return "", err
	}
	raw, err := store.Read(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return "", err
	}
	config, err := store.Read(filepath.Join(dir, "config.yaml"))
	if err != nil {
		return "", err
	}
	var m manifest
	if json.Unmarshal(raw, &m) != nil || m.ID == "" || m.Service != a.Name || digest(config) != m.SHA {
		return "", diagnostic.New("diagnostic.settings.generationChanged", "configuration generation changed; preserving files for manual inspection")
	}
	tree, err := installationTree(dir)
	if err != nil {
		return "", err
	}
	if tree["manifest.json"] != "file:"+digest(raw) || tree["config.yaml"] != "file:"+digest(config) {
		return "", diagnostic.New("diagnostic.settings.generationCheckChanged", "configuration changed while checking its generation; review again")
	}
	identity, _ := json.Marshal(tree)
	return digest(identity), nil
}
