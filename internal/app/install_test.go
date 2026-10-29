package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nelvko/lazyclash/internal/store"
)

func TestFreshStagingHasNoServiceDependencyOrPublishedState(t *testing.T) {
	a, _ := testApp(t)
	a.Service = nil
	a.PlatformError = errors.New("service manager unavailable")
	if err := store.Dir(a.Home); err != nil {
		t.Fatal(err)
	}
	m := manifest{ID: "fixture", Service: a.Name, Controller: "127.0.0.1:9097", Secret: "fixture-secret", Port: 7897, Applied: "default"}
	stage, err := a.stageFreshInstallation(t.Context(), m, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage.path)
	if stage.manifest.Version == "" || stage.manifest.BinarySHA == "" || len(stage.tree) == 0 {
		t.Fatal("staging did not produce a verified installation")
	}
	if len(read(t, filepath.Join(stage.path, "current", "config.yaml"))) == 0 {
		t.Fatal("staging did not include a usable configuration")
	}
	assertMissing(t, filepath.Join(a.Home, "installation"))
	assertMissing(t, filepath.Join(a.Home, "install-pending.json"))
}

type registrationObserver struct {
	Services
	observe func(context.Context, string, string, string)
}

func (s registrationObserver) Install(ctx context.Context, bin, config, data string) error {
	s.observe(ctx, bin, config, data)
	return s.Services.Install(ctx, bin, config, data)
}

func TestFirstInstallationRegistersValidatedPublishedPathsBeforeCommitting(t *testing.T) {
	for _, outcome := range []string{"success", "registration-error", "registration-cancel"} {
		t.Run(outcome, func(t *testing.T) {
			a, svc := testApp(t)
			addFixtureProfile(t, a, "saved")
			profile := savedSourcePath(t, a, "saved")
			before := string(read(t, profile))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var events []string
			download, version, validate := a.Download, a.Version, a.Validate
			a.Download = func(ctx context.Context, dest string, progress func(string)) error {
				events = append(events, "download")
				return download(ctx, dest, progress)
			}
			a.Version = func(ctx context.Context, bin string) (string, error) {
				events = append(events, "executable")
				return version(ctx, bin)
			}
			a.Validate = func(ctx context.Context, bin, config, data string) error {
				events = append(events, "configuration")
				assertMissing(t, filepath.Join(a.Home, "installation"))
				assertMissing(t, filepath.Join(a.Home, "install-pending.json"))
				return validate(ctx, bin, config, data)
			}
			a.Service = registrationObserver{Services: svc, observe: func(ctx context.Context, bin, config, data string) {
				if !reflect.DeepEqual(events, []string{"download", "executable", "configuration"}) {
					t.Fatalf("registration before validation: %v", events)
				}
				actualBin, actualConfig, actualData := a.paths()
				if bin != actualBin || config != actualConfig || data != actualData {
					t.Fatalf("service references staging paths: %q %q %q", bin, config, data)
				}
				m, _, err := a.load()
				if err != nil {
					t.Fatal("files were not published before service registration", err)
				}
				var journal installJournal
				if err := json.Unmarshal(read(t, filepath.Join(a.Home, "install-pending.json")), &journal); err != nil || journal.ID != m.ID {
					t.Fatalf("recovery journal was committed too early: %v", err)
				}
				if outcome == "registration-cancel" {
					cancel()
				}
			}}
			failure := errors.New("registration failed")
			if outcome == "registration-error" {
				svc.installErr = failure
			}
			err := a.Install(ctx, "", nil)
			switch outcome {
			case "success":
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := a.load(); err != nil || svc.removes != 0 {
					t.Fatalf("successful installation was not retained: %v", err)
				}
			case "registration-error", "registration-cancel":
				want := failure
				if outcome == "registration-cancel" {
					want = context.Canceled
				}
				if !errors.Is(err, want) || svc.removes != 1 {
					t.Fatalf("registration failure did not roll back: %v, removals=%d", err, svc.removes)
				}
				assertMissing(t, filepath.Join(a.Home, "installation"))
			}
			if svc.installs != 1 || len(svc.actions) != 0 || string(read(t, profile)) != before {
				t.Fatalf("registration changed service state or saved configuration: %+v", svc)
			}
			assertMissing(t, filepath.Join(a.Home, "install-pending.json"))
			staging, err := filepath.Glob(filepath.Join(a.Home, ".install-*"))
			if err != nil || len(staging) != 0 {
				t.Fatalf("staging leaked: %v %v", staging, err)
			}
		})
	}
}

func TestFreshStagingFailureCannotRegisterService(t *testing.T) {
	for _, phase := range []string{"download", "executable", "configuration", "cancelled-validation"} {
		t.Run(phase, func(t *testing.T) {
			a, svc := testApp(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failure := errors.New("staging rejected")
			switch phase {
			case "download":
				a.Download = func(_ context.Context, dest string, _ func(string)) error {
					write(t, dest, []byte("partial executable"))
					return failure
				}
			case "executable":
				a.Version = func(context.Context, string) (string, error) { return "", failure }
			case "configuration":
				a.Validate = func(context.Context, string, string, string) error { return failure }
			case "cancelled-validation":
				a.Validate = func(ctx context.Context, _, _, _ string) error {
					cancel()
					return ctx.Err()
				}
				failure = context.Canceled
			}
			if err := a.Install(ctx, "", nil); !errors.Is(err, failure) {
				t.Fatalf("staging failure lost: %v", err)
			}
			if svc.installs != 0 || svc.removes != 0 || len(svc.actions) != 0 {
				t.Fatal("failed staging reached the service manager")
			}
			assertMissing(t, filepath.Join(a.Home, "installation"))
			assertMissing(t, filepath.Join(a.Home, "install-pending.json"))
			staging, err := filepath.Glob(filepath.Join(a.Home, ".install-*"))
			if err != nil || len(staging) != 0 {
				t.Fatalf("failed staging leaked: %v %v", staging, err)
			}
		})
	}
}
