package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
)

type fakeService struct {
	active                           bool
	installs, removes                int
	actions                          []string
	installErr, removeErr, verifyErr error
	installHook                      func()
	actionHook                       func(context.Context, string) error
}

func (s *fakeService) Check(ctx context.Context) error     { return ctx.Err() }
func (s *fakeService) Available(ctx context.Context) error { return ctx.Err() }
func (s *fakeService) Inspect(ctx context.Context) (service.Status, error) {
	state := "inactive"
	if s.active {
		state = "active"
	}
	return service.Status{Active: s.active, State: state}, ctx.Err()
}
func (s *fakeService) Install(ctx context.Context, bin, config, data string) error {
	s.installs++
	if s.installHook != nil {
		s.installHook()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.installErr
}
func (s *fakeService) Verify(ctx context.Context, bin, config, data string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.verifyErr
}
func (s *fakeService) RemoveOwned(ctx context.Context, bin, config, data string) error {
	s.removes++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.removeErr
}
func (s *fakeService) Action(ctx context.Context, action string) error {
	s.actions = append(s.actions, action)
	if s.actionHook != nil {
		if err := s.actionHook(ctx, action); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.active = action != "stop"
	return nil
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testApp(t *testing.T) (*App, *fakeService) {
	t.Helper()
	a, err := New(t.TempDir(), "lazyclash-unit-test")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeService{}
	a.Service = s
	a.PlatformError = nil
	a.CheckPorts = func(string, int) error { return nil }
	a.ProbeProxy = func(context.Context, int) error { return nil }
	a.Download = func(ctx context.Context, dest string, progress func(string)) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return os.WriteFile(dest, []byte("fixture engine"), 0700)
	}
	a.Version = func(ctx context.Context, bin string) (string, error) { return mihomo.Version, ctx.Err() }
	a.Validate = func(ctx context.Context, bin, config, data string) error { return ctx.Err() }
	a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"version":"v1.19.33","meta":true}`
		if r.URL.Path == "/proxies" {
			body = `{"proxies":{"PROXY":{"type":"Selector","now":"DIRECT","all":["DIRECT","REJECT"]}}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})}
	return a, s
}
func installedApp(t *testing.T) (*App, *fakeService) {
	t.Helper()
	a, s := testApp(t)
	if err := a.Install(t.Context(), "", nil); err != nil {
		t.Fatal(err)
	}
	return a, s
}
func write(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path remains %s: %v", path, err)
	}
}
func addFixtureProfile(t *testing.T, a *App, name string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "subscription.yaml")
	write(t, p, []byte("mode: rule\nproxies: []\nproxy-groups:\n  - name: custom\n    type: select\n    proxies: [DIRECT, REJECT]\nrules: ['MATCH,custom']\n"))
	if err := a.Add(t.Context(), name, p); err != nil {
		t.Fatal(err)
	}
}

func TestInstallFailureRollsBackAndPreservesRollbackErrors(t *testing.T) {
	for _, rollbackFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "restored", true: "recovery-pending"}[rollbackFails], func(t *testing.T) {
			a, s := testApp(t)
			s.installErr = errors.New("unit install failed")
			if rollbackFails {
				s.removeErr = errors.New("unit rollback failed")
			}
			err := a.Install(t.Context(), "", nil)
			if err == nil || !strings.Contains(err.Error(), "unit install failed") {
				t.Fatalf("missing install failure: %v", err)
			}
			if s.removes != 1 {
				t.Fatalf("rollback count=%d", s.removes)
			}
			if rollbackFails {
				if !strings.Contains(err.Error(), "unit rollback failed") {
					t.Fatalf("rollback failure hidden: %v", err)
				}
				if _, err := os.Stat(filepath.Join(a.Home, "install-pending.json")); err != nil {
					t.Fatal("recovery journal lost", err)
				}
				s.removeErr = nil
				if _, err := a.Snapshot(t.Context()); err != nil {
					t.Fatal("retry recovery", err)
				}
			}
			assertMissing(t, filepath.Join(a.Home, "installation"))
			assertMissing(t, filepath.Join(a.Home, "install-pending.json"))
			entries, _ := os.ReadDir(a.Home)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".install-") {
					t.Fatal("installation stage leaked")
				}
			}
		})
	}
}

func TestInstallRefusesExistingFilesAndLeavesLocalSourceUntouched(t *testing.T) {
	a, s := testApp(t)
	existing := filepath.Join(a.Home, "installation")
	write(t, existing, []byte("user data"))
	if err := a.Install(t.Context(), "", nil); err == nil {
		t.Fatal("existing installation was overwritten")
	}
	if string(read(t, existing)) != "user data" || s.installs != 0 {
		t.Fatal("foreign data or service changed")
	}
	if err := os.Remove(existing); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "mihomo")
	write(t, local, []byte("local fixture engine"))
	if err := a.Install(t.Context(), local, nil); err != nil {
		t.Fatal(err)
	}
	if string(read(t, local)) != "local fixture engine" {
		t.Fatal("local source changed")
	}
	if s.active || len(s.actions) != 0 {
		t.Fatal("install unexpectedly started service")
	}
	if err := a.Install(t.Context(), local, nil); err == nil {
		t.Fatal("reinstall overwrote managed files")
	}
}

func TestInstallCancelledDuringPublicationCleansWithFreshContext(t *testing.T) {
	a, s := testApp(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.installHook = cancel
	if err := a.Install(ctx, "", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if s.removes != 1 {
		t.Fatal("cancelled install was not rolled back")
	}
	assertMissing(t, filepath.Join(a.Home, "installation"))
	assertMissing(t, filepath.Join(a.Home, "install-pending.json"))
}

func TestInstallRecoveryPreservesNewUserFiles(t *testing.T) {
	a, s := testApp(t)
	s.installErr = errors.New("failed unit install")
	userFile := filepath.Join(a.Home, "installation", "user-notes")
	s.installHook = func() { write(t, userFile, []byte("do not delete")) }
	err := a.Install(t.Context(), "", nil)
	if err == nil || !strings.Contains(err.Error(), "additional files") {
		t.Fatalf("unsafe recovery result: %v", err)
	}
	if string(read(t, userFile)) != "do not delete" {
		t.Fatal("user file lost")
	}
	if s.removes != 0 {
		t.Fatal("service modified before checking installation tree")
	}
	if _, err := os.Stat(filepath.Join(a.Home, "install-pending.json")); err != nil {
		t.Fatal("journal lost")
	}
}

func TestHTTPAddValidatesBeforeSavingAndDoesNotApply(t *testing.T) {
	a, s := installedApp(t)
	before := read(t, filepath.Join(a.Home, "installation", "current", "config.yaml"))
	valid := []byte("proxies: []\nproxy-groups: []\nrules: ['MATCH,DIRECT']\n")
	var fixture atomic.Value
	fixture.Store(valid)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(fixture.Load().([]byte)) }))
	defer server.Close()
	// Exercise a real local HTTP fixture rather than the readiness transport.
	a.HTTP = nil
	validations := 0
	a.Validate = func(ctx context.Context, bin, config, data string) error {
		validations++
		if strings.Contains(string(read(t, config)), "reject-engine") {
			return errors.New("engine rejected configuration")
		}
		return ctx.Err()
	}
	if err := a.Add(t.Context(), "good", server.URL+"/private-token"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(read(t, filepath.Join(a.Home, "profiles", "good.yaml")), valid) {
		t.Fatal("source was not preserved")
	}
	if !bytes.Equal(before, read(t, filepath.Join(a.Home, "installation", "current", "config.yaml"))) || len(s.actions) != 0 {
		t.Fatal("add implicitly applied subscription")
	}
	fixture.Store([]byte("private-token: ["))
	if err := a.Add(t.Context(), "bad-yaml", server.URL); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("unsafe invalid YAML error: %v", err)
	}
	assertMissing(t, filepath.Join(a.Home, "profiles", "bad-yaml.yaml"))
	fixture.Store([]byte("mode: reject-engine\n"))
	if err := a.Add(t.Context(), "bad-engine", server.URL); err == nil {
		t.Fatal("invalid engine configuration saved")
	}
	assertMissing(t, filepath.Join(a.Home, "profiles", "bad-engine.yaml"))
	if validations != 2 {
		t.Fatalf("validator count=%d", validations)
	}
	if err := a.Add(t.Context(), "good", server.URL); err == nil {
		t.Fatal("existing profile overwritten")
	}
}

func TestAddCancellationAfterResponseHeadersIsPreserved(t *testing.T) {
	a, _ := installedApp(t)
	a.HTTP = nil
	headers := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(headers)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Add(ctx, "cancelled", server.URL) }()
	<-headers
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("body cancellation lost: %v", err)
	}
	assertMissing(t, filepath.Join(a.Home, "profiles", "cancelled.yaml"))
}

func TestApplyStoppedOnlyStagesAndActiveFailureRestoresConfigAndSelection(t *testing.T) {
	a, s := installedApp(t)
	addFixtureProfile(t, a, "custom")
	if err := a.Apply(t.Context(), "custom"); err != nil {
		t.Fatal(err)
	}
	m, target, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	if m.Applied != "custom" || len(s.actions) != 0 || s.active {
		t.Fatal("staged application started service")
	}
	old := read(t, filepath.Join(a.Home, "installation", target, "config.yaml"))
	s.active = true
	failures := 0
	s.actionHook = func(ctx context.Context, action string) error {
		if action == "restart" {
			failures++
			if failures == 1 {
				return errors.New("new configuration failed restart")
			}
		}
		return nil
	}
	if err := a.Apply(t.Context(), "default"); err == nil || !strings.Contains(err.Error(), "failed restart") {
		t.Fatalf("missing restart failure: %v", err)
	}
	restored, actual, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	if actual != target || restored.Applied != "custom" || !bytes.Equal(old, read(t, filepath.Join(a.Home, "installation", "current", "config.yaml"))) {
		t.Fatal("old generation and metadata were not restored")
	}
	if failures != 2 || !s.active {
		t.Fatalf("old service not recovered: restarts=%d active=%v", failures, s.active)
	}
	assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
}

func TestApplyRecoveryRetainsJournalUntilServiceCanBeRecovered(t *testing.T) {
	a, s := installedApp(t)
	addFixtureProfile(t, a, "custom")
	before, old, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	s.active = true
	s.actionHook = func(context.Context, string) error { return errors.New("service unavailable") }
	if err := a.Apply(t.Context(), "custom"); err == nil {
		t.Fatal("failed restart accepted")
	}
	m, current, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	if current != old || m.Applied != before.Applied {
		t.Fatal("old configuration not restored")
	}
	if _, err := os.Stat(filepath.Join(a.Home, "apply-pending.json")); err != nil {
		t.Fatal("recovery journal lost")
	}
	s.actionHook = nil
	snap, err := a.Snapshot(t.Context())
	if err != nil || !snap.Installed || snap.Applied != "default" {
		t.Fatalf("recovery failed: %+v %v", snap, err)
	}
	assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
}

func TestApplyRefusesUserModifiedConfigurationAndSymlinkedDirectories(t *testing.T) {
	a, s := installedApp(t)
	addFixtureProfile(t, a, "custom")
	_, target, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(a.Home, "installation", target, "config.yaml")
	write(t, cfg, []byte("user: changed configuration\n"))
	if err := a.Apply(t.Context(), "custom"); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("user changes not detected: %v", err)
	}
	if string(read(t, cfg)) != "user: changed configuration\n" || len(s.actions) != 0 {
		t.Fatal("user changes overwritten")
	}
	a, s = installedApp(t)
	runtimePath := filepath.Join(a.Home, "installation", "runtime")
	if err := os.Remove(runtimePath); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	write(t, filepath.Join(external, "keep"), []byte("outside"))
	if err := os.Symlink(external, runtimePath); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Snapshot(t.Context()); err == nil {
		t.Fatal("symlinked runtime accepted")
	}
	if string(read(t, filepath.Join(external, "keep"))) != "outside" || len(s.actions) != 0 {
		t.Fatal("external directory changed")
	}
}

func TestSnapshotDoesNotRepairIncompleteInstallation(t *testing.T) {
	a, _ := installedApp(t)
	runtimePath := filepath.Join(a.Home, "installation", "runtime")
	if err := os.Remove(runtimePath); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Snapshot(t.Context()); err == nil {
		t.Fatal("incomplete installation presented as fresh")
	}
	assertMissing(t, runtimePath)
}

func TestPendingApplyFromInterruptedProcessRollsBackOnNextOperation(t *testing.T) {
	a, s := installedApp(t)
	m, old, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mihomo.Default(m.Controller, m.Secret, m.Port)
	if err != nil {
		t.Fatal(err)
	}
	m.Applied = "interrupted"
	next, err := a.writeGeneration(filepath.Join(a.Home, "installation"), m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(pending{Old: old, New: next, Active: false})
	if err := store.Replace(filepath.Join(a.Home, "apply-pending.json"), raw, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Switch(filepath.Join(a.Home, "installation", "current"), next, old); err != nil {
		t.Fatal(err)
	}
	snap, err := a.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Applied != "default" || len(s.actions) != 0 {
		t.Fatalf("unexpected recovered state: %+v", snap)
	}
	_, current, err := a.load()
	if err != nil || current != old {
		t.Fatalf("current=%s err=%v", current, err)
	}
	assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
}

func TestCancelledLockWaitDoesNotMutateState(t *testing.T) {
	a, _ := installedApp(t)
	unlock, err := store.Lock(t.Context(), a.Home)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := a.Apply(ctx, "default"); !errors.Is(err, context.Canceled) {
		t.Fatalf("lock cancellation lost: %v", err)
	}
	assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
}

func TestControlCancellationAfterAcceptedJobPreservesManagedService(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "running"}[active], func(t *testing.T) {
			a, s := installedApp(t)
			s.active = active
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.actionHook = func(c context.Context, action string) error {
				if action == "start" {
					s.active = true
					cancel()
					return context.Canceled
				}
				if c.Err() != nil {
					t.Fatal("cleanup inherited cancelled context")
				}
				return nil
			}
			err := a.Control(ctx, "start")
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if !s.active || len(s.actions) != 1 || s.actions[0] != "start" {
				t.Fatalf("cancelled observer undid the managed start: active=%v actions=%v", s.active, s.actions)
			}
			m, _, err := a.load()
			if err != nil {
				t.Fatal(err)
			}
			r, _, err := a.loadRuntimeRecord(m)
			if err != nil || !r.Desired.Running || r.Operation.State != "pending" {
				t.Fatalf("lost submitted intent: %+v %v", r, err)
			}
		})
	}
}

func TestSnapshotRejectsChangedUnitBeforeUsingAPI(t *testing.T) {
	a, s := installedApp(t)
	s.active = true
	s.verifyErr = errors.New("unit was modified")
	a.HTTP = &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
		t.Error("API must not be called")
		return nil, errors.New("unexpected API")
	})}
	snapshot, err := a.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ServiceError != "unit was modified" || snapshot.APIError == "" || len(snapshot.Groups) != 0 {
		t.Fatalf("%+v", snapshot)
	}
}
func TestSubscriptionRedirectDoesNotForwardTokenInReferer(t *testing.T) {
	a, _ := testApp(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "" {
			t.Error("subscription URL leaked through Referer")
		}
		_, _ = w.Write([]byte("fixture"))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	a.HTTP = nil
	raw, err := a.source(t.Context(), source.URL+"/token-in-path?token=private")
	if err != nil || string(raw) != "fixture" {
		t.Fatal(string(raw), err)
	}
}
