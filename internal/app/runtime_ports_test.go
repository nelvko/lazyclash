package app

import (
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

func TestStoppedCoreStartRejectsMixedUDPConflict(t *testing.T) {
	a, svc := installedApp(t)
	ports := availableSettings(t)
	plan, err := a.PrepareRuntimeSettings(t.Context(), ports)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyRuntimeSettings(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	a.CheckPorts = nil
	listener, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", ports.MixedPort))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	before, _ := installationTree(filepath.Join(a.Home, "installation"))
	if err := a.Control(t.Context(), "start"); err == nil || !strings.Contains(err.Error(), "udp port") {
		t.Fatal("start ignored a UDP-only proxy-port conflict", err)
	}
	after, _ := installationTree(filepath.Join(a.Home, "installation"))
	if svc.active || len(svc.actions) != 0 || !maps.Equal(before, after) {
		t.Fatal("conflicting start modified installation or reached the service action")
	}
}

func TestUDPConflictAllowsStoppedInstallationWithWarning(t *testing.T) {
	a, svc := testApp(t)
	ports := availableSettings(t)
	listener, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", ports.MixedPort))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	// Map the installer's default-port probe to temporary ports so this test
	// never reserves or depends on a developer's normal proxy listeners.
	probe := manifest{Controller: fmt.Sprintf("127.0.0.1:%d", ports.ControllerPort), Port: ports.MixedPort}
	a.CheckPorts = func(string, int) error { return (&App{}).checkPorts(probe) }
	local := filepath.Join(t.TempDir(), "mihomo")
	if err := os.WriteFile(local, []byte("fixture engine"), 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PrepareInstall(t.Context(), mihomo.Release{}, local, false)
	if err != nil || plan.BlockedReason != "" || !strings.Contains(strings.Join(plan.Warnings, " "), "change ports in Settings") {
		t.Fatalf("UDP conflict prevented stopped installation or lost its warning: %+v %v", plan, err)
	}
	if _, err := a.InstallCore(t.Context(), plan, downloadnet.Config{}, nil); err != nil {
		t.Fatal(err)
	}
	if svc.active || len(svc.actions) != 0 {
		t.Fatal("installation started or stopped a service")
	}
}
