package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

type customRulesTestBackend struct {
	*fakeBackend
	draft                 app.CustomRulesDraft
	loads, saves, applies int
	get                   func(context.Context, string) (app.CustomRulesDraft, error)
	save                  func(context.Context, app.CustomRulesDraft) (app.CustomRulesDraft, error)
	apply                 func(context.Context, app.CustomRulesDraft) error
}

func (b *customRulesTestBackend) GetCustomRules(ctx context.Context, id string) (app.CustomRulesDraft, error) {
	b.loads++
	if b.get != nil {
		return b.get(ctx, id)
	}
	draft := b.draft
	draft.Rules = slices.Clone(draft.Rules)
	return draft, ctx.Err()
}
func (b *customRulesTestBackend) SaveCustomRules(ctx context.Context, draft app.CustomRulesDraft) (app.CustomRulesDraft, error) {
	b.saves++
	if b.save != nil {
		return b.save(ctx, draft)
	}
	if err := ctx.Err(); err != nil {
		return app.CustomRulesDraft{}, err
	}
	if draft.Token != b.draft.Token {
		return app.CustomRulesDraft{}, diagnostic.New("diagnostic.customRules.conflict", "profile configuration changed while editing custom rules; reload and retry")
	}
	b.draft.Rules = slices.Clone(draft.Rules)
	b.draft.Token = "saved-" + strconv.Itoa(b.saves)
	b.draft.Pending = true
	return b.draft, nil
}
func (b *customRulesTestBackend) ApplyCustomRules(ctx context.Context, draft app.CustomRulesDraft) error {
	b.applies++
	if b.apply != nil {
		return b.apply(ctx, draft)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if draft.Token != b.draft.Token || !slices.Equal(draft.Rules, b.draft.Rules) {
		return diagnostic.New("diagnostic.customRules.conflict", "profile configuration changed while editing custom rules; reload and retry")
	}
	b.draft.Pending = false
	return nil
}

func customRulesModel(t *testing.T, width, height int) (Model, *customRulesTestBackend) {
	t.Helper()
	b := &customRulesTestBackend{fakeBackend: &fakeBackend{}, draft: app.CustomRulesDraft{
		ProfileID: "work", ProfileName: "工作配置", Token: "recipe-1", Applied: true, Validated: true,
		Rules: []string{"DOMAIN,one.example.invalid,DIRECT", "DOMAIN-SUFFIX,two.example.invalid,PROXY"},
	}}
	m := rulesModel(t, b.fakeBackend)
	m.backend = b
	m.snapshot.Applied, m.snapshot.AppliedName = "work", "工作配置"
	m.width, m.height = width, height
	m.message, m.failure = "", ""
	m.notification.items = nil
	m.notification.source = m.notificationSource()
	cmd := m.openCustomRules()
	if cmd == nil {
		t.Fatal("custom rules did not start loading")
	}
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	m.layout()
	if !m.customRules.loaded || m.rules.view != "custom" || m.customRulesDirty() {
		t.Fatal("editor did not open a clean saved draft")
	}
	return m, b
}

func submitCustomRule(t *testing.T, m Model, edit bool, value string) Model {
	t.Helper()
	m.customRulesBeginInput(edit)
	m.customRules.input.SetValue(value)
	m.customRulesSubmitInput()
	m.layout()
	if m.customRules.inputOpen {
		t.Fatalf("rule input was rejected: %v", m.customRules.err)
	}
	return m
}

func TestCustomRulesEditDeleteAndReorderStayInUnsavedDraft(t *testing.T) {
	m, b := customRulesModel(t, 160, 45)
	original := slices.Clone(b.draft.Rules)
	m = submitCustomRule(t, m, false, "DOMAIN-SUFFIX,新增.example.invalid,PROXY")
	if !m.customRulesDirty() || len(m.customRules.draft.Rules) != 3 || m.customRules.selected != 2 || b.saves != 0 || b.applies != 0 {
		t.Fatal("adding a rule did not remain an unsaved local draft")
	}
	m.customRulesActivate("rules.custom-up")
	m.customRulesActivate("rules.custom-up")
	if m.customRules.selected != 0 || !strings.Contains(m.customRules.draft.Rules[0], "新增") {
		t.Fatal("moving a custom rule did not retain its identity")
	}
	m = submitCustomRule(t, m, true, "DOMAIN,edited.example.invalid,REJECT")
	if m.customRules.draft.Rules[0] != "DOMAIN,edited.example.invalid,REJECT" {
		t.Fatal("editing changed the wrong custom rule")
	}
	m.customRulesActivate("rules.custom-delete")
	if m.customRules.confirm != "delete" || m.focus != "rules.custom-cancel" || len(m.customRules.draft.Rules) != 3 {
		t.Fatal("delete was not confirmed with cancel selected by default")
	}
	m.customRulesActivate("rules.custom-cancel")
	if len(m.customRules.draft.Rules) != 3 {
		t.Fatal("canceling deletion removed a rule")
	}
	m.customRulesActivate("rules.custom-delete")
	m.customRulesActivate("rules.custom-confirm")
	if !slices.Equal(m.customRules.draft.Rules, original) || m.customRulesDirty() || !slices.Equal(b.draft.Rules, original) || b.saves != 0 || b.applies != 0 {
		t.Fatal("confirmed deletion did not affect only the local draft")
	}
}

func TestCustomRulesSaveDoesNotApplyAndApplyRequiresConfirmation(t *testing.T) {
	m, b := customRulesModel(t, 160, 45)
	m = submitCustomRule(t, m, false, "DOMAIN,custom.example.invalid,DIRECT")
	cmd := m.customRulesActivate("rules.custom-save")
	if cmd == nil || !m.customRules.saving || b.saves != 0 || b.applies != 0 {
		t.Fatal("save was not an independent asynchronous operation")
	}
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	m.layout()
	if b.saves != 1 || b.applies != 0 || m.customRulesDirty() || !m.customRules.draft.Pending || m.customRules.draft.Token != b.draft.Token {
		t.Fatal("saving applied the profile or failed to update its reviewed draft")
	}
	m.customRulesActivate("rules.custom-apply")
	if m.customRules.confirm != "apply" || m.focus != "rules.custom-cancel" || b.applies != 0 {
		t.Fatal("apply did not require an explicit default-cancel confirmation")
	}
	m.customRulesActivate("rules.custom-cancel")
	if b.applies != 0 {
		t.Fatal("canceling apply changed the running profile")
	}
	m.customRulesActivate("rules.custom-apply")
	cmd = m.customRulesActivate("rules.custom-confirm")
	if cmd == nil || !m.customRules.applying || b.applies != 0 {
		t.Fatal("confirmation did not start the reviewed apply operation")
	}
	load := m.acceptCustomRules(cmd().(customRulesResultMsg))
	if load == nil || b.applies != 1 {
		t.Fatal("applying did not request a fresh saved observation")
	}
	m.acceptCustomRules(load().(customRulesResultMsg))
	m.layout()
	if m.customRules.draft.Pending || !m.customRules.loaded || m.customRulesDirty() || m.customRules.notice != m.tr("custom_rules.applied") {
		t.Fatal("successful apply did not retain its result and current saved rules")
	}
}

func TestCustomRulesConflictPreservesDraftAndReloadIsConfirmed(t *testing.T) {
	m, b := customRulesModel(t, 160, 45)
	m = submitCustomRule(t, m, false, "DOMAIN,local.example.invalid,DIRECT")
	want := slices.Clone(m.customRules.draft.Rules)
	b.draft.Token = "changed-by-subscription"
	b.draft.Rules = []string{"DOMAIN,upstream.example.invalid,PROXY"}
	cmd := m.saveCustomRules()
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	m.layout()
	if m.customRules.err == nil || !m.customRulesDirty() || !slices.Equal(m.customRules.draft.Rules, want) || m.customRules.draft.Token != "recipe-1" || b.applies != 0 {
		t.Fatal("a save conflict discarded or silently retargeted the local draft")
	}
	if m.customRulesActivate("rules.custom-reload") != nil || m.customRules.confirm != "reload" || b.loads != 1 {
		t.Fatal("reload discarded unsaved edits before confirmation")
	}
	m.customRulesActivate("rules.custom-cancel")
	if !slices.Equal(m.customRules.draft.Rules, want) {
		t.Fatal("canceling reload lost the conflicted draft")
	}
	m.customRulesActivate("rules.custom-reload")
	cmd = m.customRulesActivate("rules.custom-confirm")
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	m.layout()
	if m.customRules.err != nil || m.customRulesDirty() || m.customRules.draft.Token != b.draft.Token || !slices.Equal(m.customRules.draft.Rules, b.draft.Rules) {
		t.Fatal("confirmed reload did not recover the latest saved recipe")
	}
}

func TestCustomRulesSaveAndApplyFailuresKeepRecoverableState(t *testing.T) {
	m, b := customRulesModel(t, 160, 45)
	m = submitCustomRule(t, m, false, "DOMAIN,local.example.invalid,DIRECT")
	b.save = func(context.Context, app.CustomRulesDraft) (app.CustomRulesDraft, error) {
		return app.CustomRulesDraft{}, errors.New("fixture validation failed")
	}
	cmd := m.saveCustomRules()
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	if m.customRules.err == nil || !m.customRulesDirty() || m.customRules.saving || b.applies != 0 {
		t.Fatal("validation failure lost the draft or started apply")
	}
	b.save = nil
	cmd = m.saveCustomRules()
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	b.apply = func(context.Context, app.CustomRulesDraft) error { return errors.New("fixture restart rolled back") }
	cmd = m.applyCustomRules()
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	if m.customRules.err == nil || m.customRules.applying || !m.customRules.draft.Pending || m.customRulesDirty() || !slices.Equal(m.customRules.draft.Rules, b.draft.Rules) {
		t.Fatal("apply failure did not preserve the saved pending draft")
	}
}

func TestCustomRulesInputOwnsTextAndRejectsMultilinePaste(t *testing.T) {
	m, b := customRulesModel(t, 80, 24)
	m.customRulesBeginInput(false)
	for _, value := range []string{"q", "1", "2", "3", "4", "5", "6", "7", "r", "s", "x", "a", "i", "n"} {
		m, _ = update(m, key(value))
	}
	if m.page != "rules" || m.rules.view != "custom" || m.quitting || !m.customRules.inputOpen || m.customRules.input.Value() != "q1234567rsxain" || m.wizard.open || b.saves != 0 || b.applies != 0 {
		t.Fatal("rule input text triggered a global shortcut")
	}
	before := m.customRules.input.Value()
	m, _ = update(m, tea.PasteMsg{Content: "DOMAIN,one.invalid,DIRECT\nDOMAIN,two.invalid,PROXY"})
	if m.customRules.err == nil || m.customRules.input.Value() != before || m.customRulesDirty() {
		t.Fatal("multiline paste was silently converted into a different rule")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.customRules.inputOpen || m.customRulesDirty() || m.rules.view != "custom" {
		t.Fatal("canceling rule input changed the saved rule list")
	}
}

func TestCustomRulesUnsavedBackAndQuitDefaultToCancel(t *testing.T) {
	m, b := customRulesModel(t, 160, 45)
	m = submitCustomRule(t, m, false, "DOMAIN,local.example.invalid,DIRECT")
	m.customRulesActivate("rules.custom-back")
	if m.customRules.confirm != "discard" || m.focus != "rules.custom-cancel" || m.rules.view != "custom" {
		t.Fatal("leaving dirty custom rules discarded without confirmation")
	}
	m, _ = update(m, special(tea.KeyEnter))
	if m.customRules.confirm != "" || !m.customRulesDirty() || m.rules.view != "custom" {
		t.Fatal("default confirmation did not cancel leaving the editor")
	}
	m, _ = update(m, key("q"))
	if m.quitting || m.customRules.confirm != "quit" || m.focus != "rules.custom-cancel" {
		t.Fatal("quitting lost the unsaved draft without confirmation")
	}
	m, _ = update(m, special(tea.KeyEscape))
	if m.quitting || !m.customRulesDirty() || m.customRules.confirm != "" {
		t.Fatal("canceling quit did not retain the draft")
	}
	m.customRulesActivate("rules.custom-back")
	m.customRulesActivate("rules.custom-confirm")
	if m.rules.view != "" || m.customRules.loaded || b.saves != 0 || b.applies != 0 {
		t.Fatal("discard confirmation saved, applied or failed to return to the rules list")
	}
}

func TestCustomRulesDraftSurvivesPageNavigation(t *testing.T) {
	m, b := customRulesModel(t, 160, 45)
	m = submitCustomRule(t, m, false, "DOMAIN,local.example.invalid,DIRECT")
	want := slices.Clone(m.customRules.draft.Rules)
	m, _ = update(m, key("3"))
	if m.page != "profiles" || !m.customRulesDirty() {
		t.Fatal("switching pages discarded the unsaved custom rules")
	}
	m, _ = update(m, key("5"))
	if m.page != "rules" || m.rules.view != "custom" || !slices.Equal(m.customRules.draft.Rules, want) || !m.customRulesDirty() || b.loads != 1 {
		t.Fatal("returning to rules reloaded or lost the editor draft")
	}
}

func TestCustomRulesConfirmationCannotRetargetDeletion(t *testing.T) {
	m, _ := customRulesModel(t, 160, 45)
	m.customRules.selected = 0
	m.customRulesActivate("rules.custom-delete")
	m.layout()
	target := findTarget(t, m, "rules.custom-row.1")
	m, _ = click(m, target)
	if m.customRules.selected != 0 || m.customRules.confirm != "delete" {
		t.Fatal("a background row changed the deletion target during confirmation")
	}
	m.customRulesActivate("rules.custom-confirm")
	if len(m.customRules.draft.Rules) != 1 || !strings.Contains(m.customRules.draft.Rules[0], "two.example") {
		t.Fatal("deletion confirmation removed a rule other than the reviewed target")
	}
}

func TestCustomRulesDefaultReadOnlyAndLoadRecovery(t *testing.T) {
	m, b := customRulesModel(t, 160, 45)
	m.customRules.loaded = false
	m.snapshot.Applied = "default"
	b.get = func(context.Context, string) (app.CustomRulesDraft, error) {
		return app.CustomRulesDraft{}, diagnostic.New("diagnostic.customRules.readOnly", "the built-in default profile is read only; choose a saved profile to customize rules")
	}
	cmd := m.loadCustomRules()
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	m.layout()
	if m.customRules.loaded || m.customRules.err == nil || hasAction(m.targets, "rules.custom-add") || hasAction(m.targets, "rules.custom-save") || hasAction(m.targets, "rules.custom-apply") {
		t.Fatal("the built-in default exposed rule mutation actions")
	}
	if cmd := m.customRulesBeginInput(false); cmd != nil || m.customRules.inputOpen || b.saves != 0 || b.applies != 0 {
		t.Fatal("read-only editor accepted a new rule")
	}
	b.get = nil
	m.snapshot.Applied = "work"
	cmd = m.loadCustomRules()
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	m.layout()
	if !m.customRules.loaded || m.customRules.err != nil || !hasAction(m.targets, "rules.custom-add") {
		t.Fatal("a new valid load did not recover editable custom rules")
	}
}

func TestCustomRulesLateResultsDoNotReplaceNewerDraft(t *testing.T) {
	m, _ := customRulesModel(t, 160, 45)
	old := m.customRules.generation
	m.loadCustomRules()
	want := m.customRules.draft
	m.acceptCustomRules(customRulesResultMsg{generation: old, kind: "save", draft: app.CustomRulesDraft{ProfileID: "stale", Token: "stale", Rules: []string{"MATCH,REJECT"}}})
	if !reflect.DeepEqual(m.customRules.draft, want) || !m.customRules.loading {
		t.Fatal("late save result replaced a newer editor request")
	}
}

func TestCustomRulesBackCancelsLoadAndRejectsLateResult(t *testing.T) {
	m, b := customRulesModel(t, 160, 45)
	m.customRulesBack()
	var loadContext context.Context
	b.get = func(ctx context.Context, _ string) (app.CustomRulesDraft, error) {
		loadContext = ctx
		return b.draft, nil // A backend may still deliver data after cancellation.
	}
	cmd := m.openCustomRules()
	if cmd == nil || !m.customRules.loading {
		t.Fatal("reopening did not start a fresh load")
	}
	m.customRulesActivate("rules.custom-back")
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	if loadContext == nil || loadContext.Err() != context.Canceled || m.rules.view != "" || m.customRules.loaded || m.customRules.loading {
		t.Fatal("back did not cancel and isolate its late load result")
	}
}

func TestCustomRulesReloadKeepsProfileAndReopenBindsCurrentProfile(t *testing.T) {
	m, b := customRulesModel(t, 160, 45)
	m.snapshot.Applied, m.snapshot.AppliedName = "other", "Other profile"
	var requested []string
	b.get = func(ctx context.Context, id string) (app.CustomRulesDraft, error) {
		requested = append(requested, id)
		draft := b.draft
		draft.ProfileID = id
		return draft, ctx.Err()
	}
	cmd := m.loadCustomRules()
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	if len(requested) != 1 || requested[0] != "work" || m.customRules.draft.ProfileID != "work" {
		t.Fatal("reloading an existing draft silently switched its profile")
	}
	m.customRulesBack()
	cmd = m.openCustomRules()
	m.acceptCustomRules(cmd().(customRulesResultMsg))
	if !slices.Equal(requested, []string{"work", "other"}) || m.customRules.draft.ProfileID != "other" {
		t.Fatal("freshly opening the editor did not bind the current applied profile")
	}
}

func TestCustomRulesResponsiveEditorAndCompleteLongRows(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {120, 32}, {80, 24}, {60, 20}, {40, 16}, {40, 9}, {20, 7}, {5, 2}} {
		for _, state := range []string{"list", "input", "confirm"} {
			t.Run(fmt.Sprintf("%v/%s", size, state), func(t *testing.T) {
				m, _ := customRulesModel(t, size[0], size[1])
				raw := "DOMAIN-SUFFIX," + strings.Repeat("中文规则🚀", 20) + ".invalid,DIRECT"
				m.customRules.draft.Rules = []string{raw, "DOMAIN,two.invalid,PROXY"}
				switch state {
				case "input":
					m.customRulesBeginInput(true)
				case "confirm":
					m.customRulesActivate("rules.custom-delete")
				}
				m.layout()
				assertCanvas(t, m.screen, size[0], size[1])
				assertRegions(t, m.targets, size[0], size[1])
				if size[0] >= 40 && size[1] >= 16 && state == "input" && !hasAction(m.targets, "rules.custom-input") {
					t.Fatal("compact editor hid its active rule input")
				}
				if state == "list" {
					width := dashboardBodyWidth(m.dashboardOptions())
					body := m.customRulesBodyWindow(width, 0, -1)
					if size[0] >= 20 {
						var target hitRegion
						for _, hit := range body.targets {
							if hit.ID == "rules.custom-row.0" {
								target = hit
							}
						}
						var complete strings.Builder
						for _, line := range body.lines[target.Y : target.Y+target.H] {
							complete.WriteString(strings.TrimSpace(ansi.Strip(ansi.Cut(line, 5, width))))
						}
						if complete.String() != raw {
							t.Fatalf("wrapped custom rule changed its contents: %q", complete.String())
						}
					}
				}
			})
		}
	}
}
