package editor

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestResolveEditorAndPassDocumentAsOneArgument(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"visual editor", "env-editor", "nano", "vi"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("VISUAL", "'visual editor' --wait")
	t.Setenv("EDITOR", "env-editor --clean")
	file := "/private/my files/$(config).yaml"
	check := func(setting string, want ...string) {
		t.Helper()
		cmd, err := Command(t.Context(), setting, file)
		if err != nil {
			t.Fatal(err)
		}
		want[0] = filepath.Join(dir, want[0])
		if !slices.Equal(cmd.Args, append(want, file)) {
			t.Fatalf("unexpected arguments %q", cmd.Args)
		}
	}
	check("vi -f", "vi", "-f")
	check("", "visual editor", "--wait")
	check(filepath.Join(dir, "visual editor"), "visual editor")
	t.Setenv("VISUAL", "unavailable")
	check("", "env-editor", "--clean")
	t.Setenv("EDITOR", "")
	check("", "nano")
	if err := os.Remove(filepath.Join(dir, "nano")); err != nil {
		t.Fatal(err)
	}
	check("", "vi")
	if _, err := Command(t.Context(), "missing-explicit-editor", file); err == nil {
		t.Fatal("lost explicit editor selection")
	}
	if err := os.Remove(filepath.Join(dir, "vi")); err != nil {
		t.Fatal(err)
	}
	if _, err := Command(t.Context(), "", file); err == nil {
		t.Fatal("no available editor")
	}
}
