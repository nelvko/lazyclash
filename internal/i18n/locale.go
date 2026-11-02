package i18n

import (
	"strings"

	"golang.org/x/text/language"
)

// Normalize accepts the two supported languages and common POSIX locale forms.
func Normalize(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "auto") {
		return "auto", true
	}
	value, _, _ = strings.Cut(value, ".")
	value, _, _ = strings.Cut(value, "@")
	if value == "C" || value == "POSIX" {
		return English, true
	}
	tag, err := language.Parse(strings.ReplaceAll(value, "_", "-"))
	if err != nil {
		return "", false
	}
	base, _ := tag.Base()
	switch base.String() {
	case "en":
		return English, true
	case "zh":
		return Chinese, true
	default:
		return "", false
	}
}

// Resolve gives explicit choices precedence over a saved preference and the OS.
func Resolve(explicit, preference string, getenv func(string) string) string {
	for _, value := range []string{explicit, getenv("LAZYCLASH_LANG"), preference} {
		if normalized, ok := Normalize(value); ok && normalized != "auto" {
			return normalized
		}
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := getenv(key); value != "" {
			if normalized, ok := Normalize(value); ok && normalized != "auto" {
				return normalized
			}
			return English
		}
	}
	return English
}
