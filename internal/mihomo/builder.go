package mihomo

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// BuildOptions describes reproducible inputs, independent of service state.
// Base is one complete configuration. Overrides uses the small schema
// set/prepend-rules/append-rules, never an implicit YAML deep merge.
type BuildOptions struct {
	Base               []byte
	Overrides          []byte
	Controller, Secret string
	MixedPort          int
	Mode               string
}

// Build preserves unknown Mihomo fields and delegates their final semantics to
// the installed core's -t validation. Errors never include configuration values.
func Build(opts BuildOptions) ([]byte, error) {
	cfg, err := decodeConfig(opts.Base)
	if err != nil {
		return nil, err
	}
	if len(opts.Overrides) != 0 {
		if err := applyOverrides(cfg, opts.Overrides); err != nil {
			return nil, err
		}
	}
	if err := validateContainers(cfg); err != nil {
		return nil, err
	}
	if err := normalizeProviderPaths(cfg); err != nil {
		return nil, err
	}
	if err := validateReferences(cfg); err != nil {
		return nil, err
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, errors.New("cannot encode Mihomo build inputs")
	}
	out, err := Effective(raw, opts.Controller, opts.Secret, opts.MixedPort)
	if err != nil {
		return nil, err
	}
	if opts.Mode != "" {
		return WithMode(out, opts.Mode)
	}
	return out, nil
}

// ValidateSource checks that a downloaded document contains Mihomo fields.
// Build checks its complete routing and the application's runtime constraints.
func ValidateSource(raw []byte) error {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return err
	}
	// Mihomo ignores unknown YAML fields, so -t alone accepts an API error
	// object or another core's document as an empty, direct-routing profile.
	// Identify the downloaded format before building an effective configuration.
	hasRouting := false
	for _, key := range []string{"proxies", "proxy-groups", "proxy-providers", "rules", "rule-providers"} {
		if _, exists := cfg[key]; exists {
			hasRouting = true
			break
		}
	}
	for _, key := range []string{"outbounds", "inbounds"} {
		if _, exists := cfg[key]; exists && !hasRouting {
			return errors.New("subscription appears to use another core's configuration format; expected Mihomo YAML")
		}
	}
	hasField := hasRouting
	for _, key := range []string{"mode", "dns", "hosts", "sniffer", "profile"} {
		if _, exists := cfg[key]; exists {
			hasField = true
			break
		}
	}
	if !hasField {
		return errors.New("subscription contains no recognized Mihomo configuration fields")
	}
	return validateContainers(cfg)
}

func configEntries(cfg map[string]any, key string) ([]any, error) {
	value, exists := cfg[key]
	if !exists || value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("Mihomo %s must be a YAML list", key)
	}
	return items, nil
}

func configRules(cfg map[string]any) ([]string, error) {
	entries, err := configEntries(cfg, "rules")
	if err != nil {
		return nil, err
	}
	rules := make([]string, 0, len(entries))
	for _, entry := range entries {
		rule, ok := entry.(string)
		if !ok || strings.TrimSpace(rule) == "" {
			return nil, errors.New("Mihomo rules must contain nonempty strings")
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// GLOBAL may be an explicit group, or the core creates it automatically. The
// remaining names are concrete outbounds installed before parsing user entries.
var builtinOutbounds = map[string]bool{
	"DIRECT": true, "REJECT": true, "REJECT-DROP": true,
	"COMPATIBLE": true, "PASS": true, "PASS-RULE": true,
}

func validateContainers(cfg map[string]any) error {
	names := make(map[string]bool)
	for _, key := range []string{"proxies", "proxy-groups"} {
		entries, err := configEntries(cfg, key)
		if err != nil {
			return err
		}
		for i, item := range entries {
			entry, ok := item.(map[string]any)
			if !ok {
				return invalidEntryError(key, i)
			}
			name, named := entry["name"].(string)
			kind, typed := entry["type"].(string)
			if !named || !typed || strings.TrimSpace(name) == "" || strings.TrimSpace(kind) == "" {
				return invalidEntryError(key, i)
			}
			if names[name] || builtinOutbounds[name] || (key == "proxies" && name == "GLOBAL") {
				return fmt.Errorf("Mihomo %s entry %d has a duplicate or reserved outbound name", key, i+1)
			}
			names[name] = true
		}
	}
	if _, err := configRules(cfg); err != nil {
		return err
	}
	for _, key := range []string{"proxy-providers", "rule-providers"} {
		value, exists := cfg[key]
		if !exists || value == nil {
			continue
		}
		providers, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("Mihomo %s must be a mapping", key)
		}
		for name, item := range providers {
			if _, ok := item.(map[string]any); !ok || name == "" {
				return fmt.Errorf("Mihomo %s entries must be named mappings", key)
			}
		}
	}
	return nil
}

func invalidEntryError(key string, index int) error {
	return fmt.Errorf("Mihomo %s entry %d requires a mapping with nonempty name and type", key, index+1)
}

// These settings belong to lazyclash's listener/permission policy. A patch must
// fail explicitly rather than appear successful while Effective discards it.
var managedOverrideFields = map[string]bool{
	"external-controller": true, "secret": true, "mixed-port": true, "mode": true,
	"allow-lan": true, "bind-address": true, "authentication": true,
	"port": true, "socks-port": true, "redir-port": true, "tproxy-port": true,
	"routing-mark": true, "external-controller-routing-mark": true,
	"external-controller-tls": true, "external-controller-unix": true,
	"external-controller-pipe": true, "external-controller-cors": true,
	"external-doh-server": true, "external-ui": true, "external-ui-url": true,
	"external-ui-name": true, "ss-config": true, "vmess-config": true,
	"interface-name": true, "listeners": true, "tunnels": true, "tun": true,
	"tuic-server": true, "iptables": true, "ntp": true,
}

func applyOverrides(cfg map[string]any, raw []byte) error {
	patch, err := decodeConfig(raw)
	if err != nil {
		return errors.New("invalid profile overrides: expected one YAML mapping with unique keys")
	}
	for key := range patch {
		if key != "set" && key != "prepend-rules" && key != "append-rules" {
			return errors.New("unknown profile override operation; use set, prepend-rules or append-rules")
		}
	}
	if value, exists := patch["set"]; exists {
		fields, ok := value.(map[string]any)
		if !ok || containsNull(fields) {
			return errors.New("override set must be a mapping without null values")
		}
		for key, value := range fields {
			if managedOverrideFields[key] {
				return errors.New("override set contains an application-managed field; change application settings instead")
			}
			if key == "profile" {
				profile, ok := value.(map[string]any)
				if !ok {
					return errors.New("override profile must be a mapping")
				}
				if _, exists := profile["store-selected"]; exists {
					return errors.New("profile.store-selected is managed by lazyclash")
				}
			}
			if key == "dns" {
				if dns, ok := value.(map[string]any); ok {
					if _, exists := dns["listen"]; exists {
						return errors.New("dns.listen is managed by lazyclash; resolver settings may be overridden without a listener")
					}
				}
			}
			cfg[key] = value // Explicit whole-field replacement, including mappings.
		}
	}
	rules, err := configRules(cfg)
	if err != nil {
		return err
	}
	prepend, err := overrideRules(patch, "prepend-rules")
	if err != nil {
		return err
	}
	appendRules, err := overrideRules(patch, "append-rules")
	if err != nil {
		return err
	}
	if len(appendRules) != 0 {
		// MATCH may route to PASS, or to a group whose selection resolves to
		// PASS. Reject only the terminal builtins whose behavior is certain;
		// never reject an existing full profile merely for later rules.
		for _, rule := range append(prepend, rules...) {
			parts := strings.Split(rule, ",")
			if strings.EqualFold(strings.TrimSpace(parts[0]), "MATCH") && len(parts) >= 2 {
				switch strings.TrimSpace(parts[1]) {
				case "DIRECT", "REJECT", "REJECT-DROP":
					return errors.New("override append-rules would follow a terminal MATCH rule; prepend rules or replace the rule list")
				}
			}
		}
	}
	if len(prepend) != 0 || len(appendRules) != 0 {
		rules = append(append(prepend, rules...), appendRules...)
		entries := make([]any, len(rules))
		for i, rule := range rules {
			entries[i] = rule
		}
		cfg["rules"] = entries
	}
	return nil
}

func containsNull(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case map[string]any:
		for _, child := range v {
			if containsNull(child) {
				return true
			}
		}
	case map[any]any:
		for _, child := range v {
			if containsNull(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsNull(child) {
				return true
			}
		}
	}
	return false
}

func overrideRules(patch map[string]any, key string) ([]string, error) {
	value, exists := patch[key]
	if !exists {
		return nil, nil
	}
	if value == nil {
		return nil, fmt.Errorf("override %s must be a list of nonempty rule strings", key)
	}
	rules, err := configRules(map[string]any{"rules": value})
	if err != nil {
		return nil, fmt.Errorf("override %s must be a list of nonempty rule strings", key)
	}
	return rules, nil
}

func stringReferences(entry map[string]any, key string) ([]string, error) {
	value, exists := entry[key]
	if !exists {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("proxy-group references must be lists of nonempty strings")
	}
	refs := make([]string, 0, len(items))
	for _, item := range items {
		ref, ok := item.(string)
		if !ok || ref == "" {
			return nil, errors.New("proxy-group references must be lists of nonempty strings")
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func validateReferences(cfg map[string]any) error {
	outbounds := map[string]bool{"GLOBAL": true}
	for name := range builtinOutbounds {
		outbounds[name] = true
	}
	groups := make(map[string]map[string]any)
	nodes, _ := configEntries(cfg, "proxies")
	groupEntries, _ := configEntries(cfg, "proxy-groups")
	for _, item := range nodes {
		entry := item.(map[string]any)
		outbounds[entry["name"].(string)] = true
	}
	for _, item := range groupEntries {
		entry := item.(map[string]any)
		name := entry["name"].(string)
		outbounds[name], groups[name] = true, entry
	}
	providers, _ := cfg["proxy-providers"].(map[string]any)
	edges := make(map[string][]string)
	for name, group := range groups {
		refs, err := stringReferences(group, "proxies")
		if err != nil {
			return err
		}
		for _, ref := range refs {
			if !outbounds[ref] {
				return errors.New("proxy-group references an unknown outbound")
			}
			edges[name] = append(edges[name], ref)
		}
		uses, err := stringReferences(group, "use")
		if err != nil {
			return err
		}
		for _, ref := range uses {
			if _, ok := providers[ref]; !ok {
				return errors.New("proxy-group references an unknown proxy provider")
			}
		}
	}
	if hasReferenceCycle(edges) {
		return errors.New("proxy-groups contain a reference cycle")
	}
	// Nodes may dial through groups or other nodes. Validate the complete
	// namespace, including node-to-node loops.
	for _, item := range nodes {
		node := item.(map[string]any)
		value, exists := node["dialer-proxy"]
		if !exists || value == "" {
			continue
		}
		ref, ok := value.(string)
		if !ok || !outbounds[ref] {
			return errors.New("proxy dialer-proxy references an unknown outbound")
		}
		edges[node["name"].(string)] = append(edges[node["name"].(string)], ref)
	}
	if hasReferenceCycle(edges) {
		return errors.New("outbound dialer references contain a cycle")
	}
	rules, _ := configRules(cfg)
	ruleProviders, _ := cfg["rule-providers"].(map[string]any)
	for i, rule := range rules {
		parts := strings.Split(rule, ",")
		kind := strings.ToUpper(strings.TrimSpace(parts[0]))
		// Logical and sub-rule expressions have nested syntax. The core is the
		// authoritative parser; splitting them as simple CSV would reject valid input.
		if kind == "AND" || kind == "OR" || kind == "NOT" || kind == "SUB-RULE" {
			continue
		}
		if kind == "RULE-SET" && len(parts) >= 3 {
			if _, ok := ruleProviders[strings.TrimSpace(parts[1])]; !ok {
				return fmt.Errorf("rule %d references an unknown rule provider", i+1)
			}
		}
		target := 2
		switch kind {
		case "MATCH":
			target = 1
		case "DOMAIN-REGEX", "PROCESS-NAME-REGEX", "PROCESS-PATH-REGEX":
			// Mihomo treats commas inside these payloads as part of the regex
			// and takes the last component as the target, without rule params.
			target = len(parts) - 1
		}
		if len(parts) > target && !outbounds[strings.TrimSpace(parts[target])] {
			return fmt.Errorf("rule %d references an unknown outbound", i+1)
		}
	}
	return nil
}

func hasReferenceCycle(edges map[string][]string) bool {
	state := make(map[string]uint8)
	var visit func(string) bool
	visit = func(name string) bool {
		if state[name] == 1 {
			return true
		}
		if state[name] == 2 {
			return false
		}
		state[name] = 1
		for _, next := range edges[name] {
			if visit(next) {
				return true
			}
		}
		state[name] = 2
		return false
	}
	for name := range edges {
		if visit(name) {
			return true
		}
	}
	return false
}
