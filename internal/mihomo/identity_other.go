//go:build !linux

package mihomo

import "os/exec"

func setExecutionIdentity(_ *exec.Cmd, _ executionIdentity, _ bool) error { return nil }
