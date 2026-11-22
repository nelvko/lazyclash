package tui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/editor"
	"github.com/nelvko/lazyclash/internal/store"
)

type profileEditorState struct {
	document app.ProfileDocument
	path     string
	tempDir  string
	action   int
}

type profileEditorOpenedMsg struct {
	request uint64
	state   profileEditorState
	err     error
}

type profileEditorExitedMsg struct {
	request uint64
	edited  []byte
	err     error
}

func prepareEditorFile(doc app.ProfileDocument) (profileEditorState, error) {
	state := profileEditorState{document: doc, path: doc.LocalPath}
	if doc.LocalPath != "" {
		return state, nil
	}
	// Editors such as vi can create swap and backup files beside the document.
	// Place all of them in the same private directory.
	dir, err := os.MkdirTemp("", "lazyclash-profile-*")
	if err != nil {
		return state, err
	}
	state.tempDir = dir
	state.path = filepath.Join(dir, string(doc.Kind)+".yaml")
	if err = os.WriteFile(state.path, doc.Bytes, 0600); err != nil {
		state.cleanup()
		return profileEditorState{}, err
	}
	return state, nil
}

func (s profileEditorState) cleanup() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

func (m *Model) openProfileEditor(id string, kind app.ProfileDocumentKind) tea.Cmd {
	m.form, m.name = "profile-document", id
	ctx := m.newJob("profile.document.read")
	m.message = m.tr("profile_editor.opening")
	request, backend := m.id, m.backend
	return func() tea.Msg {
		doc, err := backend.GetProfileDocument(ctx, id, kind)
		if err != nil {
			return profileEditorOpenedMsg{request: request, err: err}
		}
		state, err := prepareEditorFile(doc)
		if err == nil {
			err = ctx.Err()
		}
		return profileEditorOpenedMsg{request, state, err}
	}
}

func (m *Model) acceptProfileEditor(msg profileEditorOpenedMsg) tea.Cmd {
	if msg.request != m.id || m.form != "profile-document" {
		msg.state.cleanup()
		return nil
	}
	m.cancel()
	m.busy, m.job = false, ""
	if m.quitting || msg.err != nil {
		msg.state.cleanup()
		m.closeProfileEditor()
		if m.quitting {
			return tea.Quit
		}
		if !errors.Is(msg.err, context.Canceled) {
			m.prepareNotification("profile.document.open", m.tr("profile_editor.opening"))
			m.failure = m.localizeError(msg.err)
		}
		return nil
	}
	m.profileEditor = msg.state
	return m.launchProfileEditor()
}

func (m *Model) launchProfileEditor() tea.Cmd {
	ctx := m.newJob("profile.document.edit")
	command, err := editor.Command(ctx, m.editorPreference, m.profileEditor.path)
	if err != nil {
		m.cancel()
		m.busy, m.job = false, ""
		m.failure = m.localizeError(err)
		return nil
	}
	m.message = m.tr("profile_editor.opening")
	request, path := m.id, m.profileEditor.path
	return tea.ExecProcess(command, func(err error) tea.Msg {
		if err != nil {
			return profileEditorExitedMsg{request: request, err: diagnostic.New("profile_editor.process_failed", "The editor exited with an error. Your file is available for editing or retry.")}
		}
		return readEditorOutput(request, path)
	})
}

func readEditorOutput(request uint64, path string) profileEditorExitedMsg {
	raw, err := store.Read(path)
	if err != nil {
		err = diagnostic.New("profile_editor.read_failed", "Cannot read the edited file. Use a regular YAML file.")
	}
	return profileEditorExitedMsg{request, raw, err}
}

func (m *Model) finishProfileEditor(msg profileEditorExitedMsg) tea.Cmd {
	if msg.request != m.id || m.form != "profile-document" {
		return nil
	}
	m.cancel()
	m.busy, m.job, m.message = false, "", ""
	if m.quitting || m.ctx.Err() != nil {
		// A failed editor can leave a recoverable swap file even when the
		// original bytes appear unchanged. Preserve the whole working directory.
		if msg.err == nil && bytes.Equal(msg.edited, m.profileEditor.document.Bytes) {
			m.closeProfileEditor()
		}
		m.quitting = true
		if m.keyHints.pending {
			return nil
		}
		return tea.Quit
	}
	if msg.err != nil {
		m.failure = m.localizeError(msg.err)
		return nil
	}
	state, raw := m.profileEditor, msg.edited
	if bytes.Equal(raw, state.document.Bytes) {
		m.closeProfileEditor()
		return nil
	}
	label := m.tr("profile_editor.saving")
	success := m.tr("profile_editor.saved")
	if state.document.Remote {
		success += " · " + m.tr("profile_editor.replaced")
	}
	return m.begin(label, success, func(ctx context.Context) error {
		return m.backend.SaveProfileDocument(ctx, state.document, raw)
	}, m.profileNotification("document.save", label, state.document.ID))
}

func (m *Model) closeProfileEditor() {
	id := m.name
	m.profileEditor.cleanup()
	m.profileEditor = profileEditorState{}
	m.form, m.name, m.failure, m.message = "", "", "", ""
	m.focusProfileByID(id)
}

var profileEditorActions = []string{"profile-editor.edit", "profile-editor.retry", "profile-editor.close"}

func (m *Model) profileEditorActivate(id string) tea.Cmd {
	if m.busy || m.quitting {
		return nil
	}
	switch id {
	case "profile-editor.edit":
		return m.launchProfileEditor()
	case "profile-editor.retry":
		m.newJob("profile.document.read-edit")
		request, path := m.id, m.profileEditor.path
		m.message = m.tr("profile_editor.saving")
		return func() tea.Msg { return readEditorOutput(request, path) }
	case "profile-editor.close":
		m.closeProfileEditor()
	}
	return nil
}

func (m *Model) profileEditorKey(k tea.KeyPressMsg) tea.Cmd {
	if m.busy || m.quitting {
		if k.String() == "esc" && m.job != "profile.document.edit" {
			m.cancelTask(false)
		}
		return nil
	}
	switch k.String() {
	case "tab", "right", "down":
		m.profileEditor.action = (m.profileEditor.action + 1) % len(profileEditorActions)
	case "shift+tab", "left", "up":
		m.profileEditor.action = (m.profileEditor.action + len(profileEditorActions) - 1) % len(profileEditorActions)
	case "esc":
		// Select the explicit close/discard action; a stray Escape never
		// deletes a private working copy after an unsuccessful save.
		m.profileEditor.action = len(profileEditorActions) - 1
	case "enter":
		return m.profileEditorActivate(profileEditorActions[m.profileEditor.action])
	}
	return nil
}

func (m *Model) renderProfileEditor(base string) (string, []hitRegion) {
	w := min(72, m.width)
	if w < 10 || m.height < 5 {
		return overlay(base, m.tr("profiles.enlarge"), 0, 0, m.width, m.height), nil
	}
	inner := w - 4
	title := m.tr("profile_editor.title", clean(m.profileInfo(m.name).Name))
	lines := []string{themed(cellCrop(title, inner), text, raised, false)}
	if m.busy {
		lines = append(lines, themed(cellCrop(m.message, inner), muted, raised, false))
	} else {
		for _, line := range strings.Split(wrapText(clean(m.failure), inner), "\n") {
			lines = append(lines, themed(cellCrop(line, inner), errorColor, raised, false))
		}
		// The path also lets the user save a copy in their editor if another
		// interface has changed the profile and this session can no longer commit.
		for _, line := range strings.Split(wrapText(m.tr("profile_editor.file", clean(m.profileEditor.path)), inner), "\n") {
			lines = append(lines, themed(cellCrop(line, inner), muted, raised, false))
		}
	}
	labels := []string{m.tr("profile_editor.continue"), m.tr("profile_editor.retry"), m.tr("profiles.close")}
	if m.profileEditor.tempDir != "" {
		labels[2] = m.tr("profile_editor.discard")
	}
	var buttons dashboardBlock
	if !m.busy {
		x := 0
		for i, id := range profileEditorActions {
			b := renderButton(labels[i], buttonOptions{Surface: raised, Focused: i == m.profileEditor.action, Hovered: m.hover == id, Plain: m.buttonPlain()})
			size := min(inner, ansi.StringWidth(b))
			if i == 0 || x+size > inner {
				buttons.lines = append(buttons.lines, "")
				x = 0
			}
			y := len(buttons.lines) - 1
			buttons.lines[y] += cellCrop(b, size)
			buttons.targets = append(buttons.targets, hitRegion{ID: id, X: x, Y: y, W: size, H: 1})
			x += size
		}
	}
	available := max(1, m.height-2-len(buttons.lines))
	lines = lines[:min(len(lines), available)]
	actionsY := 1 + len(lines)
	lines = append(lines, buttons.lines...)
	h := min(m.height, 2+len(lines))
	x, y := (m.width-w)/2, (m.height-h)/2
	rows := profileDialogCanvas(w, h)
	for i, line := range lines {
		if i+1 < h-1 {
			putRows(rows, 2, 1+i, cellPad(line, inner), w)
		}
	}
	for i := range buttons.targets {
		buttons.targets[i].X += x + 2
		buttons.targets[i].Y += y + actionsY
	}
	return overlay(base, strings.Join(rows, "\n"), x, y, m.width, m.height), buttons.targets
}
