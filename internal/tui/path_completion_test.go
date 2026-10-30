package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func completionDirectory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, directory := range []string{"config folder", "配置目录", ".private-dir"} {
		if err := os.Mkdir(filepath.Join(dir, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"config.yaml", "config no suffix", "配置.yml", ".private.yaml", "ignore.txt", "unsafe\x1b.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("rules: ['MATCH,DIRECT']\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPathCompletionMatchesDirectoriesFilesAndTypedPrefixes(t *testing.T) {
	dir := completionDirectory(t)
	for _, tc := range []struct {
		prefix string
		want   []string
	}{
		{"config", []string{"config folder/", "config no suffix", "config.yaml"}},
		{"配置", []string{"配置目录/", "配置.yml"}},
		{".private", []string{".private-dir/", ".private.yaml"}},
	} {
		query := dir + "/" + tc.prefix
		result := readPathMatches(query)
		if result.problem != "" || len(result.matches) != len(tc.want) {
			t.Fatalf("%s: unexpected matches: %+v", query, result)
		}
		for i, want := range tc.want {
			if result.matches[i].label != want || result.matches[i].path != dir+"/"+want {
				t.Fatalf("bad match %d: %+v; want %q", i, result.matches[i], want)
			}
		}
	}
	result := readPathMatches(dir + "/")
	for _, match := range result.matches {
		if strings.HasPrefix(match.label, ".") || strings.Contains(match.label, "\x1b") {
			t.Fatal("hidden or unsafe filename offered without a matching prefix", match)
		}
	}

	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(working, dir)
	if err != nil {
		t.Fatal(err)
	}
	result = readPathMatches(relative + "/config.y")
	if len(result.matches) != 1 || result.matches[0].path != relative+"/config.yaml" {
		t.Fatal("relative completion did not preserve the typed path style", result)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	homeRelative, err := filepath.Rel(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	query := "~/" + homeRelative + "/config.y"
	result = readPathMatches(query)
	if len(result.matches) != 1 || result.matches[0].path != "~/"+homeRelative+"/config.yaml" {
		t.Fatal("home-relative completion did not preserve its prefix", result)
	}
}

func TestPathCompletionIsAsynchronousAndDiscardsStaleResults(t *testing.T) {
	dir := completionDirectory(t)
	var completion pathCompletion
	first := completion.request(dir + "/config")
	if first == nil || !completion.loading || len(completion.matches) != 0 {
		t.Fatal("filesystem lookup did not return a command")
	}
	second := completion.request(dir + "/配置")
	if completion.update(first().(pathMatchesMsg)) {
		t.Fatal("stale result replaced the active prefix")
	}
	if !completion.update(second().(pathMatchesMsg)) {
		t.Fatal("current result was discarded")
	}
	selected, ok := completion.selectedPath()
	if !ok || selected != dir+"/配置目录/" {
		t.Fatal("first directory was not selected", selected)
	}
	completion.move(1)
	selected, ok = completion.selectedPath()
	if !ok || selected != dir+"/配置.yml" {
		t.Fatal("navigation did not select the file", selected)
	}
	if cmd := completion.request(dir + "/配置"); cmd != nil {
		t.Fatal("unchanged prefix unnecessarily repeated a directory lookup")
	}
	completion.reset()
	if completion.update(second().(pathMatchesMsg)) {
		t.Fatal("closed import accepted a pending result")
	}
	for _, query := range []string{"", "https://example.invalid/sub", "http://127.0.0.1/config"} {
		if cmd := completion.request(query); cmd != nil || completion.active {
			t.Fatal("URL or empty input initiated a directory lookup")
		}
	}
}

func TestPathCompletionReportsMissingDirectoryAndNoMatches(t *testing.T) {
	dir := completionDirectory(t)
	for _, tc := range []struct {
		query, want string
	}{
		{dir + "/missing/private-token/config", "Directory not found"},
		{dir + "/config.yaml/not-a-directory/", "Cannot read this directory"},
		{dir + "/nothing-matches", "No matching files or folders"},
		{"~other-user/config", "use ~/"},
	} {
		var completion pathCompletion
		cmd := completion.request(tc.query)
		completion.update(cmd().(pathMatchesMsg))
		view := ansi.Strip(strings.Join(completion.view(100, 5), "\n"))
		if !strings.Contains(view, tc.want) || strings.Contains(view, "private-token") {
			t.Fatalf("unexpected path feedback: %q", view)
		}
	}
}

func TestPathCompletionBoundsLargeDirectoriesAndListGeometry(t *testing.T) {
	dir := t.TempDir()
	for index := range maxPathMatches + 1 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-%03d.yaml", index)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var completion pathCompletion
	completion.update(completion.request(dir + "/")().(pathMatchesMsg))
	if len(completion.matches) != maxPathMatches || !completion.limited {
		t.Fatal("large directory was not bounded")
	}
	completion.move(maxPathMatches - 1)
	for _, width := range []int{1, 8, 40} {
		for _, rows := range []int{1, 3, 5} {
			lines := completion.view(width, rows)
			if len(lines) > rows || len(lines) == 0 {
				t.Fatal("candidate list exceeded its height")
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Fatal("candidate list exceeded its width", width, line)
				}
			}
		}
	}
}

func TestResolveLocalPathPreservesURLsAndExpandsLocalPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ input, want string }{
		{"~/配置 folder/config.yaml", filepath.Join(home, "配置 folder/config.yaml")},
		{"~", home},
		{"./config.yaml", filepath.Join(working, "config.yaml")},
		{"../config.yml", filepath.Join(filepath.Dir(working), "config.yml")},
		{"https://example.invalid/sub?token=private#fragment", "https://example.invalid/sub?token=private#fragment"},
	} {
		got, err := resolveLocalPath(tc.input)
		if err != nil || got != tc.want {
			t.Fatal("unexpected path resolution", tc.input, got, err)
		}
	}
	if _, err := resolveLocalPath("~other-user/config.yaml"); err == nil {
		t.Fatal("unsupported home notation was silently treated as a filename")
	}
}
