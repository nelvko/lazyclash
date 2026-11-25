package app

import (
	"context"

	"github.com/nelvko/lazyclash/internal/mihomo"
)

// SetCoreSetting updates one instance preference under the lock so concurrent
// edits to different settings compose.
func (a *App) SetCoreSetting(ctx context.Context, key, value string) error {
	return a.locked(ctx, func() error {
		m, target, err := a.load()
		if err != nil {
			return err
		}
		next, err := m.CoreSettings.With(key, value)
		if err != nil {
			return err
		}
		if next == m.CoreSettings {
			return nil
		}
		deployed, err := a.readGenerationConfig(target, m.SHA)
		if err != nil {
			return err
		}
		m.CoreSettings = next
		cfg, err := mihomo.WithGeneralSettings(deployed, next, m.TUN)
		if err != nil {
			return err
		}
		if digest(cfg) != m.SHA {
			if err := a.validateConfig(ctx, m, cfg); err != nil {
				return err
			}
		}
		return a.publishProfileRefresh(ctx, target, m, cfg)
	})
}
