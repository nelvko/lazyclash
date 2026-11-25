package mihomo

import (
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// GeneralSettings belongs to one managed instance and applies to every profile.
type GeneralSettings struct {
	IPv6         string `json:"ipv6"`
	UnifiedDelay string `json:"unified_delay"`
	LogLevel     string `json:"log_level"`
}

func DefaultGeneralSettings() GeneralSettings {
	return GeneralSettings{IPv6: "on", UnifiedDelay: "off", LogLevel: "info"}
}

// ResolveApplied fills unset selections from the verified generation that was
// actually deployed. Its profile may since have been edited or refreshed.
func (s GeneralSettings) ResolveApplied(raw []byte) (GeneralSettings, error) {
	if s.IPv6 == "" || s.UnifiedDelay == "" || s.LogLevel == "" {
		defaults := DefaultGeneralSettings()
		applied := struct {
			IPv6         bool   `yaml:"ipv6"`
			UnifiedDelay bool   `yaml:"unified-delay"`
			LogLevel     string `yaml:"log-level"`
		}{defaults.IPv6 == "on", defaults.UnifiedDelay == "on", defaults.LogLevel}
		if err := yaml.Unmarshal(raw, &applied); err != nil {
			return s, invalidGeneralSetting()
		}
		if s.IPv6 == "" {
			s.IPv6 = toggleValue(applied.IPv6)
		}
		if s.UnifiedDelay == "" {
			s.UnifiedDelay = toggleValue(applied.UnifiedDelay)
		}
		if s.LogLevel == "" {
			s.LogLevel = strings.ToLower(applied.LogLevel)
		}
	}
	return s, s.validate()
}

func toggleValue(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

// With validates a single edit and keeps all unrelated selections.
func (s GeneralSettings) With(key, value string) (GeneralSettings, error) {
	next := s
	switch key {
	case "ipv6":
		next.IPv6 = value
	case "unified-delay":
		next.UnifiedDelay = value
	case "log-level":
		next.LogLevel = value
	default:
		return s, invalidGeneralSetting()
	}
	if err := next.validate(); err != nil {
		return s, err
	}
	return next, nil
}

func invalidGeneralSetting() error {
	return diagnostic.New("diagnostic.settings.coreOption", "invalid Mihomo general setting or value")
}

func (s GeneralSettings) validate() error {
	toggle := func(value string) bool { return value == "on" || value == "off" }
	if toggle(s.IPv6) && toggle(s.UnifiedDelay) {
		switch s.LogLevel {
		case "silent", "error", "warning", "info", "debug":
			return nil
		}
	}
	return invalidGeneralSetting()
}

// WithGeneralSettings updates the deployed YAML even when its original inputs
// are unavailable. Host TUN still requires IPv6 while it is active.
func WithGeneralSettings(raw []byte, s GeneralSettings, tun bool) ([]byte, error) {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return nil, err
	}
	if err := s.apply(cfg, tun); err != nil {
		return nil, err
	}
	return yaml.Marshal(cfg)
}

func (s GeneralSettings) apply(cfg map[string]any, tun bool) error {
	if err := s.validate(); err != nil {
		return err
	}
	cfg["ipv6"] = tun || s.IPv6 == "on"
	cfg["unified-delay"] = s.UnifiedDelay == "on"
	cfg["log-level"] = s.LogLevel
	return nil
}
