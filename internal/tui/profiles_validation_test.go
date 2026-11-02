package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestProfileAddressValidationStaysWithEditableInput(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, source := range []string{"", "1243", "ftp://example.com/sub", "https://", "https://example.com/%zz"} {
			t.Run(language+"/"+source, func(t *testing.T) {
				calls := 0
				m := profileInteractionsModel(&fakeBackend{addSubscription: func(context.Context, string, string) error {
					calls++
					return nil
				}}, 120, 32)
				m.locale = i18n.New(language)
				m.profilesUI.url.SetValue(source)
				m.focus = "profiles.add"
				m, _ = update(m, special(tea.KeyEnter))
				if calls != 0 || m.busy || m.failure != "" || m.profilesUI.urlError == "" || m.profilesUI.url.Value() != source {
					t.Fatal("invalid address escaped local validation or discarded its input", m.failure, m.profilesUI.urlError)
				}
				if m.focus != "profiles.url" || !m.profilesUI.editing {
					t.Fatal("invalid input is not ready for correction")
				}
				body, _ := m.profilesBody(86)
				if !strings.Contains(strings.Join(strings.Fields(ansi.Strip(body)), " "), m.profilesUI.urlError) {
					t.Fatal("local validation was not rendered beside the URL")
				}
				m, _ = update(m, tea.PasteMsg{Content: "a"})
				if m.profilesUI.urlError != "" {
					t.Fatal("editing left the previous validation issue visible")
				}
			})
		}
	}
}

func TestProfileAddressValidationLeavesToolbarTargetsAccurate(t *testing.T) {
	for _, width := range []int{18, 25, 40, 69, 70, 86, 112} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := profileInteractionsModel(&fakeBackend{}, max(40, width), 24)
			m.profilesUI.url.SetValue("1243")
			m.beginProfileURLImport()
			block := m.profilesToolbar(width)
			for _, line := range block.lines {
				if ansi.StringWidth(line) > width {
					t.Fatalf("validation overflowed toolbar: %q", ansi.Strip(line))
				}
			}
			for _, hit := range block.targets {
				if hit.X < 0 || hit.Y < 0 || hit.X+hit.W > width || hit.Y+hit.H > len(block.lines) {
					t.Fatalf("validation invalidated toolbar hit region: %+v", hit)
				}
				if hit.ID == "profiles.add" && !strings.Contains(ansi.Strip(ansi.Cut(block.lines[hit.Y], hit.X, hit.X+hit.W)), "Add") {
					t.Fatal("Add hit region no longer matches its rendered row")
				}
			}
		})
	}
}

func TestProfileAddressValidationClearsWhenLeavingPage(t *testing.T) {
	m := profileInteractionsModel(&fakeBackend{}, 120, 32)
	m.profilesUI.url.SetValue("1243")
	m.beginProfileURLImport()
	m.navigate("settings")
	m.reconcileProfiles()
	if m.profilesUI.urlError != "" || m.profilesUI.url.Value() != "1243" {
		t.Fatal("navigation carried local validation to another page or discarded the draft")
	}
}

func TestProfileAddressValidationAcceptsSourcesWithoutStartingNetwork(t *testing.T) {
	for _, source := range []string{"https://example.com/sub?token=private", "http://localhost:8080/sub", "/tmp/config.yaml"} {
		if err := validateProfileSource(source); err != nil {
			t.Errorf("valid source %q rejected: %v", source, err)
		}
	}
}

func TestProfileAddressEditKeepsMalformedURLInsideDialog(t *testing.T) {
	calls := 0
	m := profileInteractionsModel(&fakeBackend{updateProfileAddress: func(context.Context, string, string) error {
		calls++
		return nil
	}}, 120, 32)
	m.openProfileEdit("profile-url", "work")
	m.input.SetValue("https://")
	m.formKey(special(tea.KeyEnter))
	if calls != 0 || m.busy || m.form != "profile-url" || m.failure == "" || m.input.Value() != "https://" {
		t.Fatal("malformed address closed its dialog or escaped validation")
	}
}
