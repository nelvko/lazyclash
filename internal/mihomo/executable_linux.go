//go:build linux

package mihomo

import (
	"errors"

	"golang.org/x/sys/unix"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

func checkFileCapabilities(path string) error {
	_, err := unix.Getxattr(path, "security.capability", nil)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	if err != nil {
		return diagnostic.New("diagnostic.executable.capabilitiesInspect", "cannot inspect Mihomo file capabilities; refusing to execute an unverified binary")
	}
	return diagnostic.New("diagnostic.executable.capabilities", "refusing a Mihomo executable with file capabilities; install an unprivileged binary")
}
