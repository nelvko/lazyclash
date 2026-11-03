package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
)

type tunService struct {
	*fakeService
	policy     bool
	policyHook func(bool) error
}

func (*tunService) ServiceScope() service.Scope { return service.System }
func (s *tunService) SetTUNPolicy(ctx context.Context, enabled bool, _, _, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.policyHook != nil {
		if err := s.policyHook(enabled); err != nil {
			return err
		}
	}
	s.policy = enabled
	return nil
}

func TestTUNTransactionPreservesStoppedIntentAndProfilePolicy(t *testing.T) {
	a, base := installedApp(t)
	s := &tunService{fakeService: base}
	a.Service = s
	if err := a.SetTUN(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	m, _, err := a.load()
	if err != nil || !m.TUN || !s.policy || s.active || len(s.actions) != 0 {
		t.Fatalf("stopped TUN intent lost: %+v %+v %v", m, s, err)
	}
	if err := a.Apply(t.Context(), "default"); err != nil {
		t.Fatal(err)
	}
	_, cfg, _ := a.paths()
	if on, err := mihomo.ConfigTUN(read(t, cfg)); err != nil || !on {
		t.Fatalf("apply lost TUN: %v %v", on, err)
	}
	if _, err := a.PrepareRuntimeSettings(t.Context(), RuntimeSettings{MixedPort: 17997, ControllerPort: 19097}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetTUN(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if s.policy || s.active {
		t.Fatal("TUN disable did not clear policy without starting")
	}
}

func TestTUNTransactionRollsBackUnitAndGeneration(t *testing.T) {
	for _, failure := range []string{"capabilities", "restart", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			a, base := installedApp(t)
			s := &tunService{fakeService: base}
			a.Service = s
			s.active = true
			_, old, _ := a.load()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			failed := false
			s.policyHook = func(enabled bool) error {
				if enabled && failure == "capabilities" && !failed {
					failed = true
					return errors.New("reload failed")
				}
				return nil
			}
			s.actionHook = func(_ context.Context, action string) error {
				if action == "restart" && !failed {
					failed = true
					if failure == "cancel" {
						cancel()
						return ctx.Err()
					}
					if failure == "restart" {
						return errors.New("start failed")
					}
				}
				return nil
			}
			err := a.SetTUN(ctx, true)
			m, current, loadErr := a.load()
			if err == nil || loadErr != nil || current != old || m.TUN || s.policy || !s.active {
				t.Fatalf("rollback lost old runtime: %v %v %+v %+v", err, loadErr, m, s)
			}
			assertMissing(t, filepath.Join(a.Home, "apply-pending.json"))
		})
	}
}

func TestTUNInterruptedRecoveryHonorsStop(t *testing.T) {
	a, base := installedApp(t)
	s := &tunService{fakeService: base}
	a.Service = s
	m, old, _ := a.load()
	_, config, _ := a.paths()
	raw, err := mihomo.WithTUN(read(t, config), true, a.Name)
	if err != nil {
		t.Fatal(err)
	}
	m.TUN = true
	next, err := a.writeGeneration(filepath.Join(a.Home, "installation"), m, raw)
	if err != nil {
		t.Fatal(err)
	}
	oldGuard, _ := a.generationGuard(old)
	newGuard, _ := a.generationGuard(next)
	p := pending{Old: old, New: next, Active: true, OldGuard: oldGuard, NewGuard: newGuard, TUNChange: &tunTransition{New: true}}
	encoded, _ := json.Marshal(p)
	if err := os.WriteFile(filepath.Join(a.Home, "apply-pending.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Switch(filepath.Join(a.Home, "installation", "current"), next, old); err != nil {
		t.Fatal(err)
	}
	s.policy = true
	s.active = true
	if err := a.rememberStop(); err != nil {
		t.Fatal(err)
	}
	if err := a.recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	m, current, err := a.load()
	if err != nil || current != old || m.TUN || s.policy || s.active || !reflect.DeepEqual(s.actions, []string{"stop"}) {
		t.Fatalf("recovery ignored Stop: %v %+v %+v", err, m, s)
	}
}

func TestTUNReadinessWaitsForCoreStackInitialization(t *testing.T) {
	for _, ready := range []bool{true, false} {
		a, s := installedApp(t)
		s.active = true
		reads := 0
		a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
			body := `{"version":"v1.19.33"}`
			if r.URL.Path == "/configs" {
				reads++
				body = `{"mode":"rule","tun":{"enable":false}}`
				if ready && reads >= 2 {
					body = `{"mode":"rule","tun":{"enable":true}}`
				}
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
		})}
		ctx, cancel := context.WithTimeout(t.Context(), 350*time.Millisecond)
		m, _, err := a.load()
		if err != nil {
			t.Fatal(err)
		}
		m.TUN = true
		err = a.waitReady(ctx, m)
		cancel()
		if (err == nil) != ready || reads < 2 {
			t.Fatalf("TUN stack was reported ready prematurely: %v reads=%d ready=%v", err, reads, ready)
		}
	}
}
