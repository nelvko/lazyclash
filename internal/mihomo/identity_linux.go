//go:build linux

package mihomo

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

func setExecutionIdentity(cmd *exec.Cmd, identity executionIdentity, set bool) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if !set || identity.uid <= 0 || identity.gid <= 0 || uint64(identity.uid) > uint64(^uint32(0)) || uint64(identity.gid) > uint64(^uint32(0)) {
		return diagnostic.New("diagnostic.executable.rootIdentity", "refusing to execute Mihomo as root; a dedicated service account is required")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(identity.uid), Gid: uint32(identity.gid), Groups: []uint32{}}}
	// A service probe should not inherit root's home or environment credentials.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "LANG=C"}
	cmd.Dir = "/"
	return nil
}
