package mihomo

import (
	"os"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// SafeExecutable rejects privilege-bearing binaries before version checks or
// configuration validation run outside systemd's NoNewPrivileges boundary.
// This is a preflight check, not isolation from a hostile process of the same
// UID replacing paths concurrently. Managed deployments use private paths.
func SafeExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return diagnostic.New("diagnostic.executable.inspect", "cannot inspect Mihomo executable")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return diagnostic.New("diagnostic.executable.regular", "Mihomo must be a regular executable file")
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return diagnostic.New("diagnostic.executable.setuid", "refusing a setuid or setgid Mihomo executable; install an unprivileged binary")
	}
	return checkFileCapabilities(path)
}
