package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nelvko/lazyclash/internal/app"
)

func TestRuntimeCommandsReportUnavailableStateWithoutInstalling(t *testing.T) {
	a, service := installBackend(t)
	var output bytes.Buffer
	stats := newStatsCommand(func() *app.App { return a })
	stats.SetOut(&output)
	if err := stats.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	var snapshot app.RuntimeSnapshot
	if err := json.Unmarshal(output.Bytes(), &snapshot); err != nil || snapshot.State != "uninstalled" || snapshot.Traffic != nil || snapshot.Connections != nil || snapshot.Memory != nil {
		t.Fatalf("misleading runtime output: %s (%v)", output.String(), err)
	}
	connections := newConnectionsCommand(func() *app.App { return a })
	connections.SilenceErrors, connections.SilenceUsage = true, true
	connections.SetOut(&output)
	if err := connections.ExecuteContext(t.Context()); err == nil {
		t.Fatal("uninstalled core appeared to have an empty live list")
	}
	for _, args := range [][]string{{"close"}, {"close", ""}, {"close", "../all"}, {"close", "a", "b"}} {
		cmd := newConnectionsCommand(func() *app.App { return a })
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		cmd.SetArgs(args)
		cmd.SetOut(&output)
		if err := cmd.ExecuteContext(t.Context()); err == nil {
			t.Fatalf("unsafe close accepted: %q", args)
		}
	}
	if service.installs != 0 || service.actions != 0 {
		t.Fatal("diagnostic command modified service state")
	}
	if _, err := os.Stat(filepath.Join(a.Home, "installation")); !os.IsNotExist(err) {
		t.Fatal("diagnostic command created an installation", err)
	}
}
