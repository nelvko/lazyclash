// Package i18n localizes presentation text without changing machine data.
package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

const (
	English = "en"
	Chinese = "zh-Hans"
)

//go:embed locales/*.json
var resources embed.FS

type catalog struct {
	bundle     *goi18n.Bundle
	localizers map[string]*goi18n.Localizer
	english    map[string]string
	static     map[string]string
}

var messages = sync.OnceValue(func() catalog {
	c := catalog{bundle: goi18n.NewBundle(language.English), english: map[string]string{}, static: map[string]string{}}
	paths, err := fs.Glob(resources, "locales/*.json")
	if err != nil {
		panic(err)
	}
	for _, path := range paths {
		raw, err := resources.ReadFile(path)
		if err != nil {
			panic(err)
		}
		var entries map[string]string
		if err := json.Unmarshal(raw, &entries); err != nil {
			panic(fmt.Errorf("%s: %w", path, err))
		}
		lang := language.English
		if strings.HasPrefix(path, "locales/zh-Hans.") {
			lang = language.SimplifiedChinese
		}
		for id, text := range entries {
			if lang == language.English {
				if _, duplicate := c.english[id]; duplicate {
					panic("duplicate message: " + id)
				}
				c.english[id] = text
				// Legacy snapshot fields contain rendered English diagnostics. Only
				// fixed, owned diagnostics are supported here; never rewrite user data.
				if strings.HasPrefix(id, "diagnostic.") && !strings.Contains(text, "%") {
					c.static[text] = id
				}
			}
			if err := c.bundle.AddMessages(lang, &goi18n.Message{ID: id, Other: text}); err != nil {
				panic(err)
			}
		}
	}
	c.localizers = map[string]*goi18n.Localizer{
		English: goi18n.NewLocalizer(c.bundle, English),
		Chinese: goi18n.NewLocalizer(c.bundle, Chinese, English),
	}
	return c
})

// Localizer is immutable. Its zero value selects English, including in tests.
type Localizer struct{ lang string }

func New(lang string) Localizer {
	normalized, ok := Normalize(lang)
	if !ok || normalized == "auto" {
		normalized = English
	}
	return Localizer{lang: normalized}
}

func (l Localizer) Language() string {
	if l.lang == Chinese {
		return Chinese
	}
	return English
}

func (l Localizer) T(id string, args ...any) string {
	c := messages()
	text, err := c.localizers[l.Language()].Localize(&goi18n.LocalizeConfig{MessageID: id})
	if text == "" || err != nil {
		text = c.english[id]
		if text == "" {
			text = id
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(text, args...)
	}
	return text
}

func (l Localizer) Detail(text string) string {
	if id := messages().static[text]; id != "" {
		return l.T(id)
	}
	return text
}

// Diagnostic preserves legacy machine text while rendering its original error.
// A mismatch means the observation has since been replaced by a newer message.
func (l Localizer) Diagnostic(text string, cause error) string {
	if cause != nil && cause.Error() == text {
		return l.Error(cause)
	}
	return l.Detail(text)
}

func (l Localizer) Error(err error) string {
	if err == nil {
		return ""
	}
	if l.Language() == English {
		return err.Error()
	}
	if safe, ok := err.(interface{ Diagnostic() error }); ok {
		return l.Error(safe.Diagnostic())
	}
	if d, ok := err.(*diagnostic.Error); ok {
		format := messages().english[d.Code]
		if format == "" {
			return l.Detail(err.Error())
		}
		format = l.T(d.Code)
		args := append([]any(nil), d.Args...)
		for i, arg := range args {
			if cause, ok := arg.(error); ok {
				args[i] = errors.New(l.Error(cause))
			}
		}
		if len(args) == 0 {
			return format
		}
		// fmt.Errorf handles %w while formatting a translated error chain.
		return fmt.Errorf(format, args...).Error()
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var parts []string
		for _, cause := range joined.Unwrap() {
			parts = append(parts, l.Error(cause))
		}
		return strings.Join(parts, "\n")
	}
	if errors.Is(err, context.Canceled) {
		return l.T("diagnostic.canceled")
	}
	return l.Detail(err.Error())
}
