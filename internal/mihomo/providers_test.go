package mihomo

import (
	"path"
	"regexp"
	"strings"
	"testing"
)

func normalizedProviders(t *testing.T, raw string) map[string]any {
	t.Helper()
	cfg, err := decodeConfig([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := normalizeProviderPaths(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func providerPath(t *testing.T, cfg map[string]any, kind, name string) string {
	t.Helper()
	return cfg[kind].(map[string]any)[name].(map[string]any)["path"].(string)
}

func TestHTTPProviderCachesCannotShareSourceSuppliedPaths(t *testing.T) {
	cfg := normalizedProviders(t, `proxy-providers:
  first: {type: http, url: 'https://first.invalid/private-token', path: ./shared.yaml, interval: 3600}
  second: {type: http, url: 'https://second.invalid/private-token', path: ./shared.yaml, interval: 3600}
  missing: {type: http, url: 'https://third.invalid/private-token'}
rule-providers:
  first: {type: http, url: 'https://first.invalid/private-token', behavior: domain, path: ./shared.yaml}
`)
	seen := map[string]bool{}
	for _, key := range []struct{ kind, name, prefix string }{{"proxy-providers", "first", "proxy"}, {"proxy-providers", "second", "proxy"}, {"proxy-providers", "missing", "proxy"}, {"rule-providers", "first", "rule"}} {
		cache := providerPath(t, cfg, key.kind, key.name)
		if !regexp.MustCompile(`^providers/`+key.prefix+`-[a-f0-9]{64}\.yaml$`).MatchString(cache) || seen[cache] || strings.Contains(cache, "private-token") || path.Clean(cache) != cache {
			t.Fatal("provider cache collided, escaped its directory or exposed source credentials")
		}
		seen[cache] = true
	}
	provider := cfg["proxy-providers"].(map[string]any)["first"].(map[string]any)
	if provider["url"] != "https://first.invalid/private-token" || provider["interval"] != 3600 {
		t.Fatal("isolating cache changed the download source or its update policy")
	}
}

func TestHTTPProviderIdentityIncludesDefinitionButIgnoresLegacyPath(t *testing.T) {
	original := "proxy-providers: {source: {type: http, url: 'https://source.invalid/token', path: ./old.yaml, header: {Authorization: [secret]}, interval: 3600}}"
	first := providerPath(t, normalizedProviders(t, original), "proxy-providers", "source")
	// Key insertion order and the old cache location cannot change identity.
	reordered := "proxy-providers: {source: {interval: 3600, header: {Authorization: [secret]}, path: cache.db, url: 'https://source.invalid/token', type: http}}"
	if next := providerPath(t, normalizedProviders(t, reordered), "proxy-providers", "source"); next != first {
		t.Fatal("equivalent provider definitions did not reuse their isolated cache")
	}
	for _, changed := range []string{
		strings.Replace(original, "source.invalid", "other.invalid", 1),
		strings.Replace(original, "[secret]", "[different-secret]", 1),
		strings.Replace(original, "interval: 3600", "interval: 7200", 1),
		strings.Replace(original, "source:", "renamed:", 1),
	} {
		cfg := normalizedProviders(t, changed)
		name := "source"
		if strings.Contains(changed, "renamed:") {
			name = "renamed"
		}
		if next := providerPath(t, cfg, "proxy-providers", name); next == first {
			t.Fatal("changed provider definition reused a different source's cache")
		}
	}
	rule := "rule-providers: {rules: {type: http, url: 'https://rules.invalid/token', behavior: domain, format: yaml}}"
	before := providerPath(t, normalizedProviders(t, rule), "rule-providers", "rules")
	after := providerPath(t, normalizedProviders(t, strings.Replace(rule, "domain", "classical", 1)), "rule-providers", "rules")
	if before == after {
		t.Fatal("different rule provider behaviors reused one cache")
	}
}

func TestFileProvidersPreserveOnlyExplicitRuntimeAssets(t *testing.T) {
	cfg := normalizedProviders(t, "proxy-providers: {local: {type: file, path: ./assets/nodes.yaml}}\nrule-providers: {local: {type: file, path: assets/rules.yaml, behavior: domain}}")
	if providerPath(t, cfg, "proxy-providers", "local") != "assets/nodes.yaml" || providerPath(t, cfg, "rule-providers", "local") != "assets/rules.yaml" {
		t.Fatal("file provider assets were moved into HTTP caches")
	}
	for _, value := range []any{"", ".", "/tmp/private-token", "../private-token", "assets/../private-token", "C:/private-token", "C:private-token", `\\server\share\private-token`, `assets\private-token`, "assets/private-token\n", nil, 12, []any{"private-token"}} {
		cfg := map[string]any{"proxy-providers": map[string]any{"local": map[string]any{"type": "file", "path": value}}}
		if err := normalizeProviderPaths(cfg); err == nil || strings.Contains(err.Error(), "private-token") {
			t.Fatal("unsafe file provider path was accepted or exposed", err)
		}
	}
}

func TestHTTPProviderRejectsWrongPathTypesWithoutExposingValues(t *testing.T) {
	for _, value := range []any{nil, true, 12, []any{"private-token"}, map[string]any{"private-token": true}} {
		cfg := map[string]any{"rule-providers": map[string]any{"rules": map[string]any{"type": "http", "path": value}}}
		if err := normalizeProviderPaths(cfg); err == nil || strings.Contains(err.Error(), "private-token") {
			t.Fatal("invalid HTTP path was accepted or exposed", err)
		}
	}
}
