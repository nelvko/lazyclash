package configuration

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const MaxNameRunes = 128
const FallbackName = "Remote Subscription"

const (
	NameCustom   = "custom"
	NameProvider = "provider"
	NameURL      = "url"
	NameFallback = "fallback"
)

// NameCandidate is the original, cleaned label before disambiguation. Keeping
// it separate from the displayed label prevents repeated update suffixes.
type NameCandidate struct {
	Name   string
	Source string
}

// CleanName prepares untrusted text for display, never for use as a file path.
func CleanName(value string) string {
	value = strings.ToValidUTF8(ansi.Strip(value), "")
	value = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, value)
	value = norm.NFC.String(strings.Join(strings.Fields(value), " "))
	return truncateName(value, MaxNameRunes)
}

func truncateName(value string, limit int) string {
	if utf8.RuneCountInString(value) > limit {
		value = string([]rune(value)[:limit])
	}
	return strings.TrimSpace(value)
}

// ResolveName shares the same import precedence for remote and local sources.
// Malformed optional headers are ignored and never become download errors.
func ResolveName(explicit string, headers http.Header, address string) NameCandidate {
	if name := CleanName(explicit); name != "" {
		return NameCandidate{Name: name, Source: NameCustom}
	}
	if name := ProviderName(headers); name != "" {
		return NameCandidate{Name: name, Source: NameProvider}
	}
	if parsed, err := url.Parse(address); err == nil {
		parts := strings.Split(parsed.EscapedPath(), "/")
		for i := len(parts) - 1; i >= 0; i-- {
			part, err := url.PathUnescape(parts[i])
			if err != nil || strings.TrimSpace(part) == "" {
				continue
			}
			part = stripYAMLSuffix(CleanName(part))
			if part != "" && part != "." && part != ".." {
				return NameCandidate{Name: part, Source: NameURL}
			}
		}
		if name := CleanName(parsed.Hostname()); name != "" {
			return NameCandidate{Name: name, Source: NameURL}
		}
	}
	return NameCandidate{Name: FallbackName, Source: NameFallback}
}

func stripYAMLSuffix(value string) string {
	for _, suffix := range []string{".yaml", ".yml"} {
		if len(value) >= len(suffix) && strings.EqualFold(value[len(value)-len(suffix):], suffix) {
			return strings.TrimSpace(value[:len(value)-len(suffix)])
		}
	}
	return value
}

// ProviderName considers only response headers, so a refresh without a valid
// provider title can retain its existing name instead of resetting to the URL.
func ProviderName(headers http.Header) string {
	for _, value := range headerValues(headers, "profile-title") {
		value = strings.TrimSpace(value)
		if len(value) >= 7 && strings.EqualFold(value[:7], "base64:") {
			value = decodeTitle(strings.TrimSpace(value[7:]))
		}
		if utf8.ValidString(value) {
			if name := CleanName(value); name != "" {
				return name
			}
		}
	}
	for _, value := range headerValues(headers, "content-disposition") {
		parameters := dispositionParameters(value)
		if filename := decodeExtendedFilename(parameters["filename*"]); filename != "" {
			if name := filenameLabel(filename); name != "" {
				return name
			}
		}
		if filename := parameters["filename"]; utf8.ValidString(filename) {
			if name := filenameLabel(filename); name != "" {
				return name
			}
		}
	}
	return ""
}

func headerValues(headers http.Header, name string) []string {
	var values []string
	for key, entries := range headers {
		if strings.EqualFold(key, name) {
			values = append(values, entries...)
		}
	}
	return values
}

func decodeTitle(value string) string {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, err := encoding.DecodeString(value)
		if err == nil && utf8.Valid(decoded) {
			return string(decoded)
		}
	}
	return ""
}

// Parse parameters independently so an invalid filename* does not discard an
// otherwise valid legacy filename. Quoted semicolons and escaped quotes stay
// inside their parameter value before the standard MIME parser decodes them.
func dispositionParameters(value string) map[string]string {
	parts := []string{}
	start, quoted, escaped := 0, false, false
	for i, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if quoted && r == '\\' {
			escaped = true
		} else if r == '"' {
			quoted = !quoted
		} else if r == ';' && !quoted {
			parts = append(parts, value[start:i])
			start = i + 1
		}
	}
	parts = append(parts, value[start:])
	parameters := make(map[string]string)
	for _, part := range parts[1:] {
		key, raw, found := strings.Cut(strings.TrimSpace(part), "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if !found || (key != "filename" && key != "filename*") {
			continue
		}
		raw = strings.TrimSpace(raw)
		_, parsed, err := mime.ParseMediaType("attachment; filename=" + raw)
		if err == nil && parameters[key] == "" {
			parameters[key] = parsed["filename"]
		} else if err != nil && parameters[key] == "" && raw != "" && !strings.ContainsRune(raw, '"') && utf8.ValidString(raw) {
			// Some subscriptions send an unquoted UTF-8 filename with spaces.
			parameters[key] = raw
		}
	}
	return parameters
}

func decodeExtendedFilename(value string) string {
	parts := strings.SplitN(value, "'", 3)
	if len(parts) != 3 {
		return ""
	}
	decoded, err := url.PathUnescape(parts[2])
	if err != nil {
		return ""
	}
	switch strings.ToLower(parts[0]) {
	case "utf-8", "utf8":
		if utf8.ValidString(decoded) {
			return decoded
		}
	case "iso-8859-1":
		runes := make([]rune, len(decoded))
		for i, b := range []byte(decoded) {
			runes[i] = rune(b)
		}
		return string(runes)
	}
	return ""
}

func filenameLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	value = value[strings.LastIndex(value, "/")+1:]
	value = CleanName(value)
	if value == "." || value == ".." {
		return ""
	}
	return value
}

// NameKey defines display-name equality independently of stable profile IDs.
func NameKey(value string) string {
	return cases.Fold().String(CleanName(value))
}

// UniqueName disambiguates display names without changing profile identity.
// excludeID lets updates recompute from the original base, not a prior suffix.
func UniqueName(candidate string, profiles map[string]ProfileRecord, excludeID string) string {
	base := CleanName(candidate)
	if base == "" {
		base = FallbackName
	}
	used := map[string]bool{NameKey("default"): true}
	for id, record := range profiles {
		if id == excludeID {
			continue
		}
		name := record.Name
		if name == "" {
			name = id
		}
		used[NameKey(name)] = true
	}
	if !used[NameKey(base)] {
		return base
	}
	for number := 1; ; number++ {
		suffix := fmt.Sprintf(" (%d)", number)
		name := truncateName(base, MaxNameRunes-utf8.RuneCountInString(suffix)) + suffix
		if !used[NameKey(name)] {
			return name
		}
	}
}
