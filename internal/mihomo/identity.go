package mihomo

import (
	"context"
	"os/exec"
)

type executionIdentity struct{ uid, gid int }
type executionIdentityKey struct{}

// WithIdentity runs version and configuration probes as the dedicated service
// account. Callers must make the probe's input files accessible to this account.
func WithIdentity(ctx context.Context, uid, gid int) context.Context {
	return context.WithValue(ctx, executionIdentityKey{}, executionIdentity{uid, gid})
}

func executableCommand(ctx context.Context, bin string, args ...string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	identity, set := ctx.Value(executionIdentityKey{}).(executionIdentity)
	if err := setExecutionIdentity(cmd, identity, set); err != nil {
		return nil, err
	}
	return cmd, nil
}
