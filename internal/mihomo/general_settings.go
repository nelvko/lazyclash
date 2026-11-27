package mihomo

import (
	"cmp"
	"math"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// GeneralSettings belongs to one managed instance and applies to every profile.
type GeneralSettings struct {
	IPv6              string `json:"ipv6"`
	UnifiedDelay      string `json:"unified_delay"`
	LogLevel          string `json:"log_level"`
	TCPConcurrent     string `json:"tcp_concurrent"`
	DisableKeepAlive  string `json:"disable_keep_alive"`
	KeepAliveIdle     string `json:"keep_alive_idle"`
	KeepAliveInterval string `json:"keep_alive_interval"`
	FindProcessMode   string `json:"find_process_mode"`
	ETagSupport       string `json:"etag_support"`
	GeoAutoUpdate     string `json:"geo_auto_update"`
	GeoUpdateInterval string `json:"geo_update_interval"`
	GeodataLoader     string `json:"geodata_loader"`
	StoreFakeIP       string `json:"store_fake_ip"`
}

func DefaultGeneralSettings() GeneralSettings {
	return GeneralSettings{
		IPv6: "on", UnifiedDelay: "off", LogLevel: "info",
		TCPConcurrent: "off", DisableKeepAlive: "off", FindProcessMode: "strict", ETagSupport: "on",
		KeepAliveIdle: "default", KeepAliveInterval: "default",
		GeoAutoUpdate: "off", GeoUpdateInterval: "24", GeodataLoader: "inherit", StoreFakeIP: "off",
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
		IPv6              bool   `yaml:"ipv6"`
		UnifiedDelay      bool   `yaml:"unified-delay"`
		LogLevel          string `yaml:"log-level"`
		TCPConcurrent     bool   `yaml:"tcp-concurrent"`
		DisableKeepAlive  bool   `yaml:"disable-keep-alive"`
		FindProcessMode   string `yaml:"find-process-mode"`
		ETagSupport       bool   `yaml:"etag-support"`
		KeepAliveIdle     int    `yaml:"keep-alive-idle"`
		KeepAliveInterval int    `yaml:"keep-alive-interval"`
		GeoAutoUpdate     bool   `yaml:"geo-auto-update"`
		GeoUpdateInterval int    `yaml:"geo-update-interval"`
		GeodataLoader     string `yaml:"geodata-loader"`
		Profile           struct {
			StoreFakeIP bool `yaml:"store-fake-ip"`
		} `yaml:"profile"`
	}{
		IPv6: defaults.IPv6 == "on", UnifiedDelay: defaults.UnifiedDelay == "on", LogLevel: defaults.LogLevel,
		TCPConcurrent: defaults.TCPConcurrent == "on", DisableKeepAlive: defaults.DisableKeepAlive == "on",
		FindProcessMode: defaults.FindProcessMode, ETagSupport: defaults.ETagSupport == "on",
		GeoUpdateInterval: 24, GeodataLoader: defaults.GeodataLoader,
	}
	if err := yaml.Unmarshal(raw, &applied); err != nil {
		return s, invalidGeneralSetting()
	}
	// Mihomo accepts memc as an alias. With a nonpositive interval its
	// startup code does not register an updater, even if the switch is on.
	if applied.GeodataLoader == "memc" {
		applied.GeodataLoader = "memconservative"
	}
	if applied.GeoUpdateInterval <= 0 {
		applied.GeoAutoUpdate = false
		applied.GeoUpdateInterval = 24
	}
	s.IPv6 = cmp.Or(s.IPv6, toggleValue(applied.IPv6))
	s.UnifiedDelay = cmp.Or(s.UnifiedDelay, toggleValue(applied.UnifiedDelay))
	s.LogLevel = cmp.Or(s.LogLevel, strings.ToLower(applied.LogLevel))
	s.TCPConcurrent = cmp.Or(s.TCPConcurrent, toggleValue(applied.TCPConcurrent))
	s.DisableKeepAlive = cmp.Or(s.DisableKeepAlive, toggleValue(applied.DisableKeepAlive))
	s.FindProcessMode = cmp.Or(s.FindProcessMode, strings.ToLower(applied.FindProcessMode))
	s.ETagSupport = cmp.Or(s.ETagSupport, toggleValue(applied.ETagSupport))
	s.KeepAliveIdle = cmp.Or(s.KeepAliveIdle, appliedKeepAlive(applied.KeepAliveIdle))
	s.KeepAliveInterval = cmp.Or(s.KeepAliveInterval, appliedKeepAlive(applied.KeepAliveInterval))
	s.GeoAutoUpdate = cmp.Or(s.GeoAutoUpdate, toggleValue(applied.GeoAutoUpdate))
	s.GeoUpdateInterval = cmp.Or(s.GeoUpdateInterval, strconv.Itoa(applied.GeoUpdateInterval))
	s.GeodataLoader = cmp.Or(s.GeodataLoader, strings.ToLower(applied.GeodataLoader))
	s.StoreFakeIP = cmp.Or(s.StoreFakeIP, toggleValue(applied.Profile.StoreFakeIP))
	return s, s.validate()
}

func appliedKeepAlive(seconds int) string {
	if seconds <= 0 {
		return "default"
	}
	return strconv.Itoa(seconds)
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
	case "keep-alive-idle":
		next.KeepAliveIdle = canonicalInterval(cmp.Or(value, "default"))
	case "keep-alive-interval":
		next.KeepAliveInterval = canonicalInterval(cmp.Or(value, "default"))
	case "find-process-mode":
		next.FindProcessMode = value
	case "etag-support":
		next.ETagSupport = value
	case "geo-auto-update":
		next.GeoAutoUpdate = value
	case "geo-update-interval":
		next.GeoUpdateInterval = canonicalInterval(value)
	case "geodata-loader":
		next.GeodataLoader = value
	case "profile.store-fake-ip":
		next.StoreFakeIP = value
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
	for _, value := range [...]string{s.IPv6, s.UnifiedDelay, s.TCPConcurrent, s.DisableKeepAlive, s.ETagSupport, s.GeoAutoUpdate, s.StoreFakeIP} {
		if value != "on" && value != "off" {
			return invalidGeneralSetting()
		}
	}
	for _, v := range [...]string{s.KeepAliveIdle, s.KeepAliveInterval} {
		if v != "default" && !positiveDuration(v, time.Second) {
			return invalidGeneralSetting()
		}
	}
	switch s.GeodataLoader {
	case "inherit", "memconservative", "standard":
	default:
		return invalidGeneralSetting()
	}
	if !positiveDuration(s.GeoUpdateInterval, time.Hour) {
		return invalidGeneralSetting()
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

func positiveDuration(value string, unit time.Duration) bool {
	n, err := strconv.ParseUint(value, 10, 63)
	return err == nil && n > 0 && n <= uint64(math.MaxInt64/unit)
}

// Go's decimal parser and Mihomo's integer settings treat leading zeros as
// insignificant. Keep one representation so repeating an edit is a no-op.
func canonicalInterval(value string) string {
	if n, err := strconv.ParseUint(value, 10, 63); err == nil {
		return strconv.FormatUint(n, 10)
	}
	return value
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

// WithProfileGeodataLoader restores the pinned profile's value or omission
// without rolling back other live settings edited after that profile was applied.
func WithProfileGeodataLoader(deployed, profile []byte) ([]byte, error) {
	cfg, err := decodeConfig(deployed)
	if err != nil {
		return nil, err
	}
	inherited, err := decodeConfig(profile)
	if err != nil {
		return nil, err
	}
	if loader, exists := inherited["geodata-loader"]; exists {
		cfg["geodata-loader"] = loader
	} else {
		delete(cfg, "geodata-loader")
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
	for key, value := range map[string]string{"keep-alive-idle": s.KeepAliveIdle, "keep-alive-interval": s.KeepAliveInterval} {
		if value == "default" {
			delete(cfg, key)
		} else {
			cfg[key], _ = strconv.Atoi(value)
		}
	}
	cfg["geo-auto-update"] = s.GeoAutoUpdate == "on"
	cfg["geo-update-interval"], _ = strconv.Atoi(s.GeoUpdateInterval)
	if s.GeodataLoader != "inherit" {
		cfg["geodata-loader"] = s.GeodataLoader
	}
	profile, ok := cfg["profile"].(map[string]any)
	if !ok {
		if cfg["profile"] != nil {
			return invalidGeneralSetting()
		}
		profile = make(map[string]any)
		cfg["profile"] = profile
	}
	profile["store-fake-ip"] = s.StoreFakeIP == "on"
	return nil
}
