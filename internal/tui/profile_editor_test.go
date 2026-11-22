package tui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
)

func openedEditorForTest(t *testing.T, m Model, id string, kind app.ProfileDocumentKind) Model {
	t.Helper()
	m.editorPreference = "vi"
	read := m.openProfileEditor(id, kind)
	msg := read().(profileEditorOpenedMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if cmd := m.acceptProfileEditor(msg); cmd == nil {
		t.Fatalf("no editor command: %s", m.failure)
	}
	m.layout()
	t.Cleanup(msg.state.cleanup)
	return m
}

func TestProfileEditorNoChangeDoesNotSaveOrRefresh(t *testing.T) {
	for _, kind := range []app.ProfileDocumentKind{app.ProfileOverrides, app.ProfileConfiguration} {
		m, b := profileClickModel(t, true, true, true, false)
		b.getDocument = func(_ context.Context, id string, k app.ProfileDocumentKind) (app.ProfileDocument, error) {
			return app.ProfileDocument{ID: id, Kind: k, Bytes: []byte("rules: []\n")}, nil
		}
		b.saveDocument = func(context.Context, app.ProfileDocument, []byte) error {
			t.Fatal("unchanged document reached backend")
			return nil
		}
		m = openedEditorForTest(t, m, "p-stable-profile-id", kind)
		state := m.profileEditor
		for name, mode := range map[string]os.FileMode{state.path: 0600, state.tempDir: 0700} {
			st, err := os.Stat(name)
			if err != nil || st.Mode().Perm() != mode {
				t.Fatalf("unexpected editor permissions for %q", name)
			}
		}
		if cmd := m.finishProfileEditor(readEditorOutput(m.id, state.path)); cmd != nil || m.busy || m.form != "" {
			t.Fatal("unchanged exit queued a backend operation")
		}
		if _, err := os.Stat(state.tempDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("private editor directory remained after unchanged exit")
		}
	}
}

func TestProfileEditorRetainsWorkOnSaveFailureAndRetry(t *testing.T) {
	before := []byte("rules: []\n")
	edited := []byte("rules: ['MATCH,DIRECT']\n")
	for _, local := range []bool{false, true} {
		m, b := profileClickModel(t, true, true, true, false)
		file := ""
		if local {
			file = filepath.Join(t.TempDir(), "my config.yaml")
			if err := os.WriteFile(file, before, 0600); err != nil {
				t.Fatal(err)
			}
		}
		b.getDocument = func(_ context.Context, id string, kind app.ProfileDocumentKind) (app.ProfileDocument, error) {
			return app.ProfileDocument{ID: id, Kind: kind, Bytes: before, LocalPath: file, Remote: !local}, nil
		}
		calls := 0
		b.saveDocument = func(_ context.Context, d app.ProfileDocument, raw []byte) error {
			calls++
			if !bytes.Equal(d.Bytes, before) || !bytes.Equal(raw, edited) {
				t.Fatal("saved wrong candidate or lost original revision")
			}
			if calls == 1 {
				return errors.New("runtime rejected candidate")
			}
			return nil
		}
		m = openedEditorForTest(t, m, "p-stable-profile-id", app.ProfileConfiguration)
		state := m.profileEditor
		if local && (state.path != file || state.tempDir != "") {
			t.Fatal("local source did not use its original path")
		}
		if err := os.WriteFile(state.path, edited, 0600); err != nil {
			t.Fatal(err)
		}
		save := m.finishProfileEditor(readEditorOutput(m.id, state.path))
		m = finishProfileJob(t, m, save)
		if m.form != "profile-document" || !strings.Contains(m.failure, "rejected") {
			t.Fatal("failed save dismissed recovery dialog", m.failure)
		}
		m, _ = update(m, special(tea.KeyEscape))
		if m.form != "profile-document" {
			t.Fatal("escape discarded user edits")
		}
		for _, size := range [][2]int{{160, 40}, {60, 20}, {40, 16}, {20, 7}} {
			m, _ = update(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			assertCanvas(t, m.screen, size[0], size[1])
			assertRegions(t, m.targets, size[0], size[1])
		}
		m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 32})
		if view := ansi.Strip(m.screen); !strings.Contains(view, "Continue editing") || !strings.Contains(view, "Retry save") {
			t.Fatalf("missing recovery controls: %s", view)
		}
		retry := m.profileEditorActivate("profile-editor.retry")
		save = m.finishProfileEditor(retry().(profileEditorExitedMsg))
		m = finishProfileJob(t, m, save)
		if calls != 2 || m.form != "" || m.failure != "" {
			t.Fatal("retry did not finish the same edit", m.failure)
		}
		if local {
			got, err := os.ReadFile(file)
			if err != nil || !bytes.Equal(edited, got) {
				t.Fatal("local source removed after save")
			}
		} else if _, err := os.Stat(state.tempDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("private editor directory retained after success")
		}
	}
}

func TestProfileEditorCloseOnlyRemovesItsPrivateWorkingDirectory(t *testing.T) {
	m, b := profileClickModel(t, true, false, false, false)
	b.getDocument = func(_ context.Context, id string, kind app.ProfileDocumentKind) (app.ProfileDocument, error) {
		return app.ProfileDocument{ID: id, Kind: kind, Bytes: []byte("rules: []")}, nil
	}
	m = openedEditorForTest(t, m, "p-stable-profile-id", app.ProfileConfiguration)
	state := m.profileEditor
	m.finishProfileEditor(profileEditorExitedMsg{request: m.id, err: errors.New("editor failed")})
	if err := os.WriteFile(state.path+".swp", nil, 0600); err != nil {
		t.Fatal(err)
	}
	m.profileEditorActivate("profile-editor.close")
	if _, err := os.Stat(state.tempDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("editor working or swap files retained after discard")
	}
}

func TestProfileOverrideImportRetainsPathOnFailure(t *testing.T) {
	m, b := profileClickModel(t, true, true, true, false)
	file := filepath.Join(t.TempDir(), "overrides.yaml")
	if err := os.WriteFile(file, []byte("rules: []"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	b.setOverrides = func(context.Context, string, []byte) error {
		calls++
		if calls == 1 {
			return errors.New("invalid override")
		}
		return nil
	}
	m.openProfileEdit("profile-overrides", "p-stable-profile-id")
	m.input.SetValue(file)
	for i := 0; i < 2; i++ {
		cmd := m.formKey(special(tea.KeyEnter))
		m = finishProfileJob(t, m, cmd)
		if i == 0 && (m.form != "profile-overrides" || m.failure == "" || m.input.Value() != file) {
			t.Fatal("import error discarded editable path")
		}
	}
	if calls != 2 || m.form != "" || m.failure != "" {
		t.Fatal("second import failed")
	}
}

func TestInterruptedProfileEditorQuitsAndKeepsPotentialEdits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changed bool
		exitErr error
		keep    bool
	}{
		{"unchanged", false, nil, false}, {"changed", true, nil, true}, {"failed with possible swap", false, errors.New("signal"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, b := profileClickModel(t, true, true, true, false)
			b.getDocument = func(_ context.Context, id string, k app.ProfileDocumentKind) (app.ProfileDocument, error) {
				return app.ProfileDocument{ID: id, Kind: k, Bytes: []byte("rules: []\n")}, nil
			}
			m = openedEditorForTest(t, m, "p-stable-profile-id", app.ProfileConfiguration)
			state := m.profileEditor
			if tc.changed {
				if err := os.WriteFile(state.path, []byte("# edited\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			m, _ = update(m, parentCanceledMsg{})
			msg := readEditorOutput(m.id, state.path)
			msg.err = tc.exitErr
			quit := m.finishProfileEditor(msg)
			if !m.quitting || m.busy || quit == nil {
				t.Fatal("interrupted editor did not finish shutdown")
			}
			if _, err := os.Stat(state.tempDir); (err == nil) != tc.keep {
				t.Fatalf("directory recovery mismatch: %v", err)
			}
		})
	}
}
