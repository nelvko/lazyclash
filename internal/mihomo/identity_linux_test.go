//go:build linux

package mihomo

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestRootMihomoExecutionRequiresDedicatedIdentity(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to verify refusing a root execution")
	}
	for _, ctx := range []context.Context{
		context.Background(),
		WithIdentity(context.Background(), 0, 65534),
		WithIdentity(context.Background(), 65534, 0),
		WithIdentity(context.Background(), -1, 65534),
	} {
		if _, err := executableCommand(ctx, "/usr/bin/id", "-u"); err == nil {
			t.Fatal("root execution without a dedicated UID/GID was accepted")
		}
	}
}

func TestRootMihomoExecutionDropsIdentityAndSupplementaryGroups(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to verify a real UID/GID transition")
	}
	ctx := WithIdentity(context.Background(), 65534, 65534)
	for _, test := range []struct {
		flag string
		want string
	}{
		{"-u", "65534"},
		{"-g", "65534"},
		{"-G", "65534"},
	} {
		cmd, err := executableCommand(ctx, "/usr/bin/id", test.flag)
		if err != nil {
			t.Fatal(err)
		}
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("id %s: %v", test.flag, err)
		}
		if got := strings.TrimSpace(string(out)); got != test.want {
			t.Fatalf("id %s = %q, want %q", test.flag, got, test.want)
		}
		if len(cmd.Env) != 3 || cmd.Dir != "/" {
			t.Fatalf("root environment or working directory leaked: env=%v dir=%q", cmd.Env, cmd.Dir)
		}
	}
}
