package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestProfilePasteMouseInsertsAtCursor(t *testing.T) {
	for _, width := range []int{120, 60, 40, 24} {
		model := profileInteractionsModel(&fakeBackend{}, width, 24)
		model.activate("profiles.url")
		model, _ = update(model, tea.PasteMsg{Content: "https://example.invalid/.yaml"})
		model, _ = update(model, special(tea.KeyEnd))
		for range len(".yaml") {
			model, _ = update(model, special(tea.KeyLeft))
		}
		field := findTarget(t, model, "profiles.url")
		paste := findTarget(t, model, "profiles.url.paste")
		if width == 24 {
			if paste.Y != field.Y+field.H || paste.X+paste.W != field.X+field.W {
				t.Fatalf("narrow field did not wrap the paste button to its right edge: %+v %+v", field, paste)
			}
		} else if paste.Y != field.Y+1 || paste.X <= field.X+2+field.InputWidth || paste.X+paste.W >= field.X+field.W {
			t.Fatalf("width %d: paste button overlaps the editor or frame: %+v %+v", width, field, paste)
		}

		reply := tea.ClipboardMsg{Content: "测试-q123", Selection: ansi.SystemClipboard}
		model, _ = update(model, reply)
		if model.profilesUI.url.Value() != "https://example.invalid/.yaml" {
			t.Fatal("an unsolicited clipboard reply changed the URL")
		}
		model, command := click(model, paste)
		if command == nil || model.profilesUI.pasteDeadline.IsZero() || model.focus != "profiles.url" {
			t.Fatalf("width %d: paste click did not request clipboard access and focus the URL", width)
		}
		model, _ = update(model, reply)
		if model.profilesUI.url.Value() != "https://example.invalid/测试-q123.yaml" || model.busy || model.page != "profiles" ||
			model.focus != "profiles.url" || !model.profilesUI.pasteDeadline.IsZero() {
			t.Fatalf("width %d: reply did not edit the URL in place: %q", width, model.profilesUI.url.Value())
		}
	}
}

func TestProfilePasteFailurePreservesDraftAndAllowsTerminalPaste(t *testing.T) {
	for _, scenario := range []string{"empty reply", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			model := profileInteractionsModel(&fakeBackend{}, 160, 24)
			model.activate("profiles.url")
			model, _ = update(model, tea.PasteMsg{Content: "https://example.invalid/"})
			model, _ = update(model, special(tea.KeyTab))
			model, _ = update(model, special(tea.KeyTab))
			model, _ = update(model, special(tea.KeyEnter))
			deadline := model.profilesUI.pasteDeadline
			if deadline.IsZero() || model.busy {
				t.Fatal("Enter on the paste button did not begin a clipboard request")
			}
			if scenario == "empty reply" {
				model, _ = update(model, tea.ClipboardMsg{Selection: ansi.SystemClipboard})
			} else {
				model, _ = update(model, profilePasteTimeoutMsg{deadline: deadline})
			}
			model, _ = update(model, tea.ClipboardMsg{Content: "late", Selection: ansi.SystemClipboard})
			if model.profilesUI.url.Value() != "https://example.invalid/" || model.busy {
				t.Fatal("clipboard failure changed the URL or submitted it")
			}
			if !strings.Contains(ansi.Strip(model.View().Content), model.tr("profiles.clipboard_unavailable")) {
				t.Fatalf("clipboard failure omitted the terminal shortcut hint:\n%s", ansi.Strip(model.View().Content))
			}

			model, _ = update(model, tea.PasteMsg{Content: "manual"})
			if model.profilesUI.url.Value() != "https://example.invalid/manual" || model.profilesUI.pasteUnavailable {
				t.Fatal("terminal paste did not edit the URL and clear the clipboard hint")
			}
		})
	}
}

func TestProfilePasteDiscardsAbandonedRequest(t *testing.T) {
	model := profileInteractionsModel(&fakeBackend{}, 120, 24)
	model, _ = click(model, findTarget(t, model, "profiles.url.paste"))
	oldDeadline := model.profilesUI.pasteDeadline
	model, _ = update(model, special(tea.KeyTab))
	model.activate("profiles.url")
	model, _ = update(model, tea.ClipboardMsg{Content: "abandoned", Selection: ansi.SystemClipboard})
	if model.profilesUI.url.Value() != "" {
		t.Fatal("a reply after leaving and returning to the field changed the draft")
	}

	model, _ = click(model, findTarget(t, model, "profiles.url.paste"))
	model, _ = update(model, profilePasteTimeoutMsg{deadline: oldDeadline})
	if model.profilesUI.pasteDeadline.IsZero() || model.profilesUI.pasteUnavailable {
		t.Fatal("an old timer ended the next request")
	}
	model, _ = update(model, tea.PasteMsg{Content: "manual"})
	model, _ = update(model, tea.ClipboardMsg{Content: "late", Selection: ansi.SystemClipboard})
	if model.profilesUI.url.Value() != "manual" || model.profilesUI.pasteUnavailable {
		t.Fatal("a late reply interfered with terminal paste")
	}
}
