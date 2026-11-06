package app

import (
	"context"
	"slices"

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
			profile = m.Applied
			if profile == "" {
				profile = "default"
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
		// Check source integrity and the complete override structure without
		// executing the engine for a read-only editor query.
		_, _, err = a.buildProfile(ctx, lib, id, m, false)
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

// SaveCustomRules validates and atomically saves a draft. It neither applies a
// profile nor changes services; the running generation remains pinned until an
// explicit Apply. Metadata supplied by a caller is never trusted for routing.
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
		entry.Spec.Overrides = string(raw)
		lib.Profiles[draft.ProfileID] = entry
		m, installed, err := a.buildContext()
		if err != nil {
			return err
		}
		if _, _, err := a.buildProfile(ctx, lib, draft.ProfileID, m, installed); err != nil {
			return err
		}
		entry.Validated = installed
		lib.Profiles[draft.ProfileID] = entry
		out, err = a.customRulesDraft(lib, draft.ProfileID, m, installed)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.library().Save(lib, old)
	})
	if err != nil {
		return CustomRulesDraft{}, err
	}
	return out, nil
}

// ApplyCustomRules applies exactly the saved recipe reviewed by the editor.
// The current profile and its token are checked under the same lock and against
// the same inputs used by the normal generation/rollback transaction.
func (a *App) ApplyCustomRules(ctx context.Context, draft CustomRulesDraft) error {
	return a.locked(ctx, func() error {
		if draft.ProfileID == "default" {
			return customRulesReadOnly()
		}
		return a.applyProfileLocked(ctx, draft.ProfileID, &draft)
	})
}

func validateCustomRulesApply(lib configuration.Library, m manifest, id string, draft CustomRulesDraft) error {
	if id == "default" {
		return customRulesReadOnly()
	}
	record, err := recipeRecord(lib, id)
	if err != nil {
		return err
	}
	if id != draft.ProfileID || draft.Token == "" || draft.Token != record.fingerprint() {
		return diagnostic.New("diagnostic.customRules.conflict", "profile configuration changed while editing custom rules; reload and retry")
	}
	rules, err := mihomo.CustomRules([]byte(record.Recipe.Overrides))
	if err != nil {
		return err
	}
	if !slices.Equal(rules, draft.Rules) {
		return diagnostic.New("diagnostic.customRules.conflict", "profile configuration changed while editing custom rules; reload and retry")
	}
	if m.Applied != id {
		return diagnostic.New("diagnostic.customRules.profileChanged", "the applied profile changed while editing custom rules; reopen the editor")
	}
	return nil
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
	return diagnostic.New("diagnostic.customRules.readOnly", "the built-in default profile is read only; choose a saved profile to customize rules")
}
