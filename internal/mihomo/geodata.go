package mihomo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"gopkg.in/yaml.v3"
)

const geoRelease = "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/"

func DefaultGeoURLs() map[string]string {
	return map[string]string{
		"mmdb": geoRelease + "geoip.metadb", "geoip": geoRelease + "geoip.dat",
		"geosite": geoRelease + "geosite.dat", "asn": geoRelease + "GeoLite2-ASN.mmdb",
	}
}

// GeoSources identifies sources without retaining URL credentials in a journal.
// Omitted fields have the same defaults as the managed runtime configuration.
func GeoSources(raw []byte) (map[string]string, error) {
	defaults := DefaultGeoURLs()
	var cfg struct {
		URLs map[string]string `yaml:"geox-url"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, diagnostic.New("diagnostic.geo.sources", "invalid GEO data sources")
	}
	for kind, url := range defaults {
		if selected, ok := cfg.URLs[kind]; ok {
			url = selected
		}
		sum := sha256.Sum256([]byte(kind + "\x00" + url))
		defaults[kind] = hex.EncodeToString(sum[:])
	}
	return defaults, nil
}

// CopyValidationAssets uses the same identity as -t. In particular, root
// must not make protected file contents accessible to the service by copying
// entries from its writable runtime. cp -P copies links without following
// them. The caller checks link containment before invoking the core.
func CopyValidationAssets(ctx context.Context, paths []string, dest string) error {
	if len(paths) == 0 {
		return ctx.Err()
	}
	args := append([]string{"-R", "-P"}, paths...)
	cmd := exec.CommandContext(ctx, "/bin/cp", append(args, dest)...)
	if identity, ok := ctx.Value(executionIdentityKey{}).(executionIdentity); ok {
		if err := setExecutionIdentity(cmd, identity, true); err != nil {
			return err
		}
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return diagnostic.New("diagnostic.geo.copy", "could not copy runtime assets for isolated configuration validation")
	}
	return nil
}
