package app

import (
	"context"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// SetCoreSetting updates one override under the instance lock so concurrent
// edits to different settings compose. Clearing it restores the deployed
// profile's own value, including a per-profile override.
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
		m.CoreSettings = next
		cfg, err := a.buildAppliedRecipe(m)
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

// Use the generation's immutable inputs. Library edits and downloads may be
// newer than the profile that the user actually applied.
func (a *App) buildAppliedRecipe(m manifest) ([]byte, error) {
	if m.Build == nil || m.Build.Profile != m.Applied ||
		(m.Applied != "default" && (m.Build.Recipe == nil || m.Build.Recipe.Core != "mihomo")) {
		return nil, diagnostic.New("diagnostic.profile.pinnedRecipeMissing", "could not identify the applied profile recipe")
	}
	return a.buildRecipe(*m.Build, m)
}
