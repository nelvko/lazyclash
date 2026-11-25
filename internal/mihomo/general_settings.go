package mihomo

import (
	"cmp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// GeneralSettings belongs to one managed instance and applies to every profile.
type GeneralSettings struct {
	IPv6             string `json:"ipv6"`
	UnifiedDelay     string `json:"unified_delay"`
	LogLevel         string `json:"log_level"`
	TCPConcurrent    string `json:"tcp_concurrent"`
	DisableKeepAlive string `json:"disable_keep_alive"`
	FindProcessMode  string `json:"find_process_mode"`
	ETagSupport      string `json:"etag_support"`
}

func DefaultGeneralSettings() GeneralSettings {
	return GeneralSettings{
		IPv6: "on", UnifiedDelay: "off", LogLevel: "info",
		TCPConcurrent: "off", DisableKeepAlive: "off", FindProcessMode: "strict", ETagSupport: "on",
	}
}

// ResolveApplied fills unset selections from the verified generation that was
// actually deployed. Its profile may since have been edited or refreshed.
func (s GeneralSettings) ResolveApplied(raw []byte) (GeneralSettings, error) {
	if s.validate() == nil {
		return s, nil
	}
	defaults := DefaultGeneralSettings()
	applied := struct {
		IPv6             bool   `yaml:"ipv6"`
		UnifiedDelay     bool   `yaml:"unified-delay"`
		LogLevel         string `yaml:"log-level"`
		TCPConcurrent    bool   `yaml:"tcp-concurrent"`
		DisableKeepAlive bool   `yaml:"disable-keep-alive"`
		FindProcessMode  string `yaml:"find-process-mode"`
		ETagSupport      bool   `yaml:"etag-support"`
	}{
		IPv6: defaults.IPv6 == "on", UnifiedDelay: defaults.UnifiedDelay == "on", LogLevel: defaults.LogLevel,
		TCPConcurrent: defaults.TCPConcurrent == "on", DisableKeepAlive: defaults.DisableKeepAlive == "on",
		FindProcessMode: defaults.FindProcessMode, ETagSupport: defaults.ETagSupport == "on",
	}
	if err := yaml.Unmarshal(raw, &applied); err != nil {
		return s, invalidGeneralSetting()
	}
	s.IPv6 = cmp.Or(s.IPv6, toggleValue(applied.IPv6))
	s.UnifiedDelay = cmp.Or(s.UnifiedDelay, toggleValue(applied.UnifiedDelay))
	s.LogLevel = cmp.Or(s.LogLevel, strings.ToLower(applied.LogLevel))
	s.TCPConcurrent = cmp.Or(s.TCPConcurrent, toggleValue(applied.TCPConcurrent))
	s.DisableKeepAlive = cmp.Or(s.DisableKeepAlive, toggleValue(applied.DisableKeepAlive))
	s.FindProcessMode = cmp.Or(s.FindProcessMode, strings.ToLower(applied.FindProcessMode))
	s.ETagSupport = cmp.Or(s.ETagSupport, toggleValue(applied.ETagSupport))
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
	case "tcp-concurrent":
		next.TCPConcurrent = value
	case "disable-keep-alive":
		next.DisableKeepAlive = value
	case "find-process-mode":
		next.FindProcessMode = value
	case "etag-support":
		next.ETagSupport = value
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
	for _, value := range [...]string{s.IPv6, s.UnifiedDelay, s.TCPConcurrent, s.DisableKeepAlive, s.ETagSupport} {
		if value != "on" && value != "off" {
			return invalidGeneralSetting()
		}
	}
	switch s.FindProcessMode {
	case "always", "strict", "off":
	default:
		return invalidGeneralSetting()
	}
	switch s.LogLevel {
	case "silent", "error", "warning", "info", "debug":
		return nil
	default:
		return invalidGeneralSetting()
	}
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
	cfg["tcp-concurrent"] = s.TCPConcurrent == "on"
	cfg["disable-keep-alive"] = s.DisableKeepAlive == "on"
	cfg["find-process-mode"] = s.FindProcessMode
	cfg["etag-support"] = s.ETagSupport == "on"
	return nil
}
