package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemScopeUsesSystemManagerWithoutLinger(t *testing.T) {
	s, calls := testManager(t)
	s.Scope = System
	s.run = func(_ context.Context, command string, args ...string) ([]byte, error) {
		*calls = append(*calls, command+" "+strings.Join(args, " "))
		if command != "systemctl" || (len(args) > 0 && args[0] == "--user") {
			t.Fatalf("wrong system scope: %s %v", command, args)
		}
		if strings.Contains(strings.Join(args, " "), "--property=SystemState") {
			return []byte("SystemState=running\n"), nil
		}
		return []byte("LoadState=loaded\nActiveState=inactive\nMainPID=0\n"), nil
	}
	if err := s.CheckPersistent(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := s.Inspect(t.Context())
	if err != nil || status.Scope != System || status.LingerKnown {
		t.Fatalf("system status: %+v %v", status, err)
	}
}

func TestSystemPolicyGrantsCapabilitiesOnlyForTUNAndRollsBackReload(t *testing.T) {
	s, _ := testManager(t)
	s.Scope = System
	s.Home = "/var/lib/lazyclash/test"
	s.Lifecycle = s.Home + "/installation/lazyclash"
	bin, config, data := "/managed/mihomo", "/managed/current/config.yaml", "/managed/runtime"
	if err := s.Install(t.Context(), bin, config, data); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.UnitDir, s.Name+".service")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(original), "User="+s.Account()) || strings.Contains(string(original), "CAP_NET_ADMIN") || !strings.Contains(string(original), "WantedBy=multi-user.target") || !strings.Contains(string(original), "ExecStopPost=+:") {
		t.Fatal("system proxy policy is incomplete")
	}
	failReload := false
	s.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "daemon-reload" && failReload {
			failReload = false
			return nil, errors.New("reload failed")
		}
		return []byte("FragmentPath=" + path + "\nDropInPaths=\nNeedDaemonReload=no\n"), nil
	}
	failReload = true
	if err := s.SetTUNPolicy(t.Context(), true, bin, config, data); err == nil {
		t.Fatal("failed reload accepted")
	}
	after, _ := os.ReadFile(path)
	if s.TUN || string(after) != string(original) {
		t.Fatal("failed capability reload not rolled back")
	}
	if err := s.SetTUNPolicy(t.Context(), true, bin, config, data); err != nil {
		t.Fatal(err)
	}
	enabled, _ := os.ReadFile(path)
	if !s.TUN || !strings.Contains(string(enabled), "AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW") {
		t.Fatal("TUN policy missing capabilities")
	}
	// A fresh process can still stop a known TUN unit with damaged config.
	s.TUN = false
	if err := s.VerifyForStop(t.Context(), bin, config, data); err != nil || !s.TUN {
		t.Fatalf("TUN stop ownership: %v", err)
	}
	if err := s.SetTUNPolicy(t.Context(), false, bin, config, data); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(path)
	if s.TUN || string(after) != string(original) {
		t.Fatal("TUN disable did not restore bounded proxy policy")
	}
}
