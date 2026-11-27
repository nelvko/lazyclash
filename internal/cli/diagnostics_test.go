package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/service"
)

func TestHumanStatusDistinguishesInstalledAndRunningVersions(t *testing.T) {
	s := app.Snapshot{
		Installed: true, Version: "v2", RunningVersion: "v1",
		Capabilities:    app.Capabilities{Scope: "system"},
		Service:         service.Status{Active: true, State: "active", EnableState: "enabled"},
		RuntimeSettings: app.RuntimeSettings{MixedPort: 7897}, AppliedName: "office\nprofile",
	}
	var out bytes.Buffer
	if err := printStatus(&out, i18n.New(i18n.Chinese), "lazyclash-test", s); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"系统 · lazyclash-test.service", "v2", "运行版本  v1", "http://127.0.0.1:7897", "控制 API  可用", "officeprofile"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary missing %q: %s", want, out.String())
		}
	}
}

func TestDiagnosisTreatsStoppedCoreAndLingerAsNormal(t *testing.T) {
	s := app.Snapshot{Installed: true, Capabilities: app.Capabilities{Scope: "user"},
		Service: service.Status{State: "inactive", LingerKnown: true}, APIError: "Start the service to view proxy groups"}
	var out bytes.Buffer
	if err := diagnose(&out, i18n.New(i18n.English), s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Linger") || !strings.Contains(out.String(), "lazyclash start") {
		t.Fatalf("missing actionable findings: %s", out.String())
	}
	for _, state := range []string{"failed", "active"} {
		s.Service.State, s.Service.Active = state, state == "active"
		s.APIError = "connection refused"
		if err := diagnose(&out, i18n.New(i18n.English), s); err == nil {
			t.Errorf("doctor accepted %s with an unavailable API", state)
		}
	}
}
