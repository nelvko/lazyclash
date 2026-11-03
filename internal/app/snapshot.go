package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
)

type Snapshot struct {
	TUN                 bool
	TUNActual           *bool
	TUNError            string
	Installed           bool
	Version             string
	RunningVersion      string
	ConfigReady         bool
	HasRun              bool
	GuideDismissed      bool
	UnvalidatedProfiles []string
	Service             service.Status
	ServiceError        string
	ServiceDiagnostic   error `json:"-"`
	Controller          string
	ProfilePending      bool
	Generation          string
	Applied             string
	AppliedName         string
	Profiles            []string
	ProfileDetails      []ProfileInfo
	Groups              []mihomo.Group
	Proxies             map[string]mihomo.Proxy
	APIError            string
	APIDiagnostic       error `json:"-"`
	Desired             DesiredState
	Operation           OperationState
	Drift               string
	Mode                ModeStatus
	Capabilities        Capabilities
}

func (a *App) Snapshot(ctx context.Context) (Snapshot, error) {
	var out Snapshot
	err := a.locked(ctx, func() error {
		out.Capabilities = a.Capabilities()
		prefs, _, err := a.loadPreferencesLocked()
		if err != nil {
			return err
		}
		out.HasRun = prefs.HasRun
		out.GuideDismissed = prefs.GuideDismissed
		m, generation, err := a.load()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			out.TUN = m.TUN
			out.Installed = true
			out.ConfigReady = true
			out.Version = m.Version
			out.Controller = m.Controller
			out.Applied = m.Applied
			out.Generation = generation
			r, _, err := a.loadRuntimeRecord(m)
			if err != nil {
				return err
			}
			out.Desired, out.Operation = r.Desired, r.Operation
			stopped, err := a.stopIntent()
			if err != nil {
				return err
			}
			if stopped != nil {
				out.Desired.Known, out.Desired.Running = true, false
			}
			config, err := store.Read(filepath.Join(a.Home, "installation", "current", "config.yaml"))
			if err != nil {
				return err
			}
			out.Mode.Desired, err = mihomo.ConfigMode(config)
			if err != nil {
				return err
			}
		}
		if a.Service == nil {
			out.Service.State = "unsupported"
			if a.PlatformError != nil {
				out.ServiceError = a.PlatformError.Error()
				out.ServiceDiagnostic = a.PlatformError
			}
		} else {
			s, err := a.Service.Inspect(ctx)
			out.Service = s
			if err != nil {
				out.ServiceError = err.Error()
				out.ServiceDiagnostic = err
			}
		}
		if out.Installed && a.Service != nil && out.ServiceError == "" {
			bin, config, data := a.paths()
			if err := a.Service.Verify(ctx, bin, config, data); err != nil {
				out.ServiceError = err.Error()
				out.ServiceDiagnostic = err
			}
		}
		lib, _, err := a.library().Load()
		if err != nil {
			return err
		}
		out.Profiles = []string{"default"}
		for name, entry := range lib.Profiles {
			out.Profiles = append(out.Profiles, name)
			if !entry.Validated {
				out.UnvalidatedProfiles = append(out.UnvalidatedProfiles, name)
			}
		}
		sort.Strings(out.Profiles[1:])
		sort.Strings(out.UnvalidatedProfiles)
		for _, name := range out.Profiles {
			info, err := a.profileInfo(lib, name, m, out.Installed)
			if err != nil {
				return err
			}
			out.ProfileDetails = append(out.ProfileDetails, info)
			if name == out.Applied {
				out.ProfilePending = info.Pending
				out.AppliedName = info.Name
			}
		}
		if out.Installed && out.ServiceError != "" {
			out.APIError = "Managed service is unavailable; resolve the service diagnostic first"
		} else if out.Installed && out.Service.Active {
			out.RunningVersion, err = a.client(m).Version(ctx)
			if err != nil {
				out.APIError = err.Error()
				out.APIDiagnostic = err
			} else if !prefs.HasRun {
				// An owned, authenticated running engine is also evidence of a
				// completed first run, including starts performed outside this CLI.
				if err := a.markHasRunLocked(ctx); err != nil {
					return err
				}
				out.HasRun = true
			}
			out.Proxies, err = a.client(m).Proxies(ctx)
			if err != nil {
				out.APIError = err.Error()
				out.APIDiagnostic = err
			} else {
				out.Groups = mihomo.ProxyGroups(out.Proxies)
			}
			out.Mode.Running = true
			cfg, err := a.client(m).Configuration(ctx)
			if err != nil {
				out.Mode.Error = err.Error()
				out.Mode.ErrorDiagnostic = err
			} else {
				out.Mode.Actual = cfg.Mode
				out.TUNActual = &cfg.TUN
				if cfg.TUN != out.TUN {
					out.TUNError = "Running TUN differs from the saved policy; restart the service"
				}
			}
		} else if out.Installed {
			out.APIError = "Start the service to view proxy groups"
		}
		out.Drift = runtimeDrift(out.Desired, out.Service, out.APIError)
		if out.Drift == "" && out.Mode.Actual != "" && out.Mode.Desired != out.Mode.Actual {
			out.Drift = "The running mode differs from the saved configuration; apply the desired mode again"
		}
		return ctx.Err()
	})
	return out, err
}
