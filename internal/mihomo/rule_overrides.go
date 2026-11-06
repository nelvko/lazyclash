package mihomo

import (
	"bytes"
	"slices"
	"strings"
	"unicode"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"gopkg.in/yaml.v3"
)

// CustomRules reads the user-owned prepend list without exposing other override
// fields. A complete set.rules takeover must be removed explicitly first.
func CustomRules(raw []byte) ([]string, error) {
	patch, err := customRulesPatch(raw)
	if err != nil {
		return nil, err
	}
	rules, err := overrideRules(patch, "prepend-rules")
	if err == nil {
		err = validateCustomRules(rules)
	}
	return append([]string{}, rules...), err
}

// WithCustomRules replaces only prepend-rules. Other override values retain
// their semantics; YAML formatting is canonicalized when the list changes.
func WithCustomRules(raw []byte, rules []string) ([]byte, error) {
	patch, err := customRulesPatch(raw)
	if err != nil {
		return nil, err
	}
	if err := validateCustomRules(rules); err != nil {
		return nil, err
	}
	previous, err := overrideRules(patch, "prepend-rules")
	if err != nil {
		return nil, err
	}
	if slices.Equal(previous, rules) {
		return bytes.Clone(raw), nil
	}
	if len(rules) == 0 {
		delete(patch, "prepend-rules")
	} else {
		patch["prepend-rules"] = slices.Clone(rules)
	}
	if len(patch) == 0 {
		return nil, nil
	}
	result, err := yaml.Marshal(patch)
	if err != nil {
		return nil, diagnostic.New("diagnostic.config.buildEncoding", "cannot encode Mihomo build inputs")
	}
	if len(result) > MaxConfigSize {
		return nil, diagnostic.New("diagnostic.profile.overridesSize", "overrides exceed 8 MiB")
	}
	return result, nil
}

func validateCustomRules(rules []string) error {
	size := 0
	for _, rule := range rules {
		if strings.TrimSpace(rule) == "" || strings.IndexFunc(rule, unicode.IsControl) >= 0 {
			return diagnostic.New("diagnostic.customRules.invalidRules", "custom rules must be nonempty single-line strings without control characters")
		}
		size += len(rule)
		if size > MaxConfigSize {
			return diagnostic.New("diagnostic.profile.overridesSize", "overrides exceed 8 MiB")
		}
	}
	return nil
}

func customRulesPatch(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	patch, err := decodeConfig(raw)
	if err != nil {
		return nil, diagnostic.New("diagnostic.config.overridesMapping", "invalid profile overrides: expected one YAML mapping with unique keys")
	}
	if fields, ok := patch["set"].(map[string]any); ok {
		if _, exists := fields["rules"]; exists {
			return nil, diagnostic.New("diagnostic.customRules.takeover", "this profile replaces the complete rules list; remove set.rules before editing custom rules")
		}
	}
	// Share the builder's override schema, including managed-field restrictions,
	// rather than letting this editor silently repair an invalid saved document.
	if err := applyOverrides(map[string]any{}, raw); err != nil {
		return nil, err
	}
	return patch, nil
}
