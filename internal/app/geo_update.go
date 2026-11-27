package app

import (
	"context"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// UpdateActiveGeo serializes explicit GEO updates with profile publication.
// Mihomo owns the writes and chooses which databases to update.
func (a *App) UpdateActiveGeo(ctx context.Context) error {
	return a.locked(ctx, func() error {
		before, client, _, err := a.inspectRuntimeLocked(ctx)
		if err != nil {
			return err
		}
		if before.State == "stopped" {
			return diagnostic.New("diagnostic.geo.requireRunning", "start the managed service before updating active GEO data")
		}
		if before.State != "ready" {
			return runtimeError(before)
		}
		if err := client.UpdateGeo(ctx); err != nil {
			return err
		}
		// systemd can restart the service independently of the application lock.
		after, err := a.Service.Inspect(ctx)
		if err != nil {
			return err
		}
		if !after.Active || profileProcess(after) != profileProcess(before.Service) {
			return diagnostic.New("diagnostic.geo.updateCoreChanged", "the managed service changed during the GEO update")
		}
		return nil
	})
}
