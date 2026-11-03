// Package mihomo implements the single supported proxy engine.
package mihomo

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

const MaxConfigSize = 8 << 20

// Default needs no subscription or geodata files and still exposes a useful group.
func Default(controller, secret string, mixedPort int) ([]byte, error) {
	return Effective([]byte("mode: rule\nlog-level: warning\nproxies: []\nproxy-groups:\n  - name: PROXY\n    type: select\n    proxies: [DIRECT, REJECT]\nrules: [\"MATCH,PROXY\"]\n"), controller, secret, mixedPort)
}

// Effective owns the local listeners and credentials. Subscription routing and
// outbound definitions survive, but cannot enable privileged or public inbounds.
func Effective(raw []byte, controller, secret string, mixedPort int) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 || len(raw) > MaxConfigSize {
		return nil, diagnostic.New("diagnostic.config.yamlSize", "Mihomo YAML must be nonempty and at most 8 MiB")
	}
	if err := loopbackAddress(controller); err != nil {
		return nil, err
	}
	if secret == "" || strings.IndexFunc(secret, unicode.IsControl) >= 0 {
		return nil, diagnostic.New("diagnostic.config.controllerSecret", "controller secret must be nonempty and contain no control characters")
	}
	if mixedPort < 1024 || mixedPort > 65535 {
		return nil, diagnostic.New("diagnostic.config.mixedPort", "mixed port must be between 1024 and 65535")
	}
	_, apiPort, _ := net.SplitHostPort(controller)
	if apiPort == strconv.Itoa(mixedPort) {
		return nil, diagnostic.New("diagnostic.config.portConflict", "controller and proxy ports must differ")
	}
	cfg, err := decodeConfig(raw)
	if err != nil {
		return nil, err
	}
	if tun, ok := cfg["tun"].(map[string]any); ok {
		if tun["enable"] == true {
			return nil, diagnostic.New("diagnostic.config.tunEnabled", "this profile enables TUN; disable tun.enable before importing and explicitly authorize TUN in lazyclash settings")
		}
	}
	cfg["external-controller"], cfg["secret"] = controller, secret
	cfg["mixed-port"], cfg["allow-lan"], cfg["bind-address"] = mixedPort, false, "127.0.0.1"
	cfg["authentication"] = []string{}
	for _, k := range []string{"port", "socks-port", "redir-port", "tproxy-port", "routing-mark", "external-controller-routing-mark"} {
		cfg[k] = 0
	}
	for _, k := range []string{"external-controller-tls", "external-controller-unix", "external-controller-pipe", "external-doh-server", "external-ui", "external-ui-url", "external-ui-name", "ss-config", "vmess-config", "interface-name"} {
		cfg[k] = ""
	}
	for _, k := range []string{"listeners", "tunnels"} {
		if k == "listeners" {
			if listeners, ok := cfg[k].([]any); ok {
				for _, listener := range listeners {
					if entry, ok := listener.(map[string]any); ok && entry["type"] == "tun" {
						return nil, diagnostic.New("diagnostic.config.tunListener", "this profile contains a TUN listener; managed TUN must be explicitly authorized in lazyclash settings")
					}
				}
			}
		}
		cfg[k] = []any{}
	}
	for _, k := range []string{"tun", "tuic-server", "iptables", "ntp"} {
		cfg[k] = map[string]any{"enable": false}
	}
	// Resolver settings may be required to reach the subscription's nodes even
	// without a public DNS listener. Own the listener, not the resolver policy.
	dns := map[string]any{"enable": false}
	if value, exists := cfg["dns"]; exists {
		var ok bool
		dns, ok = value.(map[string]any)
		if !ok {
			return nil, diagnostic.New("diagnostic.config.dnsMapping", "invalid Mihomo DNS configuration: expected a mapping")
		}
	}
	dns["listen"] = ""
	cfg["dns"] = dns
	cfg["external-controller-cors"] = map[string]any{"allow-origins": []string{}, "allow-private-network": false}
	// Mihomo owns this cache in its stable -d directory. API selections then
	// survive both configuration generations and systemd crash recovery.
	profile, _ := cfg["profile"].(map[string]any)
	if profile == nil {
		profile = make(map[string]any)
	}
	profile["store-selected"] = true
	cfg["profile"] = profile
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, diagnostic.New("diagnostic.config.encode", "cannot encode Mihomo YAML")
	}
	return out, nil
}

func decodeConfig(raw []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 || len(raw) > MaxConfigSize {
		return nil, diagnostic.New("diagnostic.config.yamlSize", "Mihomo YAML must be nonempty and at most 8 MiB")
	}
	var cfg map[string]any
	d := yaml.NewDecoder(bytes.NewReader(raw))
	if err := d.Decode(&cfg); err != nil || cfg == nil {
		return nil, diagnostic.New("diagnostic.config.yamlMapping", "invalid Mihomo YAML: expected one mapping with unique keys")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, diagnostic.New("diagnostic.config.yamlDocuments", "invalid Mihomo YAML: multiple documents are not supported")
	}
	return cfg, nil
}

func ValidMode(mode string) bool {
	return mode == "rule" || mode == "global" || mode == "direct"
}

func ConfigMode(raw []byte) (string, error) {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return "", err
	}
	if cfg["mode"] == nil {
		return "rule", nil
	}
	mode, ok := cfg["mode"].(string)
	if !ok || !ValidMode(mode) {
		return "", diagnostic.New("diagnostic.api.mode", "Mihomo mode must be rule, global or direct")
	}
	return mode, nil
}

// WithMode changes only the operation mode; listener and routing ownership
// remain with the effective configuration that the application already built.
func WithMode(raw []byte, mode string) ([]byte, error) {
	if !ValidMode(mode) {
		return nil, diagnostic.New("diagnostic.api.mode", "Mihomo mode must be rule, global or direct")
	}
	cfg, err := decodeConfig(raw)
	if err != nil {
		return nil, err
	}
	cfg["mode"] = mode
	return yaml.Marshal(cfg)
}

func ConfigTUN(raw []byte) (bool, error) {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return false, err
	}
	tun, ok := cfg["tun"].(map[string]any)
	if !ok && cfg["tun"] != nil {
		return false, diagnostic.New("diagnostic.config.tun", "invalid Mihomo TUN configuration")
	}
	enabled, ok := tun["enable"].(bool)
	if !ok && tun["enable"] != nil {
		return false, diagnostic.New("diagnostic.config.tunEnable", "invalid Mihomo TUN enable setting")
	}
	return enabled, nil
}

func ConfigSelectionPersistence(raw []byte) (bool, error) {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return false, err
	}
	profile, ok := cfg["profile"].(map[string]any)
	if !ok && cfg["profile"] != nil {
		return false, diagnostic.New("diagnostic.config.profileCache", "invalid Mihomo profile cache configuration")
	}
	if profile["store-selected"] == nil {
		return true, nil // Mihomo's native default.
	}
	enabled, ok := profile["store-selected"].(bool)
	if !ok {
		return false, diagnostic.New("diagnostic.config.selectionCache", "invalid Mihomo selection cache setting")
	}
	return enabled, nil
}

func loopbackAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return diagnostic.New("diagnostic.config.controllerLoopback", "controller must be a loopback IP address with a port")
	}
	ip := net.ParseIP(host)
	p, err := strconv.Atoi(port)
	if ip == nil || !ip.IsLoopback() || err != nil || p < 1024 || p > 65535 {
		return diagnostic.New("diagnostic.config.controllerPort", "controller must use a loopback IP and a port between 1024 and 65535")
	}
	return nil
}

type limitedOutput struct {
	bytes.Buffer
	limit int
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := w.limit - w.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = w.Buffer.Write(p)
	}
	return n, nil
}

func BinaryVersion(ctx context.Context, bin string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := SafeExecutable(bin); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd, err := executableCommand(ctx, bin, "-v")
	if err != nil {
		return "", err
	}
	out := &limitedOutput{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	err = cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", diagnostic.New("diagnostic.executable.version", "cannot execute Mihomo binary with -v; check the OS, architecture and executable permission")
	}
	for _, line := range strings.Split(out.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "Mihomo" {
			continue
		}
		index := 1
		if fields[index] == "Meta" {
			index++
		}
		if index < len(fields) && validVersionToken(fields[index]) {
			return fields[index], nil
		}
	}
	return "", diagnostic.New("diagnostic.executable.identity", "binary did not identify itself as Mihomo with a version")
}

// Validate intentionally keeps engine output private: parser errors may include
// full subscription URLs, passwords or control API credentials.
func Validate(ctx context.Context, bin, config, data string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := SafeExecutable(bin); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd, err := executableCommand(ctx, bin, "-t", "-d", data, "-f", config)
	if err != nil {
		return err
	}
	out := &limitedOutput{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	err = cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return diagnostic.New("diagnostic.config.validation", "Mihomo configuration validation failed (engine output withheld to protect credentials)")
	}
	return nil
}
