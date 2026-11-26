//go:build linux

package app

import (
	"net"

	"golang.org/x/sys/unix"
)

func socketPeerPID(conn *net.UnixConn) int {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0
	}
	var pid int
	_ = raw.Control(func(fd uintptr) {
		peer, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err == nil {
			pid = int(peer.Pid)
		}
	})
	return pid
}
