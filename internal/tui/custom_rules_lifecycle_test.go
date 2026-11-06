package tui

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/app"
)

type customRulesLifecycleBackend struct {
	*customRulesTestBackend
	hintsSaved bool
}

func (b *customRulesLifecycleBackend) SaveKeyHintsPreference(ctx context.Context, _ bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.hintsSaved = true
	return nil
}

func customRulesMutation(t *testing.T, m *Model, kind string) tea.Cmd {
	t.Helper()
	if kind == "save" {
		*m = submitCustomRule(t, *m, false, "DOMAIN,local.example.invalid,DIRECT")
		return m.saveCustomRules()
	}
	m.customRules.draft.Pending = true
	return m.applyCustomRules()
}

func customRulesLifecycleResult(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	// Update may append reader/timer commands; the user action stays first.
	msg := cmd()
	for {
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			break
		}
		if len(batch) == 0 {
			t.Fatal("empty action batch")
		}
		msg = batch[0]()
	}
	switch msg.(type) {
	case keyHintsSavedMsg, customRulesResultMsg:
		return msg
	default:
		t.Fatalf("unexpected persistence result %T", msg)
		return nil
	}
}

func customRulesAssertNoQuit(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.QuitMsg:
		t.Fatal("quit before pending work or draft confirmation completed")
	case tea.BatchMsg:
		for _, next := range msg {
			customRulesAssertNoQuit(t, next)
		}
	}
}

func TestCustomRulesQuitWaitsForMutationAndKeyHintsPersistence(t *testing.T) {
	for _, kind := range []string{"save", "apply"} {
		for _, hintsFirst := range []bool{false, true} {
			for _, quitKey := range []string{"q", "ctrl+c"} {
				t.Run(kind+"/"+quitKey+"/hints-first="+strconv.FormatBool(hintsFirst), func(t *testing.T) {
					m, custom := customRulesModel(t, 120, 32)
					backend := &customRulesLifecycleBackend{customRulesTestBackend: custom}
					m.backend = backend
					m, hints := update(m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
					mutation := customRulesMutation(t, &m, kind)
					if hints == nil || mutation == nil || !m.keyHints.pending {
						t.Fatal("test did not start both independent persistence operations")
					}
					event := key("q")
					if quitKey == "ctrl+c" {
						event = ctrlC()
					}
					m, quit := update(m, event)
					if quit != nil || !m.quitting {
						t.Fatal("quit did not wait for the custom rule operation")
					}
					first, last := mutation, hints
					if hintsFirst {
						first, last = hints, mutation
					}
					m, quit = update(m, customRulesLifecycleResult(t, first))
					if quit != nil {
						t.Fatal("quit before both persistence operations completed")
					}
					m, quit = update(m, customRulesLifecycleResult(t, last))
					expectQuit(t, quit)
					if m.keyHints.pending || m.customRules.saving || m.customRules.applying || !backend.hintsSaved {
						t.Fatal("quit did not finish both pending operations")
					}
				})
			}
		}
	}
}

func TestCustomRulesParentCancellationWaitsForMutationCleanup(t *testing.T) {
	for _, kind := range []string{"save", "apply"} {
		t.Run(kind, func(t *testing.T) {
			m, backend := customRulesModel(t, 120, 32)
			started, canceled, cleanup := make(chan struct{}), make(chan struct{}), make(chan struct{})
			defer close(cleanup)
			work := func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				close(canceled)
				<-cleanup
				return ctx.Err()
			}
			backend.save = func(ctx context.Context, _ app.CustomRulesDraft) (app.CustomRulesDraft, error) {
				return app.CustomRulesDraft{}, work(ctx)
			}
			backend.apply = func(ctx context.Context, _ app.CustomRulesDraft) error { return work(ctx) }
			mutation := customRulesMutation(t, &m, kind)
			result := make(chan tea.Msg, 1)
			go func() { result <- mutation() }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("custom rule operation did not start")
			}
			m, quit := update(m, parentCanceledMsg{})
			if quit != nil || !m.quitting || !m.customRules.saving && !m.customRules.applying {
				t.Fatal("parent cancellation bypassed custom rule cleanup")
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("parent cancellation did not cancel the custom rule operation")
			}
			select {
			case <-result:
				t.Fatal("custom rule operation completed before safe cleanup")
			default:
			}
			cleanup <- struct{}{}
			select {
			case done := <-result:
				m, quit = update(m, done)
				expectQuit(t, quit)
				if m.customRules.saving || m.customRules.applying {
					t.Fatal("custom rule operation stayed pending after cleanup")
				}
			case <-time.After(time.Second):
				t.Fatal("custom rule cleanup did not release quit")
			}
		})
	}
}

func TestCustomRulesUnsubmittedInputRequiresDiscardBeforeBackOrReload(t *testing.T) {
	for _, edit := range []bool{false, true} {
		for _, action := range []string{"rules.custom-back", "rules.custom-reload"} {
			t.Run(action+"/edit="+strconv.FormatBool(edit), func(t *testing.T) {
				m, backend := customRulesModel(t, 120, 32)
				m.customRulesBeginInput(edit)
				value := "DOMAIN,unfinished.example.invalid,REJECT"
				m.customRules.input.SetValue(value)
				original := slices.Clone(m.customRules.draft.Rules)
				if m.customRulesDirty() {
					t.Fatal("unsubmitted input should not mutate the saved rule list")
				}
				if cmd := m.customRulesActivate(action); cmd != nil || m.customRules.confirm == "" || m.rules.view != "custom" {
					t.Fatal("unsubmitted input was discarded without confirmation")
				}
				m.customRulesActivate("rules.custom-cancel")
				if !m.customRules.inputOpen || m.customRules.input.Value() != value || !slices.Equal(m.customRules.draft.Rules, original) {
					t.Fatal("canceling discard lost the unsubmitted input")
				}
				m.customRulesActivate(action)
				cmd := m.customRulesActivate("rules.custom-confirm")
				if action == "rules.custom-reload" {
					if cmd == nil {
						t.Fatal("confirmed reload did not fetch the saved draft")
					}
					m, _ = update(m, cmd())
					if backend.loads != 2 || !m.customRules.loaded || !slices.Equal(m.customRules.draft.Rules, original) {
						t.Fatal("reload did not restore the saved rule list")
					}
				} else if m.rules.view != "" || m.customRules.loaded {
					t.Fatal("confirmed Back did not leave the custom rule editor")
				}
				if m.customRules.inputOpen || backend.saves != 0 || backend.applies != 0 {
					t.Fatal("discard kept the pending input open or invoked a mutation")
				}
			})
		}
	}
}

func TestCustomRulesQuitConfirmationClosesObscuringOverlays(t *testing.T) {
	for _, overlay := range []string{"help", "menu", "ports", "wizard", "network", "form", "notifications"} {
		t.Run(overlay, func(t *testing.T) {
			m, backend := customRulesModel(t, 120, 32)
			m = submitCustomRule(t, m, false, "DOMAIN,unsaved.example.invalid,DIRECT")
			want := slices.Clone(m.customRules.draft.Rules)
			m.navigate("settings")
			switch overlay {
			case "help":
				m.helpOpen = true
			case "menu":
				m.menu.open = true
			case "ports":
				m.ports.open = true
			case "wizard":
				m.wizard.open = true
			case "network":
				m.wizard.networkOpen = true
			case "form":
				m.form = "add"
			case "notifications":
				m.notification.panel.show(0)
			}
			m, quit := update(m, ctrlC())
			customRulesAssertNoQuit(t, quit)
			if m.quitting || m.page != "rules" || m.rules.view != "custom" || m.customRules.confirm != "quit" {
				t.Fatal("quit did not return to the unsaved rule confirmation")
			}
			if m.helpOpen || m.menu.open || m.ports.open || m.wizard.open || m.wizard.networkOpen || m.form != "" || m.notification.panel.open {
				t.Fatal("an overlay still obscures the custom rule confirmation")
			}
			if !hasAction(m.targets, "rules.custom-cancel") || !hasAction(m.targets, "rules.custom-confirm") {
				t.Fatal("the discard confirmation is not visible and interactive")
			}
			m, _ = update(m, special(tea.KeyEscape))
			if m.quitting || m.customRules.confirm != "" || !m.customRulesDirty() || !slices.Equal(m.customRules.draft.Rules, want) || backend.saves != 0 || backend.applies != 0 {
				t.Fatal("canceling quit did not retain the custom rule draft")
			}
		})
	}
}

func TestCustomRulesQuitCancelsAndWaitsForConcurrentTask(t *testing.T) {
	for _, kind := range []string{"save", "apply"} {
		for _, customFirst := range []bool{false, true} {
			t.Run(kind+"/custom-first="+strconv.FormatBool(customFirst), func(t *testing.T) {
				m, _ := customRulesModel(t, 120, 32)
				mutation := customRulesMutation(t, &m, kind)
				task := actionCmd(t, m.begin("Concurrent action", "Action complete", func(ctx context.Context) error { return ctx.Err() }))
				m, quit := update(m, ctrlC())
				if quit != nil || !m.quitting || !m.busy || !errors.Is(m.opCtx.Err(), context.Canceled) {
					t.Fatal("quit did not cancel and await the concurrent task")
				}
				customDone := mutation().(customRulesResultMsg)
				if !errors.Is(customDone.err, context.Canceled) {
					t.Fatal("quit did not cancel the custom rule operation")
				}
				first, last := tea.Msg(task()), tea.Msg(customDone)
				if customFirst {
					first, last = last, first
				}
				m, quit = update(m, first)
				customRulesAssertNoQuit(t, quit)
				m, quit = update(m, last)
				expectQuit(t, quit)
				if m.busy || m.customRules.saving || m.customRules.applying {
					t.Fatal("a mutation remained pending after quit was released")
				}
			})
		}
	}
}
