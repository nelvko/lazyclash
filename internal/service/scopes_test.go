package service

import (
	"context"
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
