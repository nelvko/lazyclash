package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/downloadnet"
)

func TestDownloadMethodChoicesAreExclusiveAndOnlyAccelerationShowsPrefix(t *testing.T) {
	m := ready(&fakeBackend{})
	m.openNetwork(false)
	for _, method := range []string{"direct", "environment", "accelerated"} {
		if m.wizard.draft.Method != method {
			t.Fatalf("selected %q, want %q", m.wizard.draft.Method, method)
		}
		doc, _, _ := m.wizardDocument(90)
		plain := ansi.Strip(strings.Join(doc.lines, "\n"))
		if strings.Count(plain, "(*)") != 1 {
			t.Fatalf("download methods are not exclusive: %s", plain)
		}
		prefix := false
		for _, hit := range doc.targets {
			prefix = prefix || hit.ID == "prefix"
			if hit.ID == "mirror" || hit.ID == "proxy-url" || hit.ID == "proxy-mode" {
				t.Fatalf("obsolete network combination still available: %s", hit.ID)
			}
		}
		if prefix != (method == "accelerated") {
			t.Fatalf("prefix visible for method %q: %t", method, prefix)
		}
		m.cycleOption("download-method", 1)
	}
	if m.wizard.draft.Method != "direct" {
		t.Fatal("method choices did not wrap")
	}
}

func TestAccelerationPreviewExpandsTheActualFullURL(t *testing.T) {
	m := ready(&fakeBackend{})
	m.openNetwork(false)
	m.wizard.draft = downloadnet.Config{Method: "accelerated", Prefix: "https://accelerator.example.invalid/"}
	m.wizard.release = testRelease()
	m.proxies.expanded = "PROXY"
	doc, _, _ := m.wizardDocument(300)
	if strings.Contains(ansi.Strip(strings.Join(doc.lines, "\n")), testRelease().URL) {
		t.Fatal("full URL preview should be collapsed initially")
	}
	m.wizardActivate("url-preview")
	if m.proxies.expanded != "PROXY" {
		t.Fatal("opening a download preview changed the selected proxy group")
	}
	doc, _, _ = m.wizardDocument(300)
	if !strings.Contains(ansi.Strip(strings.Join(doc.lines, "\n")), m.wizard.draft.Prefix+testRelease().URL) {
		t.Fatal("expanded preview differs from the requested acceleration URL")
	}
}

func TestEnvironmentProxyStatusNeverDisplaysCredentials(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://sensitive-user:secret-pass@proxy.example.invalid:7890")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	m := ready(&fakeBackend{})
	m.openNetwork(false)
	m.wizard.draft.Method = "environment"
	doc, _, _ := m.wizardDocument(120)
	plain := ansi.Strip(strings.Join(doc.lines, "\n"))
	if !strings.Contains(plain, "Environment proxy detected") || strings.Contains(plain, "secret-pass") || strings.Contains(plain, "sensitive-user") {
		t.Fatalf("environment status is missing or exposes credentials: %s", plain)
	}
}
