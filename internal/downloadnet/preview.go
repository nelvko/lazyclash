package downloadnet

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var displayVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)
var displayAsset = regexp.MustCompile(`^mihomo[-a-zA-Z0-9_.]*\.gz$`)

// FilePreview shows the effective release-file address, including the complete
// nested official URL used by prefix accelerators. It is display-only: unknown
// static path segments are visibly masked, userinfo is removed, and no query
// values appear. AssetURL remains the exact address used for the request.
func (c Config) FilePreview(original, version, asset string) (string, error) {
	// Source previews remain useful while a custom connection-proxy field is
	// being edited. Connection validation belongs to Test/Apply, not this view.
	source := c.Normalized()
	source.ProxyMode, source.ProxyURL = "none", ""
	actual, err := source.AssetURL(original, version, asset)
	if err != nil {
		return "", err
	}
	switch source.Source {
	case "prefix":
		prefix, err := previewURL(source.Prefix, version, asset)
		if err != nil {
			return "", err
		}
		originalPreview, err := previewURL(original, version, asset)
		if err != nil {
			return "", err
		}
		// Deliberately mirror AssetURL's string concatenation. path.Join would
		// destroy the inner https:// and misrepresent the actual download request.
		return strings.TrimRight(prefix, "/") + "/" + originalPreview, nil
	default:
		return previewURL(actual, version, asset)
	}
}

func previewURL(raw, version, asset string) (string, error) {
	u, err := webURL(raw)
	if err != nil {
		return "", errors.New("cannot preview an invalid release-file URL")
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
	case "", "proxy", "github", "github.com", "gh", "cache", "MetaCubeX", "mihomo", "releases", "release", "download", "downloads", "assets", "files", "linux", "stable", "latest", "{version}", "{asset}":
		return true
	}
	// Only release-like values supplied by the resolved release may be exposed.
	// A static token that merely resembles a version or filename is still masked.
	if part == version && displayVersion.MatchString(part) {
		return true
	}
	return part == asset && len(part) <= 160 && displayAsset.MatchString(part)
}
