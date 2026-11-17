package tui

import (
	"regexp"
	"strings"
)

func compileSearchMatcher(query string, caseSensitive, wholeWord, regular bool) (func(string) bool, error) {
	pattern := strings.TrimSpace(query)
	if pattern == "" {
		return func(string) bool { return true }, nil
	}
	if !regular {
		pattern = regexp.QuoteMeta(pattern)
	}
	if wholeWord {
		// Boundaries belong to the expression so alternatives can satisfy them:
		// a|ab must still match the complete word "ab". Go's \b is ASCII-only.
		pattern = `(?:^|[^\p{L}\p{N}_])(?:` + pattern + `)(?:$|[^\p{L}\p{N}_])`
	}
	if !caseSensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return re.MatchString, nil
}
