package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

type CoreUpdateCheck struct {
	InstalledVersion    string              `json:"installedVersion"`
	LatestVersion       string              `json:"latestVersion"`
	Channel             mihomo.Channel      `json:"channel"`
	Status              mihomo.UpdateStatus `json:"status"`
	CheckedAt           time.Time           `json:"checkedAt"`
	InstalledError      string              `json:"installedError,omitempty"`
	InstalledDiagnostic error               `json:"-"`
	guard, seal         string
}

type CoreUpgradeResult struct {
	InstalledVersion string `json:"installedVersion"`
	PreviousVersion  string `json:"previousVersion"`
	Restarted        bool   `json:"restarted"`
}

// CheckUpdate is an observation, including when recovery is pending. It never
// creates storage, repairs an installation, downloads a core or controls a unit.
// Network queries happen after releasing the read lock so Stop stays responsive.
func (a *App) CheckUpdate(ctx context.Context, channel mihomo.Channel, cfg downloadnet.Config) (CoreUpdateCheck, error) {
	var check CoreUpdateCheck
	if channel != "" && channel != mihomo.ChannelStable && channel != mihomo.ChannelAlpha {
		return check, diagnostic.New("diagnostic.upgrade.channel", "update channel must be stable, alpha or automatic")
	}
	if err := cfg.Validate(); err != nil {
		return check, err
	}
	unlock, err := store.ReadLock(ctx, a.Home)
	if err != nil {
		return check, err
	}
	check = a.observeCoreVersion(ctx)
	unlock()
	if err := ctx.Err(); err != nil {
		return check, err
	}
	installedChannel := mihomo.ChannelForVersion(check.InstalledVersion)
	if channel == "" {
		channel = installedChannel
		if check.Status == mihomo.UpdateNotInstalled || check.Status == mihomo.UpdateUnavailable {
			channel = mihomo.ChannelStable
		}
	}
	check.Channel = channel
	if channel == mihomo.ChannelUnknown {
		check.Status = mihomo.UpdateIncomparable
	} else {
		query := a.QueryLatest
		if query == nil {
			query = mihomo.LatestVersion
		}
		check.LatestVersion, err = query(ctx, channel, cfg)
		if err != nil {
			return check, err
		}
		if check.Status != mihomo.UpdateNotInstalled && check.Status != mihomo.UpdateUnavailable {
			check.Status = mihomo.CompareVersions(check.InstalledVersion, check.LatestVersion, channel)
			if installedChannel != channel {
				check.Status = mihomo.UpdateIncomparable
			}
		}
	}
	check.CheckedAt = time.Now().UTC()
	check.seal = a.updateSeal(check)
	return check, nil
}

// Called with a read or write lock, without implicit recovery. A manifest
// version alone is insufficient: -v must agree with the managed binary.
func (a *App) observeCoreVersion(ctx context.Context) CoreUpdateCheck {
	check := CoreUpdateCheck{Status: mihomo.UpdateUnavailable}
	if _, err := os.Lstat(filepath.Join(a.Home, "installation")); errors.Is(err, os.ErrNotExist) {
		check.Status = mihomo.UpdateNotInstalled
		return check
	}
	for _, name := range []string{"core-pending.json", "apply-pending.json", "install-pending.json"} {
		if _, err := os.Lstat(filepath.Join(a.Home, name)); err == nil {
			check.InstalledDiagnostic = diagnostic.New("diagnostic.upgrade.recoveryPending", "Installation recovery is pending; inspect status before checking again")
			check.InstalledError = check.InstalledDiagnostic.Error()
			return check
		} else if !errors.Is(err, os.ErrNotExist) {
			check.InstalledError = err.Error()
			check.InstalledDiagnostic = err
			return check
		}
	}
	m, generation, err := a.load()
	if err == nil {
		err = a.requireService()
	}
	if err == nil {
		bin, _, _ := a.paths()
		check.InstalledVersion, err = a.binaryVersion(ctx, bin)
	}
	if err == nil && check.InstalledVersion != m.Version {
		// Normalize a harmless v prefix while retaining the actual -v output.
		if mihomo.CompareVersions(check.InstalledVersion, m.Version, mihomo.ChannelStable) != mihomo.UpdateCurrent {
			err = diagnostic.New("diagnostic.upgrade.localVersion", "installed Mihomo version differs from its installation metadata")
		}
	}
	if err != nil {
		check.InstalledError = err.Error()
		check.InstalledDiagnostic = err
		return check
	}
	check.Status = mihomo.UpdateIncomparable
	raw, _ := json.Marshal([]string{m.ID, generation, m.BinarySHA, m.SHA, check.InstalledVersion})
	check.guard = digest(raw)
	return check
}

func (a *App) updateSeal(check CoreUpdateCheck) string {
	raw, _ := json.Marshal(check)
	return digest(append(raw, []byte("\x00"+a.Home+"\x00"+a.Name+"\x00"+check.guard)...))
}

// UpgradeCore uses the reviewed target, never a second mutable latest lookup.
// The namespace lock serializes upgrades with install/configuration/lifecycle
// operations, and the check's identity rejects a queued stale request.
func (a *App) UpgradeCore(ctx context.Context, check CoreUpdateCheck, cfg downloadnet.Config, progress func(mihomo.Progress)) (CoreUpgradeResult, error) {
	var result CoreUpgradeResult
	if check.seal == "" || check.seal != a.updateSeal(check) {
		return result, diagnostic.New("diagnostic.upgrade.checkChanged", "update check changed or belongs to another installation; check again")
	}
	if check.Channel != mihomo.ChannelStable || check.Status != mihomo.UpdateAvailable {
		return result, diagnostic.New("diagnostic.upgrade.notAvailable", "upgrade requires a newer comparable stable version; check updates first")
	}
	if err := cfg.Validate(); err != nil {
		return result, err
	}
	err := a.locked(ctx, func() error {
		fresh := a.observeCoreVersion(ctx)
		if fresh.Status == mihomo.UpdateUnavailable || fresh.guard == "" || fresh.guard != check.guard {
			return diagnostic.New("diagnostic.upgrade.stale", "installation changed since the update check; check again")
		}
		if mihomo.CompareVersions(fresh.InstalledVersion, check.LatestVersion, mihomo.ChannelStable) != mihomo.UpdateAvailable {
			return diagnostic.New("diagnostic.upgrade.downgrade", "target is not a newer stable version; refusing to downgrade")
		}
		if progress != nil {
			progress(mihomo.Progress{Stage: "query", Detail: "Resolving the reviewed release for this device"})
		}
		release, err := a.ResolveRelease(ctx, check.LatestVersion, cfg)
		if err != nil {
			return err
		}
		if mihomo.CompareVersions(release.Version, check.LatestVersion, mihomo.ChannelStable) != mihomo.UpdateCurrent {
			return diagnostic.New("diagnostic.upgrade.target", "resolved release differs from the reviewed target")
		}
		plan, err := a.prepareInstallLocked(ctx, release, "", false)
		if err != nil {
			return err
		}
		if plan.BlockedReason != "" {
			if plan.BlockedDiagnostic != nil {
				return plan.BlockedDiagnostic
			}
			return errors.New(plan.BlockedReason)
		}
		status, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		if status.Active != plan.ServiceActive || (status.State != "active" && status.State != "inactive" && status.State != "failed") {
			return diagnostic.New("diagnostic.runtime.serviceChanging", "service is changing state; wait before starting or restarting it")
		}
		if status.Active {
			m, _, err := a.load()
			if err != nil {
				return err
			}
			intent, _, err := a.loadRuntimeRecord(m)
			if err != nil {
				return err
			}
			stopped, err := a.stopIntent()
			if err != nil {
				return err
			}
			if stopped != nil || intent.Desired.Known && !intent.Desired.Running {
				return diagnostic.New("diagnostic.upgrade.runtimeDrift", "service is running despite saved stop intent; use Start or Stop to resolve the state before upgrading")
			}
			if err := a.checkPersistent(ctx); err != nil {
				return err
			}
		}
		_, err = a.installCoreLocked(ctx, plan, cfg, progress, true)
		if err != nil {
			return err
		}
		result = CoreUpgradeResult{InstalledVersion: release.Version, PreviousVersion: fresh.InstalledVersion, Restarted: status.Active}
		return nil
	})
	return result, err
}
