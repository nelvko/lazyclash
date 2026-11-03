package mihomo

import (
	"context"
	"os"
	"os/exec"
)

type executionIdentity struct{ uid, gid int }
type executionIdentityKey struct{}

// WithIdentity runs version and configuration probes as the dedicated service
// account. Callers must make the probe's input files accessible to this account.
func WithIdentity(ctx context.Context, uid, gid int) context.Context {
	return context.WithValue(ctx, executionIdentityKey{}, executionIdentity{uid, gid})
}

func prepareDownloadedExecutable(ctx context.Context, file *os.File) error {
	mode := os.FileMode(0700)
	if identity, set := ctx.Value(executionIdentityKey{}).(executionIdentity); set && os.Geteuid() == 0 {
		// Keep root ownership while allowing the dedicated probe to execute it.
		if err := file.Chown(0, identity.gid); err != nil {
			return err
		}
		mode = 0750
	}
	return file.Chmod(mode)
}

func executableCommand(ctx context.Context, bin string, args ...string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	identity, set := ctx.Value(executionIdentityKey{}).(executionIdentity)
	if err := setExecutionIdentity(cmd, identity, set); err != nil {
		return nil, err
	}
	return cmd, nil
}
