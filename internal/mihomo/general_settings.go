package mihomo

import "github.com/nelvko/lazyclash/internal/diagnostic"

// GeneralSettings contains instance-level overrides. An empty value inherits
// the built profile, including an omitted field that Mihomo defaults itself.
type GeneralSettings struct {
	IPv6         string `json:"ipv6,omitempty"`
	UnifiedDelay string `json:"unified_delay,omitempty"`
	LogLevel     string `json:"log_level,omitempty"`
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
		return s, diagnostic.New("diagnostic.settings.coreOption", "invalid Mihomo general setting or value")
	}
	if err := next.Validate(); err != nil {
		return s, err
	}
	return next, nil
}

func (s GeneralSettings) Validate() error {
	toggle := func(value string) bool { return value == "" || value == "on" || value == "off" }
	if toggle(s.IPv6) && toggle(s.UnifiedDelay) {
		switch s.LogLevel {
		case "", "silent", "error", "warning", "info", "debug":
			return nil
		}
	}
	return diagnostic.New("diagnostic.settings.coreOption", "invalid Mihomo general setting or value")
}

func (s GeneralSettings) apply(cfg map[string]any) {
	if s.IPv6 != "" {
		cfg["ipv6"] = s.IPv6 == "on"
	}
	if s.UnifiedDelay != "" {
		cfg["unified-delay"] = s.UnifiedDelay == "on"
	}
	if s.LogLevel != "" {
		cfg["log-level"] = s.LogLevel
	}
}
