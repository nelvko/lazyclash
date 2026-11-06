package tui

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/configuration"
)

type customRulesBackend interface {
	GetCustomRules(context.Context, string) (app.CustomRulesDraft, error)
	SaveCustomRules(context.Context, app.CustomRulesDraft) (app.CustomRulesDraft, error)
	ApplyCustomRules(context.Context, app.CustomRulesDraft) error
}
type customRulesState struct {
	draft                             app.CustomRulesDraft
	original                          []string
	loaded, loading, saving, applying bool
	generation                        uint64
	cancel                            context.CancelFunc
	selected, editIndex               int
	input                             textinput.Model
	inputOpen                         bool
	confirm                           string
	err                               error
	notice                            string
	noticeAfterLoad                   string
	returnScroll                      int
}
type customRulesResultMsg struct {
	generation uint64
	kind       string
	draft      app.CustomRulesDraft
	err        error
}

func newCustomRulesState() customRulesState {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = configuration.MaxDocumentSize
	styleInput(&input, inputBackground, false)
	return customRulesState{input: input, editIndex: -1}
}
func (m Model) customRulesDirty() bool {
	return m.customRules.loaded && !slices.Equal(m.customRules.original, m.customRules.draft.Rules)
}
func (m Model) customRulesUnsaved() bool {
	s := m.customRules
	if m.customRulesDirty() {
		return true
	}
	if !s.inputOpen {
		return false
	}
	value := strings.TrimSpace(s.input.Value())
	if s.editIndex >= 0 && s.editIndex < len(s.draft.Rules) {
		return value != s.draft.Rules[s.editIndex]
	}
	return value != ""
}
func (m *Model) openCustomRules() tea.Cmd {
	if _, ok := m.backend.(customRulesBackend); !ok {
		return nil
	}
	m.rules.editing = false
	m.rules.search.Blur()
	m.stopRuleProviders()
	m.customRules.returnScroll = m.scroll
	m.rules.view = "custom"
	m.scroll = 0
	m.focus = "rules.custom-back"
	if m.customRules.loaded {
		return nil
	}
	m.customRules.draft = app.CustomRulesDraft{}
	m.customRules.original = nil
	m.customRules.noticeAfterLoad = ""
	return m.loadCustomRules()
}
func (m *Model) loadCustomRules() tea.Cmd {
	s := &m.customRules
	if s.cancel != nil {
		s.cancel()
	}
	s.generation++
	s.loading = true
	s.inputOpen = false
	s.input.Blur()
	s.err = nil
	s.notice = ""
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	s.cancel = cancel
	generation, backend, profile := s.generation, m.backend.(customRulesBackend), m.snapshot.Applied
	if s.draft.ProfileID != "" {
		profile = s.draft.ProfileID
	}
	return func() tea.Msg {
		d, e := backend.GetCustomRules(ctx, profile)
		return customRulesResultMsg{generation: generation, kind: "load", draft: d, err: e}
	}
}
func (m *Model) acceptCustomRules(v customRulesResultMsg) tea.Cmd {
	s := &m.customRules
	if v.generation != s.generation {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.loading, s.saving, s.applying = false, false, false
	s.err = v.err
	if m.quitting {
		if !m.busy && !m.keyHints.pending {
			return tea.Quit
		}
		return nil
	}
	if v.err != nil {
		return nil
	}
	if v.kind == "apply" {
		s.noticeAfterLoad = m.tr("custom_rules.applied")
		s.loaded = false
		return m.loadCustomRules()
	}
	s.draft = v.draft
	s.draft.Rules = slices.Clone(v.draft.Rules)
	s.original = slices.Clone(v.draft.Rules)
	s.loaded = true
	s.selected = max(0, min(s.selected, len(s.draft.Rules)-1))
	if v.kind == "save" {
		s.notice = m.tr("custom_rules.saved")
	} else {
		s.notice = s.noticeAfterLoad
		s.noticeAfterLoad = ""
	}
	return nil
}
func (m *Model) customRulesBeginInput(edit bool) tea.Cmd {
	s := &m.customRules
	if !s.loaded || s.loading || s.saving || s.applying {
		return nil
	}
	s.input.Reset()
	s.editIndex = -1
	if edit {
		if s.selected < 0 || s.selected >= len(s.draft.Rules) {
			return nil
		}
		s.editIndex = s.selected
		s.input.SetValue(s.draft.Rules[s.selected])
	}
	s.inputOpen = true
	s.err = nil
	s.notice = ""
	s.input.CursorEnd()
	m.focus = "rules.custom-input"
	return s.input.Focus()
}
func (m *Model) customRulesSubmitInput() {
	s := &m.customRules
	value := strings.TrimSpace(s.input.Value())
	if value == "" || strings.ContainsAny(value, "\r\n") {
		s.err = errors.New(m.tr("custom_rules.single_line"))
		return
	}
	s.draft.Rules = slices.Clone(s.draft.Rules)
	if s.editIndex < 0 {
		s.draft.Rules = append(s.draft.Rules, value)
		s.selected = len(s.draft.Rules) - 1
	} else {
		s.draft.Rules[s.editIndex] = value
		s.selected = s.editIndex
	}
	s.inputOpen = false
	s.input.Blur()
	s.err = nil
	s.notice = ""
	m.focus = "rules.custom-row." + strconv.Itoa(s.selected)
	m.revealCustomRule()
}
func (m *Model) saveCustomRules() tea.Cmd {
	s := &m.customRules
	if !m.customRulesDirty() || s.loading || s.saving || s.applying || s.inputOpen {
		return nil
	}
	s.saving = true
	s.err = nil
	s.notice = ""
	s.generation++
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	s.cancel = cancel
	generation, draft, backend := s.generation, s.draft, m.backend.(customRulesBackend)
	draft.Rules = slices.Clone(draft.Rules)
	return func() tea.Msg {
		d, e := backend.SaveCustomRules(ctx, draft)
		return customRulesResultMsg{generation: generation, kind: "save", draft: d, err: e}
	}
}
func (m *Model) applyCustomRules() tea.Cmd {
	s := &m.customRules
	if !s.loaded || m.customRulesDirty() || !s.draft.Pending || s.saving || s.applying || s.inputOpen {
		return nil
	}
	s.applying = true
	s.err = nil
	s.confirm = ""
	s.generation++
	ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
	s.cancel = cancel
	generation, draft, backend := s.generation, s.draft, m.backend.(customRulesBackend)
	draft.Rules = slices.Clone(draft.Rules)
	return func() tea.Msg {
		e := backend.ApplyCustomRules(ctx, draft)
		return customRulesResultMsg{generation: generation, kind: "apply", err: e}
	}
}
func (m *Model) customRulesBack() {
	s := &m.customRules
	s.inputOpen = false
	s.input.Blur()
	s.confirm = ""
	s.loaded = false
	m.rules.view = ""
	m.scroll = s.returnScroll
	m.focus = "rules.custom"
}
func (m *Model) revealCustomRule() {
	g := dashboardGeometry(m.dashboardOptions())
	for _, target := range m.customRulesBodyWindow(g.width, 0, 0).targets {
		if target.ID == m.focus {
			m.revealBodyTarget(target)
			return
		}
	}
}
func (m *Model) customRulesActivate(id string) tea.Cmd {
	s := &m.customRules
	if s.confirm != "" && id != "rules.custom-confirm" && id != "rules.custom-cancel" {
		return nil
	}
	if id == "rules.custom-back" && s.loading {
		if s.cancel != nil {
			s.cancel()
			s.cancel = nil
		}
		s.generation++
		s.loading = false
		m.customRulesBack()
		return nil
	}
	if s.loading || s.saving || s.applying {
		return nil
	}
	switch {
	case id == "rules.custom-back":
		if m.customRulesUnsaved() {
			s.confirm = "discard"
			m.focus = "rules.custom-cancel"
		} else {
			m.customRulesBack()
		}
	case id == "rules.custom-reload":
		if m.customRulesUnsaved() {
			s.confirm = "reload"
			m.focus = "rules.custom-cancel"
		} else {
			return m.loadCustomRules()
		}
	case id == "rules.custom-add":
		return m.customRulesBeginInput(false)
	case id == "rules.custom-edit":
		return m.customRulesBeginInput(true)
	case id == "rules.custom-submit":
		m.customRulesSubmitInput()
	case id == "rules.custom-input-cancel":
		s.inputOpen = false
		s.input.Blur()
		s.err = nil
		m.focus = "rules.custom-add"
	case id == "rules.custom-save":
		return m.saveCustomRules()
	case id == "rules.custom-apply":
		if !m.customRulesDirty() && s.loaded && s.draft.Pending {
			s.confirm = "apply"
			m.focus = "rules.custom-cancel"
		}
	case id == "rules.custom-delete":
		if s.selected >= 0 && s.selected < len(s.draft.Rules) {
			s.confirm = "delete"
			m.focus = "rules.custom-cancel"
		}
	case id == "rules.custom-up" || id == "rules.custom-down":
		next := s.selected - 1
		if id == "rules.custom-down" {
			next = s.selected + 1
		}
		if next >= 0 && next < len(s.draft.Rules) {
			s.draft.Rules = slices.Clone(s.draft.Rules)
			s.draft.Rules[next], s.draft.Rules[s.selected] = s.draft.Rules[s.selected], s.draft.Rules[next]
			s.selected = next
			m.focus = "rules.custom-row." + strconv.Itoa(next)
			s.notice = ""
			m.revealCustomRule()
		}
	case id == "rules.custom-cancel":
		s.confirm = ""
		m.focus = "rules.custom-back"
	case id == "rules.custom-confirm":
		action := s.confirm
		s.confirm = ""
		switch action {
		case "apply":
			return m.applyCustomRules()
		case "delete":
			if s.selected >= 0 && s.selected < len(s.draft.Rules) {
				s.draft.Rules = slices.Delete(slices.Clone(s.draft.Rules), s.selected, s.selected+1)
				s.selected = max(0, min(s.selected, len(s.draft.Rules)-1))
				m.focus = "rules.custom-row." + strconv.Itoa(s.selected)
				s.notice = ""
			}
		case "reload":
			return m.loadCustomRules()
		case "quit":
			s.draft.Rules = slices.Clone(s.original)
			s.inputOpen = false
			s.input.Blur()
			return m.requestQuit()
		case "discard":
			s.draft.Rules = slices.Clone(s.original)
			m.customRulesBack()
		}
	case strings.HasPrefix(id, "rules.custom-row."):
		index, err := strconv.Atoi(strings.TrimPrefix(id, "rules.custom-row."))
		if err == nil && index >= 0 && index < len(s.draft.Rules) {
			s.selected = index
			m.focus = id
		}
	}
	return nil
}
func (m *Model) customRulesInputKey(k tea.KeyPressMsg) tea.Cmd {
	s := &m.customRules
	if s.confirm != "" {
		switch k.String() {
		case "esc":
			return m.customRulesActivate("rules.custom-cancel")
		case "tab", "shift+tab":
			if m.focus == "rules.custom-confirm" {
				m.focus = "rules.custom-cancel"
			} else {
				m.focus = "rules.custom-confirm"
			}
		case "enter", "space", " ":
			return m.customRulesActivate(m.focus)
		}
		return nil
	}
	switch k.String() {
	case "esc":
		return m.customRulesActivate("rules.custom-input-cancel")
	case "enter":
		if m.focus == "rules.custom-input-cancel" {
			return m.customRulesActivate(m.focus)
		}
		m.customRulesSubmitInput()
		return nil
	case "tab", "shift+tab":
		controls := []string{"rules.custom-input", "rules.custom-submit", "rules.custom-input-cancel"}
		index := slices.Index(controls, m.focus)
		delta := 1
		if k.String() == "shift+tab" {
			delta = -1
		}
		m.focus = controls[(max(0, index)+delta+len(controls))%len(controls)]
		if m.focus == "rules.custom-input" {
			return s.input.Focus()
		}
		s.input.Blur()
		return nil
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(k)
	s.err = nil
	return cmd
}
func (m *Model) customRulesKey(k tea.KeyPressMsg) (bool, tea.Cmd) {
	s := &m.customRules
	if s.inputOpen || s.confirm != "" {
		return true, m.customRulesInputKey(k)
	}
	switch k.String() {
	case "esc":
		return true, m.customRulesActivate("rules.custom-back")
	case "a":
		return true, m.customRulesBeginInput(false)
	case "e":
		return true, m.customRulesBeginInput(true)
	case "d", "x", "delete":
		return true, m.customRulesActivate("rules.custom-delete")
	case "r", "g", "ctrl+r":
		return true, m.customRulesActivate("rules.custom-reload")
	case "alt+up":
		return true, m.customRulesActivate("rules.custom-up")
	case "alt+down":
		return true, m.customRulesActivate("rules.custom-down")
	case "s", "ctrl+s":
		return true, m.saveCustomRules()
	case "up", "k", "down", "j", "home", "end", "pgup", "pgdown":
		if !s.loaded || len(s.draft.Rules) == 0 {
			return true, nil
		}
		delta := 1
		if k.String() == "up" || k.String() == "k" {
			delta = -1
		}
		if k.String() == "pgup" {
			delta = -max(1, dashboardGeometry(m.dashboardOptions()).height/3)
		}
		if k.String() == "pgdown" {
			delta = max(1, dashboardGeometry(m.dashboardOptions()).height/3)
		}
		s.selected = max(0, min(len(s.draft.Rules)-1, s.selected+delta))
		if k.String() == "home" {
			s.selected = 0
		}
		if k.String() == "end" {
			s.selected = len(s.draft.Rules) - 1
		}
		m.focus = "rules.custom-row." + strconv.Itoa(s.selected)
		m.revealCustomRule()
		return true, nil
	case "enter":
		if strings.HasPrefix(m.focus, "rules.custom-row.") {
			return true, m.customRulesBeginInput(true)
		}
	}
	return false, nil
}

func (m Model) customRulesInputActive() bool {
	return m.page == "rules" && m.rules.view == "custom" && m.customRules.inputOpen && !m.sidebarOpen && m.focus != "menu" && !strings.HasPrefix(m.focus, "nav.")
}
