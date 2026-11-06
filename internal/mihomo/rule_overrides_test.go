package mihomo

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCustomRulesPreserveOtherOverrideValuesAndOrderedDuplicates(t *testing.T) {
	raw := []byte(`set:
  log-level: info
  native-extension:
    nested: [{name: private-setting, options: {enabled: true, priority: 4}}]
append-rules: ["MATCH,DIRECT"]
prepend-rules: ["DOMAIN,old.invalid,REJECT"]
`)
	rules := []string{"DOMAIN,z.invalid,DIRECT", "DOMAIN,a.invalid,REJECT", "DOMAIN,z.invalid,DIRECT"}
	updated, err := WithCustomRules(raw, rules)
	if err != nil {
		t.Fatal(err)
	}
	got, err := CustomRules(updated)
	if err != nil || !reflect.DeepEqual(got, rules) {
		t.Fatalf("rule order or duplicate changed: %v %v", got, err)
	}
	var before, after map[string]any
	if err := yaml.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(updated, &after); err != nil {
		t.Fatal(err)
	}
	delete(before, "prepend-rules")
	delete(after, "prepend-rules")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("editing custom rules changed another override value")
	}
	got[0] = "MATCH,REJECT"
	fresh, err := CustomRules(updated)
	if err != nil || fresh[0] != rules[0] {
		t.Fatal("returned rule slice mutated persisted YAML", err)
	}
	cleared, err := WithCustomRules(updated, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(cleared, &after); err != nil {
		t.Fatal(err)
	}
	if _, exists := after["prepend-rules"]; exists {
		t.Fatal("clearing rules retained the prepend field")
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("clearing custom rules changed another override")
	}
}

func TestCustomRulesNoOpAndClearPreserveEmptyRecipe(t *testing.T) {
	raw := []byte("# useful editor comment\nprepend-rules: ['DOMAIN,a.invalid,DIRECT']\n")
	updated, err := WithCustomRules(raw, []string{"DOMAIN,a.invalid,DIRECT"})
	if err != nil || !bytes.Equal(updated, raw) {
		t.Fatal("no-op edit changed the raw document", err)
	}
	updated[0] = 'x'
	if raw[0] != '#' {
		t.Fatal("returned document aliases its input")
	}
	cleared, err := WithCustomRules(raw, []string{})
	if err != nil || len(cleared) != 0 {
		t.Fatal("clearing the only override left a synthetic recipe", err)
	}
	rules, err := CustomRules(nil)
	if err != nil || rules == nil || len(rules) != 0 {
		t.Fatal("empty overrides did not return an empty rule list", err)
	}
}

func TestCustomRulesRejectMalformedOverridesWithoutDisclosingValues(t *testing.T) {
	for _, raw := range []string{
		"private-token", "[]", "null", " ", "prepend-rules: [",
		"prepend-rules: []\nprepend-rules: []", "{}\n---\n{}",
		"prepend-rules: null", "prepend-rules: [123]", "prepend-rules: ['']",
		"append-rules: null", "set: null", "merge: {secret: private-token}",
		"set: {secret: private-token}", "set: {native-extension: {secret: null}}",
		"set: {rules: []}", "set: {rules: ['MATCH,DIRECT']}",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := CustomRules([]byte(raw)); err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatal("malformed override accepted or its values disclosed", err)
			}
			if _, err := WithCustomRules([]byte(raw), []string{"DOMAIN,a.invalid,DIRECT"}); err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatal("editor silently repaired invalid overrides or disclosed values", err)
			}
		})
	}
}

func TestCustomRulesRejectEmptyMultilineAndOversizedInput(t *testing.T) {
	for _, rule := range []string{"", " \t ", "DOMAIN,a.invalid,DIRECT\nMATCH,REJECT", "DOMAIN,a.invalid,\x00DIRECT", "DOMAIN,a.invalid,\tDIRECT", strings.Repeat("x", MaxConfigSize+1)} {
		if _, err := WithCustomRules(nil, []string{rule}); err == nil {
			t.Fatal("invalid custom rule accepted")
		}
	}
}
