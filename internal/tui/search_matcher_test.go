package tui

import "testing"

func TestSearchMatcherModesAndUnicodeWords(t *testing.T) {
	for _, tc := range []struct {
		name, query, value              string
		caseSensitive, wholeWord, regex bool
		want, invalid                   bool
	}{
		{name: "empty query matches empty value", value: "", want: true},
		{name: "whitespace query matches all", query: " \t\n ", value: "event", regex: true, wholeWord: true, want: true},
		{name: "trim query", query: "  alpha ", value: "ALPHA event", want: true},
		{name: "case sensitive", query: "Alpha", value: "alpha event", caseSensitive: true},
		{name: "literal punctuation", query: "a.b", value: "a.b", want: true},
		{name: "literal punctuation does not expand", query: "a.b", value: "axb"},
		{name: "literal invalid expression", query: "[", value: "[TCP] event", want: true},
		{name: "regular expression", query: "^a.b$", value: "axb", regex: true, want: true},
		{name: "invalid regular expression", query: "[", regex: true, invalid: true},
		{name: "whole word at newline", query: "Alpha", value: "event\nALPHA\nnext", wholeWord: true, want: true},
		{name: "whole word excludes suffix", query: "alpha", value: "alphabet", wholeWord: true},
		{name: "whole Unicode word", query: "中文", value: "🚀中文.example", wholeWord: true, want: true},
		{name: "whole Unicode word excludes prefix", query: "中文", value: "前中文.example", wholeWord: true},
		{name: "whole Unicode word excludes underscore", query: "中文", value: "中文_1.example", wholeWord: true},
		{name: "whole regex alternatives", query: "a|ab", value: "ab", wholeWord: true, regex: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			match, err := compileSearchMatcher(tc.query, tc.caseSensitive, tc.wholeWord, tc.regex)
			if (err != nil) != tc.invalid {
				t.Fatalf("compile error = %v; invalid = %t", err, tc.invalid)
			}
			if tc.invalid {
				if match != nil {
					t.Fatal("invalid expression returned a matcher")
				}
				return
			}
			if got := match(tc.value); got != tc.want {
				t.Fatalf("query %q matching %q = %t; want %t", tc.query, tc.value, got, tc.want)
			}
		})
	}
}
