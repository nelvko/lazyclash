//go:build !linux

package app

import "net"

// Managed systemd services run on Linux. A live socket on another platform
// cannot be identified as belonging to one of these services.
func socketPeerPID(*net.UnixConn) int { return 0 }
