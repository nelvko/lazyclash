package mihomo

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

// TUNResources reserves a private device, routing table and eleven consecutive
// policy-rule priorities. sing-tun owns that priority range while auto-route runs.
type TUNResources struct {
	Device string `json:"device"`
	Table  int    `json:"table"`
	Rule   int    `json:"rule"`
}

func ManagedTUN(serviceName string) (TUNResources, error) {
	if !regexp.MustCompile(`^lazyclash-[A-Za-z0-9-]+$`).MatchString(serviceName) {
		return TUNResources{}, fmt.Errorf("invalid TUN service name")
	}
	sum := sha256.Sum256([]byte(serviceName))
	return TUNResources{
		Device: fmt.Sprintf("lc%x", sum[:6]),
		Table:  100000 + int(binary.BigEndian.Uint32(sum[:4])%1000000),
		Rule:   10000 + int(binary.BigEndian.Uint32(sum[4:8])%1000)*11,
	}, nil
}

// WithTUN applies the application's explicitly authorized host traffic policy.
// Subscription fields never authorize network administration. DNS interception
// stays inside the TUN; this policy does not change the host resolver settings.
func WithTUN(raw []byte, enabled bool, serviceName string) ([]byte, error) {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return nil, err
	}
	cfg["tun"] = map[string]any{"enable": false}
	if !enabled {
		return yaml.Marshal(cfg)
	}
	r, err := ManagedTUN(serviceName)
	if err != nil {
		return nil, err
	}
	cfg["ipv6"] = true
	cfg["tun"] = map[string]any{
		"enable":                true,
		"device":                r.Device,
		"stack":                 "mixed",
		"mtu":                   1500,
		"auto-route":            true,
		"auto-redirect":         false,
		"auto-detect-interface": true,
		"strict-route":          true,
		"iproute2-table-index":  r.Table,
		"iproute2-rule-index":   r.Rule,
		"inet4-address":         []string{"198.18.0.1/30"},
		"inet6-address":         []string{"fdfe:dcba:9876::1/126"},
		"dns-mode":              "disabled",
		"dns-hijack":            []string{"any:53", "tcp://any:53"},
	}
	dns, _ := cfg["dns"].(map[string]any)
	if dns == nil {
		dns = make(map[string]any)
	}
	dns["enable"] = true
	dns["listen"] = ""
	if dns["nameserver"] == nil {
		dns["nameserver"] = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}
	}
	if dns["default-nameserver"] == nil {
		dns["default-nameserver"] = []string{"1.1.1.1", "8.8.8.8"}
	}
	cfg["dns"] = dns
	return yaml.Marshal(cfg)
}
