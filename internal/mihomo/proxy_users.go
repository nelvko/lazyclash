package mihomo

import (
	"strings"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"gopkg.in/yaml.v3"
)

// ConfigProxyUsers reads the effective accounts using Mihomo's first-colon
// separator. Malformed entries without a separator are ignored by the core.
func ConfigProxyUsers(raw []byte) ([]string, error) {
	var cfg struct {
		Authentication []string `yaml:"authentication"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, diagnostic.New("diagnostic.settings.proxyAuthConfig", "could not read proxy authentication settings")
	}
	users := make([]string, 0, len(cfg.Authentication))
	positions := make(map[string]int, len(cfg.Authentication))
	for _, entry := range cfg.Authentication {
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
	return users, nil
}
