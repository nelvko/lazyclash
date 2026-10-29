package mihomo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"unicode"
)

// normalizeProviderPaths isolates HTTP caches by provider definition rather
// than trusting source-supplied names in the shared core data directory. File
// providers remain user-owned assets, explicitly relative to that directory.
func normalizeProviderPaths(cfg map[string]any) error {
	for _, kind := range []struct{ field, prefix string }{{"proxy-providers", "proxy"}, {"rule-providers", "rule"}} {
		providers, _ := cfg[kind.field].(map[string]any)
		for name, value := range providers {
			provider, ok := value.(map[string]any)
			if !ok {
				return errors.New("provider definition must be a mapping")
			}
			switch provider["type"] {
			case "http":
				if value, exists := provider["path"]; exists {
					if _, ok := value.(string); !ok {
						return errors.New("HTTP provider path must be a string when present")
					}
				}
				definition := make(map[string]any, len(provider))
				for key, value := range provider {
					if key != "path" {
						definition[key] = value
					}
				}
				identity, err := json.Marshal(struct {
					Kind, Name string
					Definition map[string]any
				}{kind.field, name, definition})
				if err != nil {
					return errors.New("cannot encode provider cache identity")
				}
				// URLs, authorization headers and all other options contribute to
				// identity, but their plaintext never enters a filename or error.
				hash := sha256.Sum256(identity)
				provider["path"] = "providers/" + kind.prefix + "-" + hex.EncodeToString(hash[:]) + ".yaml"
			case "file":
				value, ok := provider["path"].(string)
				if !ok || value == "" || path.IsAbs(value) || strings.ContainsAny(value, "\\:") || strings.IndexFunc(value, unicode.IsControl) >= 0 {
					return errors.New("file provider path must name an asset relative to the core runtime directory")
				}
				for _, part := range strings.Split(value, "/") {
					if part == ".." {
						return errors.New("file provider path cannot traverse outside its runtime asset directory")
					}
				}
				clean := path.Clean(value)
				if clean == "." {
					return errors.New("file provider path must name a local asset file")
				}
				provider["path"] = clean
			}
		}
	}
	return nil
}
