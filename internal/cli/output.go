package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

func writeJSON(cmd *cobra.Command, v any) error {
	e := json.NewEncoder(cmd.OutOrStdout())
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func reportedError(text string, cause error) error {
	if cause != nil && cause.Error() == text {
		return cause
	}
	return errors.New(text)
}

func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
