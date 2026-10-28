// Package mihomo implements the single supported proxy engine.
package mihomo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
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
		return nil, errors.New("Mihomo YAML must be nonempty and at most 8 MiB")
	}
	if err := loopbackAddress(controller); err != nil {
		return nil, err
	}
	if secret == "" || strings.IndexFunc(secret, unicode.IsControl) >= 0 {
		return nil, errors.New("controller secret must be nonempty and contain no control characters")
	}
	if mixedPort < 1024 || mixedPort > 65535 {
		return nil, errors.New("mixed port must be between 1024 and 65535")
	}
	_, apiPort, _ := net.SplitHostPort(controller)
	if apiPort == strconv.Itoa(mixedPort) {
		return nil, errors.New("controller and proxy ports must differ")
	}
	var cfg map[string]any
	d := yaml.NewDecoder(bytes.NewReader(raw))
	if err := d.Decode(&cfg); err != nil || cfg == nil {
		return nil, errors.New("invalid Mihomo YAML: expected one mapping with unique keys")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, errors.New("invalid Mihomo YAML: multiple documents are not supported")
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
		cfg[k] = []any{}
	}
	for _, k := range []string{"tun", "dns", "tuic-server", "iptables", "ntp"} {
		cfg[k] = map[string]any{"enable": false}
	}
	cfg["external-controller-cors"] = map[string]any{"allow-origins": []string{}, "allow-private-network": false}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, errors.New("cannot encode Mihomo YAML")
	}
	return out, nil
}

func loopbackAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("controller must be a loopback IP address with a port")
	}
	ip := net.ParseIP(host)
	p, err := strconv.Atoi(port)
	if ip == nil || !ip.IsLoopback() || err != nil || p < 1024 || p > 65535 {
		return errors.New("controller must use a loopback IP and a port between 1024 and 65535")
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

var versionPattern = regexp.MustCompile(`(?m)\bMihomo\b[^\r\n]*?\b(v[0-9][^\s]*)`)

func BinaryVersion(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-v")
	out := &limitedOutput{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", errors.New("cannot execute Mihomo binary with -v; check the OS, architecture and executable permission")
	}
	match := versionPattern.FindStringSubmatch(out.String())
	if len(match) != 2 {
		return "", errors.New("binary did not identify itself as Mihomo with a version")
	}
	return match[1], nil
}

// Validate intentionally keeps engine output private: parser errors may include
// full subscription URLs, passwords or control API credentials.
func Validate(ctx context.Context, bin, config, data string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-t", "-d", data, "-f", config)
	out := &limitedOutput{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("Mihomo configuration validation failed (engine output withheld to protect credentials)")
	}
	return nil
}
