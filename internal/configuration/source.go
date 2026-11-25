package configuration

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// IsRemoteSource reports whether the address uses a supported HTTP scheme.
func IsRemoteSource(address string) bool {
	return strings.HasPrefix(address, "https://") || strings.HasPrefix(address, "http://")
}

// SourceUserAgent returns the optional request header for a remote source.
// A local file has no HTTP request options to retain.
func SourceUserAgent(address, value string) (string, error) {
	if value == "" || !IsRemoteSource(address) {
		return "", nil
	}
	if !utf8.ValidString(value) || strings.ContainsFunc(value, func(r rune) bool { return r != '\t' && unicode.IsControl(r) }) {
		return "", diagnostic.New("diagnostic.source.invalidUserAgent", "User-Agent must be a valid single-line HTTP header value")
	}
	return strings.Trim(value, " \t"), nil
}
