package mihomo

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func buildOptions(base string) BuildOptions {
	return BuildOptions{Base: []byte(base), Controller: "127.0.0.1:19090", Secret: "managed-secret", MixedPort: 17890}
}

func builtConfig(t *testing.T, opts BuildOptions) map[string]any {
	t.Helper()
	raw, err := Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestBuildFullProfileSeparatesUserRoutingAndApplicationSettings(t *testing.T) {
	opts := buildOptions(`external-controller: 0.0.0.0:9999
secret: subscription-secret
mixed-port: 9998
mode: direct
proxies:
  - {name: subscription-node, type: socks5, server: example.invalid, port: 1080}
proxy-groups:
  - {name: PROXY, type: select, proxies: [subscription-node, DIRECT]}
rules:
  - DOMAIN-SUFFIX,example.invalid,PROXY
  - MATCH,DIRECT
sniffer: {enable: true, force-dns-mapping: true}
experimental-field: {version: 9}
`)
	baseBefore := bytes.Clone(opts.Base)
	opts.Mode = "global"
	opts.Overrides = []byte(`set:
  sniffer: {enable: false}
  log-level: info
prepend-rules:
  - DOMAIN,private.example.invalid,REJECT
  - DOMAIN,private.example.invalid,REJECT
`)
	patchBefore := bytes.Clone(opts.Overrides)
	cfg := builtConfig(t, opts)
	if cfg["external-controller"] != opts.Controller || cfg["secret"] != opts.Secret || cfg["mixed-port"] != opts.MixedPort || cfg["mode"] != "global" {
		t.Fatal("application settings did not win over subscription settings")
	}
	if !reflect.DeepEqual(cfg["sniffer"], map[string]any{"enable": false}) {
		t.Fatal("set implicitly deep-merged a mapping instead of replacing it")
	}
	if cfg["experimental-field"].(map[string]any)["version"] != 9 {
		t.Fatal("unknown core configuration was discarded")
	}
	rules, _ := configRules(cfg)
	want := []string{"DOMAIN,private.example.invalid,REJECT", "DOMAIN,private.example.invalid,REJECT", "DOMAIN-SUFFIX,example.invalid,PROXY", "MATCH,DIRECT"}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("rule order or duplicate preservation changed: %v", rules)
	}
	if !bytes.Equal(opts.Base, baseBefore) || !bytes.Equal(opts.Overrides, patchBefore) {
		t.Fatal("build mutated persisted input bytes")
	}
}

func TestBuildManagedProfileExtractsOnlyOrderedInlineNodes(t *testing.T) {
	opts := buildOptions(`proxies:
  - {name: local, type: direct}
proxy-groups:
  - {name: PROXY, type: select, include-all-proxies: true}
rules: ["MATCH,PROXY"]
hosts: {template.example.invalid: 192.0.2.5}
`)
	opts.NodeSources = []NodeSource{
		{Name: "one", Raw: []byte(`proxies:
  - {name: first, type: socks5, server: one.invalid, port: 1080}
proxy-groups:
  - {name: source-group, type: select, proxies: [not-extracted]}
rules: ["MATCH,source-group"]
proxy-providers: {ignored: {type: http, url: https://private.invalid/token}}
dns: {enable: true}
hosts: {source.example.invalid: 192.0.2.8}
`)},
		{Name: "two", Raw: []byte(`proxies:
  - {name: second, type: socks5, server: two.invalid, port: 1080}
`)},
	}
	cfg := builtConfig(t, opts)
	nodes, _ := configEntries(cfg, "proxies")
	var names []string
	for _, node := range nodes {
		names = append(names, node.(map[string]any)["name"].(string))
	}
	if !reflect.DeepEqual(names, []string{"local", "first", "second"}) {
		t.Fatalf("node order changed: %v", names)
	}
	groups, _ := configEntries(cfg, "proxy-groups")
	if len(groups) != 1 || groups[0].(map[string]any)["include-all-proxies"] != true {
		t.Fatal("managed template groups were replaced")
	}
	if _, exists := cfg["proxy-providers"]; exists {
		t.Fatal("managed source imported a provider")
	}
	if !reflect.DeepEqual(cfg["hosts"], map[string]any{"template.example.invalid": "192.0.2.5"}) {
		t.Fatal("source hosts replaced managed template hosts")
	}
}

func TestBuildRejectsAmbiguousManagedSources(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sources []NodeSource
	}{
		{name: "empty-list", sources: []NodeSource{}},
		{name: "provider-only", sources: []NodeSource{{Name: "provider", Raw: []byte("proxy-providers: {remote: {type: http, url: https://private.invalid/token}}")}}},
		{name: "empty-nodes", sources: []NodeSource{{Name: "empty", Raw: []byte("proxies: []")}}},
		{name: "duplicate-identifiers", sources: []NodeSource{{Name: "same", Raw: []byte("proxies: [{name: first, type: direct}]")}, {Name: "same", Raw: []byte("proxies: [{name: second, type: direct}]")}}},
		{name: "duplicate-nodes", sources: []NodeSource{{Name: "one", Raw: []byte("proxies: [{name: same, type: direct}]")}, {Name: "two", Raw: []byte("proxies: [{name: same, type: direct}]")}}},
		{name: "lost-dialer-group", sources: []NodeSource{{Name: "source", Raw: []byte("proxies: [{name: first, type: socks5, dialer-proxy: source-group}]\nproxy-groups: [{name: source-group, type: select, proxies: [DIRECT]}]")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := buildOptions("rules: [\"MATCH,DIRECT\"]")
			opts.NodeSources = tc.sources
			if _, err := Build(opts); err == nil {
				t.Fatal("ambiguous managed source accepted")
			}
		})
	}
}

func TestBuildOverridesReplaceListsAndRejectHiddenDeletionOrOwnedFields(t *testing.T) {
	base := "proxies: [{name: old, type: direct}]\nproxy-groups: [{name: PROXY, type: select, proxies: [old]}]\nrules: [\"MATCH,PROXY\"]"
	opts := buildOptions(base)
	opts.Overrides = []byte(`set:
  proxies: [{name: replacement, type: direct}]
  proxy-groups: [{name: PROXY, type: select, proxies: [replacement]}]
  rules: ["MATCH,REJECT"]
`)
	cfg := builtConfig(t, opts)
	nodes, _ := configEntries(cfg, "proxies")
	if len(nodes) != 1 || nodes[0].(map[string]any)["name"] != "replacement" {
		t.Fatal("set did not replace the entire proxy list")
	}
	for _, patch := range []string{
		"set: {mixed-port: 19999}", "set: {secret: private-token}",
		"set: {tun: {enable: true}}", "set: {listeners: []}",
		"set: {dns: {listen: '0.0.0.0:53'}}",
		"set: {profile: {store-selected: false}}", "set: {sniffer: null}",
		"set: {sniffer: {enable: null}}", "set: {rules: [null]}",
		"set: null", "append-rules: null", "merge: {mode: rule}",
		"set: {proxy-groups: []}", // Leaves a dangling MATCH target.
	} {
		opts := buildOptions(base)
		opts.Overrides = []byte(patch)
		if _, err := Build(opts); err == nil {
			t.Fatalf("unsafe override accepted: %s", patch)
		}
	}
}

func TestBuildAllowsResolverOverridesWithoutOpeningDNSListener(t *testing.T) {
	opts := buildOptions("rules: [\"MATCH,DIRECT\"]")
	opts.Overrides = []byte("set: {dns: {enable: true, nameserver: [https://dns.example.invalid/dns-query]}}")
	cfg := builtConfig(t, opts)
	dns := cfg["dns"].(map[string]any)
	if dns["enable"] != true || dns["listen"] != "" || len(dns["nameserver"].([]any)) != 1 {
		t.Fatal("resolver override did not preserve resolver policy with the managed listener boundary")
	}
}

func TestBuildDetectsReferencesAfterAllInputsAreAssembled(t *testing.T) {
	for _, tc := range []struct {
		name, base, reason string
	}{
		{name: "duplicate-group-node", base: "proxies: [{name: same, type: direct}]\nproxy-groups: [{name: same, type: select, proxies: [DIRECT]}]", reason: "duplicate"},
		{name: "builtin-node", base: "proxies: [{name: DIRECT, type: direct}]", reason: "reserved"},
		{name: "missing-group-target", base: "proxy-groups: [{name: PROXY, type: select, proxies: [missing]}]", reason: "unknown outbound"},
		{name: "missing-provider", base: "proxy-groups: [{name: PROXY, type: select, use: [missing]}]", reason: "unknown proxy provider"},
		{name: "group-cycle", base: "proxy-groups: [{name: one, type: select, proxies: [two]}, {name: two, type: select, proxies: [one]}]", reason: "cycle"},
		{name: "missing-rule-target", base: "rules: [\"DOMAIN,example.invalid,missing\"]", reason: "unknown outbound"},
		{name: "missing-rule-provider", base: "rules: [\"RULE-SET,missing,DIRECT\"]", reason: "unknown rule provider"},
		{name: "node-cycle", base: "proxies: [{name: one, type: direct, dialer-proxy: two}, {name: two, type: direct, dialer-proxy: one}]", reason: "cycle"},
		{name: "node-group-cycle", base: "proxies: [{name: one, type: direct, dialer-proxy: PROXY}]\nproxy-groups: [{name: PROXY, type: select, proxies: [one]}]", reason: "cycle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Build(buildOptions(tc.base))
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("expected %s, got %v", tc.reason, err)
			}
		})
	}
	// Patches may repair references before the final namespace is validated.
	opts := buildOptions("proxy-groups: [{name: PROXY, type: select, proxies: [new]}]\nrules: [\"MATCH,PROXY\"]")
	opts.Overrides = []byte("set: {proxies: [{name: new, type: direct}]}")
	builtConfig(t, opts)
}

func TestBuildRulesRemainOrderedAndAppendCannotHideAfterMatch(t *testing.T) {
	opts := buildOptions("rules: [\"DOMAIN,existing.invalid,DIRECT\"]")
	opts.Overrides = []byte("prepend-rules: [\"DOMAIN,first.invalid,REJECT\"]\nappend-rules: [\"MATCH,DIRECT\"]")
	cfg := builtConfig(t, opts)
	rules, _ := configRules(cfg)
	if !reflect.DeepEqual(rules, []string{"DOMAIN,first.invalid,REJECT", "DOMAIN,existing.invalid,DIRECT", "MATCH,DIRECT"}) {
		t.Fatal("ordered rule operations changed matching precedence")
	}
	opts.Base = []byte("rules: [\"MATCH,DIRECT\"]")
	opts.Overrides = []byte("append-rules: [\"DOMAIN,unreachable.invalid,REJECT\"]")
	if _, err := Build(opts); err == nil || !strings.Contains(err.Error(), "MATCH") {
		t.Fatal("silently appended an unreachable rule", err)
	}
	// A blanket CSV parser must not mistake a nested logical expression for an
	// outbound name. Native -t remains responsible for the expression's semantics.
	builtConfig(t, buildOptions("rules: [\"AND,((DOMAIN,a.invalid),(NETWORK,TCP)),DIRECT\", \"MATCH,DIRECT\"]"))
}

func TestBuildProviderReferencesAndExplicitGlobalGroup(t *testing.T) {
	cfg := builtConfig(t, buildOptions(`proxy-providers:
  remote: {type: http, url: https://example.invalid/provider, interval: 3600}
rule-providers:
  custom: {type: inline, behavior: domain, payload: [example.invalid]}
proxy-groups:
  - {name: GLOBAL, type: select, proxies: [DIRECT], use: [remote]}
rules: ["RULE-SET,custom,GLOBAL", "MATCH,DIRECT"]
`))
	if cfg["proxy-providers"] == nil || cfg["rule-providers"] == nil {
		t.Fatal("full-profile providers were lost")
	}
}

func TestValidateSourceIsStructuralAndErrorsWithholdConfigurationValues(t *testing.T) {
	if err := ValidateSource([]byte("proxies: [{name: node, type: direct}]\nproxy-groups: [{name: source, type: select, proxies: [missing]}]\nrules: [\"MATCH,missing\"]")); err != nil {
		t.Fatal("source validation incorrectly requires its unused routing to resolve", err)
	}
	for _, raw := range []string{
		"", "null", "[]", "a: 1\na: 2", "a: 1\n---\na: 2",
		"proxies: {}", "proxy-groups: [private-token]",
		"proxy-providers: {remote: private-token}", "rules: [123]",
		"proxies: [{name: private-token, type: direct}, {name: private-token, type: direct}]",
	} {
		if err := ValidateSource([]byte(raw)); err == nil || strings.Contains(err.Error(), "private-token") {
			t.Fatalf("source accepted or leaked configuration: %v", err)
		}
	}
	opts := buildOptions("rules: [\"MATCH,private-token\"]")
	if _, err := Build(opts); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("reference error leaked a configuration value", err)
	}
}

func TestBuildPreservesMihomoRegexAndPassRuleSyntax(t *testing.T) {
	// Each input was also accepted by the official v1.19.33 binary with -t.
	// Regex payloads contain commas; rule types are case-insensitive; MATCH can
	// select PASS directly or through a group and continue with later rules.
	for _, tc := range []struct{ name, base string }{
		{name: "domain-regex", base: `rules: ['DOMAIN-REGEX,^api[0-9]{1,3}\.example\.invalid$,DIRECT', 'MATCH,DIRECT']`},
		{name: "process-name-regex", base: `rules: ['PROCESS-NAME-REGEX,^foo(,bar)?$,DIRECT', 'MATCH,DIRECT']`},
		{name: "process-path-regex", base: `rules: ['process-path-regex,^/bin/foo(,bar)?$,DIRECT', 'match,DIRECT']`},
		{name: "lowercase-logic", base: `rules: ['and,((DOMAIN,a.invalid),(NETWORK,TCP)),DIRECT', 'match,DIRECT']`},
		{name: "match-pass", base: `rules: ['MATCH,PASS', 'DOMAIN,a.invalid,DIRECT', 'MATCH,REJECT']`},
		{name: "match-pass-group", base: "proxy-groups: [{name: maybe, type: select, proxies: [PASS,DIRECT]}]\nrules: ['MATCH,maybe', 'MATCH,DIRECT']"},
		{name: "existing-trailing-rule", base: `rules: ['MATCH,DIRECT', 'DOMAIN,a.invalid,REJECT']`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := decodeConfig([]byte(tc.base))
			if err != nil {
				t.Fatal(err)
			}
			after := builtConfig(t, buildOptions(tc.base))
			if !reflect.DeepEqual(before["rules"], after["rules"]) {
				t.Fatal("builder rewrote core-specific rule syntax")
			}
		})
	}
}

func TestBuildAppendChecksOnlyCertainTerminalMatches(t *testing.T) {
	for _, rule := range []string{"MATCH,DIRECT", "match,REJECT", "MATCH,REJECT-DROP"} {
		opts := buildOptions("rules: ['" + rule + "']")
		opts.Overrides = []byte("append-rules: ['DOMAIN,new.invalid,DIRECT']")
		if _, err := Build(opts); err == nil || !strings.Contains(err.Error(), "append-rules") {
			t.Fatal("appended after a known terminal builtin", err)
		}
	}
	for _, base := range []string{
		"rules: ['MATCH,PASS']",
		"proxy-groups: [{name: maybe, type: select, proxies: [PASS,DIRECT]}]\nrules: ['MATCH,maybe']",
	} {
		opts := buildOptions(base)
		opts.Overrides = []byte("append-rules: ['MATCH,DIRECT']")
		builtConfig(t, opts)
	}
}

func TestSourceNullContainersRemainEquivalentToOmittedFields(t *testing.T) {
	base := "proxies: null\nproxy-groups: null\nrules: null\nproxy-providers: null\nrule-providers: null\n"
	if err := ValidateSource([]byte(base)); err != nil {
		t.Fatal("valid native empty source containers were rejected", err)
	}
	builtConfig(t, buildOptions(base))
	for _, patch := range []string{"set: {proxies: null}", "set: {rule-providers: null}", "prepend-rules: null", "append-rules: null"} {
		opts := buildOptions(base)
		opts.Overrides = []byte(patch)
		if _, err := Build(opts); err == nil {
			t.Fatal("null was accepted as an implicit override deletion")
		}
	}
}

func TestValidateSourceRejectsErrorResponsesAndForeignCoreDocuments(t *testing.T) {
	for _, raw := range []string{
		"{}", "error: private-content-token", `{"error":"private-content-token","code":403}`,
		"outbounds: [{type: direct, tag: private-content-token}]",
		"outbounds: null\ndns: {servers: [{tag: private-content-token, type: local}]}",
		"inbounds: []\nmode: rule\nprivate-field: private-content-token",
	} {
		err := ValidateSource([]byte(raw))
		if err == nil || strings.Contains(err.Error(), "private-content-token") {
			t.Fatalf("unrecognized source was accepted or echoed: %v", err)
		}
	}
	for _, raw := range []string{
		"mode: rule", "mode: invalid-native-mode", "rules: []", "rules: null",
		"proxies: []", "proxies: null", "proxy-groups: []", "proxy-providers: {}",
		"rule-providers: null", "dns: {}", "hosts: {}", "sniffer: {}", "profile: {}",
		"rules: []\noutbounds: []\ncustom-extension: preserved",
	} {
		if err := ValidateSource([]byte(raw)); err != nil {
			t.Fatal("valid recognizable source container was rejected", err)
		}
	}
	// Format identification belongs to downloaded sources. User templates may
	// retain unknown extension fields, and an invalid explicit mode must reach
	// native -t unchanged rather than silently becoming the default rule mode.
	cfg := builtConfig(t, buildOptions("custom-extension: preserved"))
	if cfg["custom-extension"] != "preserved" {
		t.Fatal("builder applied source-format restrictions to a template")
	}
	cfg = builtConfig(t, buildOptions("mode: invalid-native-mode"))
	if cfg["mode"] != "invalid-native-mode" {
		t.Fatal("invalid mode was silently replaced before native validation")
	}
}
