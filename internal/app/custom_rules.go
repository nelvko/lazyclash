package app

import (
	"context"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// CustomRulesDraft edits the saved profile's prepend-rules, independently of
// the running core's rule indexes. Token binds edits to the complete recipe and
// is deliberately omitted from public JSON output.
type CustomRulesDraft struct {
	ProfileID   string   `json:"profile_id"`
	ProfileName string   `json:"profile_name"`
	Rules       []string `json:"rules"`
	Token       string   `json:"-"`
	Pending     bool     `json:"pending"`
	Applied     bool     `json:"applied"`
	Validated   bool     `json:"validated"`
}

// GetCustomRules reads an editable saved profile. An empty reference selects
// the currently applied profile; the built-in default has no editable recipe.
func (a *App) GetCustomRules(ctx context.Context, profile string) (CustomRulesDraft, error) {
	var out CustomRulesDraft
	err := a.locked(ctx, func() error {
		lib, _, err := a.library().Load()
		if err != nil {
			return err
		}
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		if profile == "" {
			profile = m.selectedProfile()
			if profile == "" {
				return customRulesReadOnly()
			}
		}
		id, err := resolveProfileID(lib, profile)
		if err != nil {
			return err
		}
		out, err = a.customRulesDraft(lib, id, m, installed)
		if err != nil {
			return err
		}
		// Verify the immutable source before returning an editable recipe.
		_, err = a.library().Read(lib.Profiles[id].Spec.Revision)
		if err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return CustomRulesDraft{}, err
	}
	return out, nil
}

// SaveCustomRules updates the override document reviewed by the rules editor.
func (a *App) SaveCustomRules(ctx context.Context, draft CustomRulesDraft) (CustomRulesDraft, error) {
	var out CustomRulesDraft
	err := a.locked(ctx, func() error {
		lib, old, err := a.library().Load()
		if err != nil {
			return err
		}
		if draft.ProfileID == "default" {
			return customRulesReadOnly()
		}
		record, err := recipeRecord(lib, draft.ProfileID)
		if err != nil {
			return err
		}
		if draft.Token == "" || draft.Token != record.fingerprint() {
			return diagnostic.New("diagnostic.customRules.conflict", "profile configuration changed while editing custom rules; reload and retry")
		}
		entry := lib.Profiles[draft.ProfileID]
		raw, err := mihomo.WithCustomRules([]byte(entry.Spec.Overrides), draft.Rules)
		if err != nil {
			return err
		}
		previous := entry
		entry.Spec.Overrides = string(raw)
		lib.Profiles[draft.ProfileID] = entry
		if err := a.saveProfileEdit(ctx, lib, old, draft.ProfileID, previous); err != nil {
			return err
		}
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		out, err = a.customRulesDraft(lib, draft.ProfileID, m, installed)
		return err
	})
	if err != nil {
		return CustomRulesDraft{}, err
	}
	return out, nil
}

func (a *App) customRulesDraft(lib configuration.Library, id string, m manifest, installed bool) (CustomRulesDraft, error) {
	if id == "default" {
		return CustomRulesDraft{}, customRulesReadOnly()
	}
	record, err := recipeRecord(lib, id)
	if err != nil {
		return CustomRulesDraft{}, err
	}
	rules, err := mihomo.CustomRules([]byte(record.Recipe.Overrides))
	if err != nil {
		return CustomRulesDraft{}, err
	}
	info, err := a.profileInfo(lib, id, m, installed)
	if err != nil {
		return CustomRulesDraft{}, err
	}
	return CustomRulesDraft{
		ProfileID: id, ProfileName: info.Name, Rules: rules, Token: record.fingerprint(),
		Pending: info.Pending, Applied: installed && m.Applied == id, Validated: info.Validated,
	}, nil
}

func customRulesReadOnly() error {
	return diagnostic.New("diagnostic.customRules.readOnly", "select a saved profile to customize its rules")
}
