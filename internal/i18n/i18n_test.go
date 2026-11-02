package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

func TestCatalogsHaveMatchingKeysAndFormatParameters(t *testing.T) {
	catalogs := map[string]map[string]string{English: {}, Chinese: {}}
	paths, err := fs.Glob(resources, "locales/*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		lang := English
		if strings.HasPrefix(path, "locales/zh-Hans.") {
			lang = Chinese
		}
		raw, _ := resources.ReadFile(path)
		var entries map[string]string
		if err := json.Unmarshal(raw, &entries); err != nil {
			t.Fatal(err)
		}
		for key, value := range entries {
			if value == "" {
				t.Errorf("empty message %s in %s", key, path)
			}
			if _, exists := catalogs[lang][key]; exists {
				t.Errorf("duplicate key %s in %s", key, path)
			}
			catalogs[lang][key] = value
		}
	}
	format := regexp.MustCompile(`%([\[][0-9]+[\]])?[-+# 0]*([0-9]+|\*)?(\.([0-9]+|\*))?[a-zA-Z%]`)
	for key, en := range catalogs[English] {
		zh, exists := catalogs[Chinese][key]
		if !exists {
			t.Errorf("missing Chinese translation %s", key)
			continue
		}
		englishArgs, chineseArgs := format.FindAllString(en, -1), format.FindAllString(zh, -1)
		slices.Sort(englishArgs)
		slices.Sort(chineseArgs)
		if !slices.Equal(englishArgs, chineseArgs) {
			t.Errorf("format parameters differ for %s: %v vs %v", key, englishArgs, chineseArgs)
		}
	}
	for key := range catalogs[Chinese] {
		if _, exists := catalogs[English][key]; !exists {
			t.Errorf("missing English source %s", key)
		}
	}
}

func TestLiteralMessageReferencesExist(t *testing.T) {
	for _, pkg := range []string{"tui", "cli"} {
		paths, err := filepath.Glob(filepath.Join("..", pkg, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "T" && selector.Sel.Name != "tr" {
					return true
				}
				literal, ok := call.Args[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				key, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				if _, exists := messages().english[key]; !exists {
					t.Errorf("unknown key %s at %s", key, fset.Position(literal.Pos()))
				}
				return true
			})
		}
	}
}

func TestLocalizersAreIndependentAndErrorsKeepTheirCause(t *testing.T) {
	var wg sync.WaitGroup
	for _, lang := range []string{English, Chinese} {
		wg.Go(func() {
			l := New(lang)
			for range 30 {
				want := "Language saved"
				if lang == Chinese {
					want = "语言已保存"
				}
				if got := l.T("language.saved"); got != want {
					t.Errorf("language leaked: %q", got)
				}
			}
		})
	}
	wg.Wait()
	cause := errors.New("untranslated external failure")
	err := diagnostic.New("language.save_failed", "Could not save language: %w", cause)
	if !errors.Is(err, cause) {
		t.Fatal("error chain lost")
	}
	var typed *diagnostic.Error
	if !errors.As(err, &typed) || typed.Code != "language.save_failed" {
		t.Fatal("message identity lost")
	}
	if got := New(Chinese).Error(err); got != "无法保存语言：untranslated external failure" {
		t.Fatal(got)
	}
	unknown := fmt.Errorf("external path: %s", "/tmp/English name.yaml")
	if got := New(Chinese).Error(unknown); got != unknown.Error() {
		t.Fatal("external diagnostics were rewritten")
	}
	var zero Localizer
	if zero.Language() != English || zero.T("language.saved") != "Language saved" {
		t.Fatal("zero localizer must preserve English")
	}
}

func TestDiagnosticSourcesKeepStableCatalogMessages(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "*", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "New" {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "diagnostic" {
				return true
			}
			id, ok := call.Args[0].(*ast.BasicLit)
			if !ok || id.Kind != token.STRING {
				return true
			}
			key, _ := strconv.Unquote(id.Value)
			english, exists := messages().english[key]
			if !exists {
				t.Errorf("missing diagnostic %s at %s", key, fset.Position(id.Pos()))
				return true
			}
			if source, ok := call.Args[1].(*ast.BasicLit); ok && source.Kind == token.STRING {
				text, _ := strconv.Unquote(source.Value)
				if text != english {
					t.Errorf("stale source %s at %s: %q vs %q", key, fset.Position(source.Pos()), text, english)
				}
			}
			return true
		})
	}
}

func TestDynamicDiagnosticKeepsParametersAndRejectsStaleCause(t *testing.T) {
	err := diagnostic.New("diagnostic.api.http", "Mihomo API returned HTTP %d", 503)
	locale := New(Chinese)
	text := locale.Diagnostic(err.Error(), err)
	if !strings.Contains(text, "503") || strings.Contains(text, "returned") {
		t.Fatal("dynamic diagnostic lost translation or status: " + text)
	}
	if got := locale.Diagnostic("newer external failure", err); got != "newer external failure" {
		t.Fatal("stale cause replaced newer observation")
	}
}

func TestLocaleResolution(t *testing.T) {
	tests := []struct {
		explicit, saved string
		env             map[string]string
		want            string
	}{
		{"en", Chinese, map[string]string{"LAZYCLASH_LANG": Chinese}, English},
		{"", "en", map[string]string{"LAZYCLASH_LANG": "zh_CN.UTF-8"}, Chinese},
		{"", Chinese, map[string]string{"LANG": "en_US.UTF-8"}, Chinese},
		{"", "", map[string]string{"LC_ALL": "C", "LANG": "zh_CN.UTF-8"}, English},
		{"auto", "", map[string]string{"LC_MESSAGES": "zh_CN.UTF-8", "LANG": "en"}, Chinese},
		{"", "", map[string]string{"LC_ALL": "fr_FR.UTF-8", "LANG": "zh_CN.UTF-8"}, English},
		{"", "", map[string]string{}, English},
	}
	for _, tt := range tests {
		if got := Resolve(tt.explicit, tt.saved, func(key string) string { return tt.env[key] }); got != tt.want {
			t.Errorf("%+v got=%s", tt, got)
		}
	}
	for _, value := range []string{"en_US.UTF-8", "zh-CN", "zh-Hans", "zh_TW.UTF-8", "C", "POSIX", "auto"} {
		if _, ok := Normalize(value); !ok {
			t.Errorf("supported locale rejected: %s", value)
		}
	}
	if _, ok := Normalize("invalid-language"); ok {
		t.Fatal("unsupported locale accepted")
	}
}
