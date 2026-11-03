package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
	"github.com/nelvko/lazyclash/internal/store"
)

const delayProxyFixture = `{"proxies":{
	"MAIN":{"type":"Selector","now":"AUTO","all":["AUTO","HK","DIRECT","REJECT","BALANCE","CYCLE"]},
	"AUTO":{"type":"URLTest","now":"HK","all":["HK"]},
	"HK":{"type":"Shadowsocks","udp":true},
	"DIRECT":{"type":"Direct"},"REJECT":{"type":"Reject"},
	"BALANCE":{"type":"LoadBalance","all":["HK"]},
	"CYCLE":{"type":"Selector","now":"CYCLE","all":["CYCLE"]}
}}`

func proxyTestResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}
}

func TestProxyDelayValidatesLiveMembersAndAutomaticReferences(t *testing.T) {
	a, s := installedApp(t)
	s.active = true
	delayCalls := 0
	a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/proxies" {
			return proxyTestResponse(r, delayProxyFixture), nil
		}
		delayCalls++
		if r.Method != http.MethodGet || (r.URL.Path != "/proxies/HK/delay" && r.URL.Path != "/proxies/AUTO/delay") {
			t.Errorf("unexpected mutation or request: %s %s", r.Method, r.URL)
		}
		return proxyTestResponse(r, `{"delay":32}`), nil
	})}
	for _, tt := range []struct {
		group, node string
		ok          bool
	}{
		{"MAIN", "HK", true}, {"MAIN", "AUTO", true}, {"AUTO", "HK", true},
		{"missing", "HK", false}, {"AUTO", "DIRECT", false}, {"MAIN", "missing", false},
		{"MAIN", "DIRECT", false}, {"MAIN", "REJECT", false}, {"MAIN", "BALANCE", false}, {"MAIN", "CYCLE", false},
	} {
		t.Run(tt.group+"/"+tt.node, func(t *testing.T) {
			before := delayCalls
			result, err := a.TestProxyDelay(t.Context(), tt.group, tt.node)
			if (err == nil) != tt.ok {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if tt.ok && (result.State != "success" || result.Delay != 32) {
				t.Fatalf("result=%+v", result)
			}
			if !tt.ok && delayCalls != before {
				t.Fatal("invalid or untestable proxy reached delay endpoint")
			}
		})
	}
}

type proxyPIDService struct {
	*fakeService
	pid int
}

func (s *proxyPIDService) Inspect(ctx context.Context) (service.Status, error) {
	status, err := s.fakeService.Inspect(ctx)
	status.PID = s.pid
	return status, err
}

func TestProxyDelayReleasesLockAndRejectsChangedCore(t *testing.T) {
	for _, change := range []string{"pid", "configuration", "stop"} {
		t.Run(change, func(t *testing.T) { testProxyDelayChangedCore(t, change) })
	}
}

func testProxyDelayChangedCore(t *testing.T, change string) {
	a, s := installedApp(t)
	s.active = true
	owner := &proxyPIDService{fakeService: s, pid: 100}
	a.Service = owner
	started, finish := make(chan struct{}), make(chan struct{})
	a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/proxies" {
			return proxyTestResponse(r, delayProxyFixture), nil
		}
		close(started)
		select {
		case <-finish:
			return proxyTestResponse(r, `{"delay":42}`), nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})}
	done := make(chan error, 1)
	go func() {
		_, err := a.TestProxyDelay(t.Context(), "MAIN", "HK")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("delay request did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	lockErr := a.locked(ctx, func() error {
		switch change {
		case "pid":
			owner.pid = 200
		case "stop":
			s.active = false
		case "configuration":
			m, old, err := a.load()
			if err != nil {
				return err
			}
			_, config, _ := a.paths()
			next, err := a.writeGeneration(filepath.Join(a.Home, "installation"), m, read(t, config))
			if err != nil {
				return err
			}
			return store.Switch(filepath.Join(a.Home, "installation", "current"), next, old)
		}
		return nil
	})
	close(finish)
	if lockErr != nil {
		t.Fatalf("delay holds application lock: %v", lockErr)
	}
	if err := <-done; err == nil || (change != "stop" && !strings.Contains(err.Error(), "core changed")) {
		t.Fatalf("stale observation published: %v", err)
	}
}

func TestProxyDelayUnavailableOwnershipAndCancellation(t *testing.T) {
	for _, state := range []string{"stopped", "unowned", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			a, s := installedApp(t)
			s.active = state != "stopped"
			if state == "unowned" {
				s.verifyErr = errors.New("not owned")
			}
			calls := 0
			a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == "/proxies" {
					return proxyTestResponse(r, delayProxyFixture), nil
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			result, err := a.TestProxyDelay(ctx, "MAIN", "HK")
			if err == nil || result.State != "" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if state != "cancelled" && calls != 0 {
				t.Fatal("unavailable service reached controller")
			}
			if state == "cancelled" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost caller deadline: %v", err)
			}
		})
	}
}

func TestSnapshotGroupsAndMetadataShareOneObservation(t *testing.T) {
	a, s := installedApp(t)
	s.active = true
	proxyCalls := 0
	a.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/proxies":
			proxyCalls++
			return proxyTestResponse(r, delayProxyFixture), nil
		case "/configs":
			return proxyTestResponse(r, `{"mode":"rule","tun":{"enable":false}}`), nil
		default:
			return proxyTestResponse(r, `{"version":"v1.19.33"}`), nil
		}
	})}
	snapshot, err := a.Snapshot(t.Context())
	if err != nil || snapshot.APIError != "" || proxyCalls != 1 {
		t.Fatalf("snapshot=%+v calls=%d err=%v", snapshot, proxyCalls, err)
	}
	if len(snapshot.Groups) != 4 || len(snapshot.Proxies) != 7 || snapshot.Proxies["HK"].Type != "Shadowsocks" || !snapshot.Proxies["HK"].UDP {
		t.Fatalf("groups=%+v proxies=%+v", snapshot.Groups, snapshot.Proxies)
	}
	if groups := mihomo.ProxyGroups(snapshot.Proxies); len(groups) != len(snapshot.Groups) {
		t.Fatal("metadata and group observations differ")
	}
}
