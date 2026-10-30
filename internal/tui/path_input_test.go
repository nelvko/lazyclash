package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func acceptPathMatches(t *testing.T, m Model) Model {
	t.Helper()
	if !m.paths.loading {
		t.Fatal("editing a local path did not schedule completion")
	}
	result := readPathMatches(m.paths.query)
	result.generation = m.paths.generation
	m, _ = update(m, result)
	return m
}

func TestImportPathCompletionSelectsWithoutSubmittingAndNavigatesFolders(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "配置 folder")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, "config no suffix")
	if err := os.WriteFile(file, []byte("rules: ['MATCH,DIRECT']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls, imported := 0, ""
	m := ready(&fakeBackend{add: func(_ context.Context, name, source string) error {
		calls++
		imported = source
		if name != "" {
			t.Fatal("local import assigned a custom name")
		}
		return nil
	}})
	m.page = "profiles"
	m.openForm()
	m, _ = update(m, tea.PasteMsg{Content: dir + "/配置"})
	m = acceptPathMatches(t, m)
	if !strings.Contains(ansi.Strip(m.View().Content), "配置 folder/") {
		t.Fatal("matching folder was not listed")
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.input.Value() != folder+"/" || m.form != "source" || calls != 0 {
		t.Fatal("Tab did not complete the directory or submitted an import")
	}
	m = acceptPathMatches(t, m)
	m, _ = update(m, special(tea.KeyEnter))
	if m.form != "source" || calls != 0 {
		t.Fatal("Enter attempted to import a directory")
	}
	m, _ = update(m, special(tea.KeyTab))
	if m.input.Value() != file || calls != 0 {
		t.Fatal("file completion did not preserve spaces and Unicode")
	}
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if calls != 1 || imported != file || m.form != "" || m.failure != "" {
		t.Fatal("Enter did not explicitly import the completed file", imported, m.failure)
	}
}

func TestImportCompletionUsesSelectedMatchAndRejectsClosedResults(t *testing.T) {
	dir := completionDirectory(t)
	m := ready(&fakeBackend{})
	m.openForm()
	m, _ = update(m, tea.PasteMsg{Content: dir + "/config"})
	m = acceptPathMatches(t, m)
	m, _ = update(m, special(tea.KeyDown))
	m, _ = update(m, special(tea.KeyTab))
	if m.input.Value() != filepath.Join(dir, "config no suffix") {
		t.Fatal("Tab did not use the selected file")
	}
	late := readPathMatches(m.paths.query)
	late.generation = m.paths.generation
	m, _ = update(m, special(tea.KeyEsc))
	m, _ = update(m, late)
	if m.paths.active || m.form != "" {
		t.Fatal("closed import accepted a late directory listing")
	}
}

func TestRelativePathImportAndOverrideResolveBeforeCallingBackend(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	raw := []byte("set: {}\n")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(working, file)
	if err != nil {
		t.Fatal(err)
	}
	imports, overrides := 0, 0
	m := ready(&fakeBackend{
		snapshot: app.Snapshot{Profiles: []string{"default", "work"}, ProfileDetails: profileFixtures("default", "work")},
		add: func(_ context.Context, _, path string) error {
			imports++
			if path != file {
				t.Fatal("backend received an unresolved local path", path)
			}
			return nil
		},
		setOverrides: func(_ context.Context, id string, received []byte) error {
			overrides++
			if id != "work" || string(received) != string(raw) {
				t.Fatal("relative override changed its target or content")
			}
			return nil
		},
	})
	m.openForm()
	m.input.SetValue(relative)
	m, cmd := update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	m.openProfileEdit("profile-overrides", "work")
	m.input.SetValue(relative)
	m, cmd = update(m, special(tea.KeyEnter))
	m = finishProfileJob(t, m, cmd)
	if imports != 1 || overrides != 1 || m.failure != "" {
		t.Fatal("relative paths were not accepted", m.failure)
	}
}

func TestImportCompletionFitsNarrowAndShortTerminals(t *testing.T) {
	dir := completionDirectory(t)
	for _, size := range [][2]int{{120, 32}, {60, 20}, {30, 12}, {24, 7}, {20, 7}, {8, 4}} {
		m := ready(&fakeBackend{})
		m.openForm()
		m, _ = update(m, tea.PasteMsg{Content: dir + "/"})
		m = acceptPathMatches(t, m)
		m.failure = "Choose a configuration file"
		m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		assertCanvas(t, m.View().Content, size[0], size[1])
		assertRegions(t, m.targets, size[0], size[1])
		if size[0] >= 24 {
			findTarget(t, m, "import-input")
			if !strings.Contains(ansi.Strip(m.View().Content), "Esc") {
				t.Fatal("short import dialog lost its cancel hint")
			}
		}
	}
}
