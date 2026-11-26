package mihomo

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"gopkg.in/yaml.v3"
)

const DefaultLANAllowedIPs = "0.0.0.0/0\n::/0"

// AccessRules contains the effective, normalized source networks shown to the user.
// A blank allow list denies every source, while an absent Mihomo key allows both IP families.
type AccessRules struct {
	LANAllowedIPs    string `json:"lan_allowed_ips"`
	LANDisallowedIPs string `json:"lan_disallowed_ips"`
	SkipAuthPrefixes string `json:"skip_auth_prefixes"`
}

// AccessOverrides pins explicitly edited fields across subscription generations.
// A non-nil empty string represents an explicitly empty list.
type AccessOverrides struct {
	LANAllowedIPs    *string `json:"lan_allowed_ips,omitempty"`
	LANDisallowedIPs *string `json:"lan_disallowed_ips,omitempty"`
	SkipAuthPrefixes *string `json:"skip_auth_prefixes,omitempty"`
}

func (o AccessOverrides) apply(cfg map[string]any) {
	for key, value := range map[string]*string{
		"lan-allowed-ips": o.LANAllowedIPs, "lan-disallowed-ips": o.LANDisallowedIPs, "skip-auth-prefixes": o.SkipAuthPrefixes,
	} {
		if value != nil {
			cfg[key] = prefixLines(*value)
		}
	}
}

func prefixLines(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

// NormalizePrefixes accepts one source IP or CIDR per line, returning a sorted set.
func NormalizePrefixes(text string) (string, error) {
	var prefixes []string
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(line)
		if err != nil {
			ip, ipErr := netip.ParseAddr(line)
			if ipErr != nil || ip.Zone() != "" {
				return "", diagnostic.New("diagnostic.settings.invalidPrefix", "line %d: enter an IPv4/IPv6 address or CIDR network", i+1)
			}
			prefix = netip.PrefixFrom(ip, ip.BitLen())
		}
		prefixes = append(prefixes, prefix.Masked().String())
	}
	slices.Sort(prefixes)
	return strings.Join(slices.Compact(prefixes), "\n"), nil
}

// ConfigProxyAccess reads the deployed accounts and source networks in one pass.
func ConfigProxyAccess(raw []byte) ([]string, AccessRules, error) {
	var cfg struct {
		Authentication []string `yaml:"authentication"`
		Allowed        []string `yaml:"lan-allowed-ips"`
		Denied         []string `yaml:"lan-disallowed-ips"`
		Skip           []string `yaml:"skip-auth-prefixes"`
	}
	cfg.Allowed = prefixLines(DefaultLANAllowedIPs)
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, AccessRules{}, diagnostic.New("diagnostic.settings.coreOption", "invalid Mihomo general setting or value")
	}
	var rules AccessRules
	for field, list := range map[*string][]string{&rules.LANAllowedIPs: cfg.Allowed, &rules.LANDisallowedIPs: cfg.Denied, &rules.SkipAuthPrefixes: cfg.Skip} {
		value, err := NormalizePrefixes(strings.Join(list, "\n"))
		if err != nil {
			return nil, AccessRules{}, err
		}
		*field = value
	}
	return effectiveProxyUsers(cfg.Authentication), rules, nil
}

func effectiveProxyUsers(entries []string) []string {
	users := make([]string, 0, len(entries))
	positions := make(map[string]int, len(entries))
	for _, entry := range entries {
		name, _, ok := strings.Cut(entry, ":")
		if !ok {
			continue
		}
		if pos, exists := positions[name]; exists {
			users[pos] = entry
		} else {
			positions[name] = len(users)
			users = append(users, entry)
		}
	}
	return users
}
