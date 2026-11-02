package downloadnet

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

var displayVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)
var displayAsset = regexp.MustCompile(`^mihomo[-a-zA-Z0-9_.]*\.gz$`)

// FilePreview shows the effective release-file address, including the complete
// nested official URL used by prefix accelerators. It is display-only: unknown
// static path segments are visibly masked, userinfo is removed, and no query
// values appear. URL remains the exact address used for the request.
func (c Config) FilePreview(original, version, asset string) (string, error) {
	c = c.Normalized()
	actual, err := c.URL(original)
	if err != nil {
		return "", err
	}
	if c.Method == MethodAccelerated {
		prefix, err := previewURL(c.Prefix, "", "")
		if err != nil {
			return "", err
		}
		originalPreview, err := previewURL(original, version, asset)
		if err != nil {
			return "", err
		}
		// Deliberately mirror URL's string concatenation. path.Join would
		// destroy the inner https:// and misrepresent the actual download request.
		return strings.TrimRight(prefix, "/") + "/" + originalPreview, nil
	}
	return previewURL(actual, version, asset)
}

// Preview also supports version.txt and release API endpoints. Only recognized
// public Mihomo paths may expose the resolved version or asset name.
func (c Config) Preview(original string) (string, error) {
	var version, asset string
	u, err := webURL(original)
	if err != nil {
		return "", diagnostic.New("diagnostic.network.previewURL", "cannot preview an invalid download URL")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if u.Hostname() == "github.com" && len(parts) == 6 && parts[0] == "MetaCubeX" && parts[1] == "mihomo" && parts[2] == "releases" && parts[3] == "download" {
		version, asset = parts[4], parts[5]
	}
	if u.Hostname() == "api.github.com" && len(parts) == 6 && parts[0] == "repos" && parts[1] == "MetaCubeX" && parts[2] == "mihomo" && parts[3] == "releases" && parts[4] == "tags" {
		version = parts[5]
	}
	return c.FilePreview(original, version, asset)
}

func previewURL(raw, version, asset string) (string, error) {
	u, err := webURL(raw)
	if err != nil {
		return "", diagnostic.New("diagnostic.network.previewURL", "cannot preview an invalid download URL")
	}
	parts := strings.Split(u.EscapedPath(), "/")
	for i, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil || !publicDisplaySegment(decoded, version, asset) {
			parts[i] = "[redacted]"
		}
	}
	result := u.Scheme + "://" + u.Host + strings.Join(parts, "/")
	if u.RawQuery != "" || u.ForceQuery {
		result += "?[redacted]"
	}
	return result, nil
}

func publicDisplaySegment(part, version, asset string) bool {
	switch part {
	case "", "proxy", "github", "github.com", "gh", "cache", "repos", "tags", "MetaCubeX", "mihomo", "releases", "release", "download", "downloads", "assets", "files", "linux", "stable", "latest", "version.txt", "Prerelease-Alpha":
		return true
	}
	// Only release-like values supplied by the resolved release may be exposed.
	// A static token that merely resembles a version or filename is still masked.
	if part == version && displayVersion.MatchString(part) {
		return true
	}
	return part == asset && len(part) <= 160 && displayAsset.MatchString(part)
}
