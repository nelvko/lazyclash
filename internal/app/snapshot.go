package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

type Snapshot struct {
	Installed           bool
	Version             string
	RunningVersion      string
	ConfigReady         bool
	HasRun              bool
	GuideDismissed      bool
	UnvalidatedProfiles []string
	Service             service.Status
	ServiceError        string
	Controller          string
	Applied             string
	Profiles            []string
	Groups              []mihomo.Group
	APIError            string
}

func (a *App) Snapshot(ctx context.Context) (Snapshot, error) {
	var out Snapshot
	err := a.locked(ctx, func() error {
		prefs, _, err := a.loadPreferencesLocked()
		if err != nil {
			return err
		}
		out.HasRun = prefs.HasRun
		out.GuideDismissed = prefs.GuideDismissed
		m, _, err := a.load()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			out.Installed = true
			out.ConfigReady = true
			out.Version = m.Version
			out.Controller = m.Controller
			out.Applied = m.Applied
		}
		if a.Service == nil {
			out.Service.State = "unsupported"
			if a.PlatformError != nil {
				out.ServiceError = a.PlatformError.Error()
			}
		} else {
			s, err := a.Service.Inspect(ctx)
			out.Service = s
			if err != nil {
				out.ServiceError = err.Error()
			}
		}
		if out.Installed && a.Service != nil && out.ServiceError == "" {
			bin, config, data := a.paths()
			if err := a.Service.Verify(ctx, bin, config, data); err != nil {
				out.ServiceError = err.Error()
			}
		}
		profilesDir := filepath.Join(a.Home, "profiles")
		if err := existingDir(profilesDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		entries, err := os.ReadDir(profilesDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		out.Profiles = []string{"default"}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				out.Profiles = append(out.Profiles, strings.TrimSuffix(e.Name(), ".yaml"))
			}
		}
		sort.Strings(out.Profiles[1:])
		for _, name := range out.Profiles[1:] {
			if _, err := os.Lstat(filepath.Join(profilesDir, name+".unvalidated")); err == nil || !out.Installed {
				out.UnvalidatedProfiles = append(out.UnvalidatedProfiles, name)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if out.Installed && out.ServiceError != "" {
			out.APIError = "Managed service is unavailable; resolve the service diagnostic first"
		} else if out.Installed && out.Service.Active {
			out.RunningVersion, err = a.client(m).Version(ctx)
			if err != nil {
				out.APIError = err.Error()
			} else if !prefs.HasRun {
				// An owned, authenticated running engine is also evidence of a
				// completed first run, including starts performed outside this CLI.
				if err := a.markHasRunLocked(ctx); err != nil {
					return err
				}
				out.HasRun = true
			}
			out.Groups, err = a.client(m).Groups(ctx)
			if err != nil {
				out.APIError = err.Error()
			}
		} else if out.Installed {
			out.APIError = "Start the service to view proxy groups"
		}
		return ctx.Err()
	})
	return out, err
}
