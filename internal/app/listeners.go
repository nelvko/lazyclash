package app

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// Mihomo resolves these paths relative to its stable -d directory. It unlinks
// an existing Unix socket name and serves it without API authentication.
func (a *App) listenerPath(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	_, _, runtime := a.paths()
	return filepath.Join(runtime, path)
}

func (a *App) checkListenerFiles(ctx context.Context, current manifest, next RuntimeSettings) error {
	if next.HTTPSAddress != "" {
		cert, err := a.readControllerPEM(next.CertificatePath)
		if err != nil {
			return err
		}
		key, err := a.readControllerPEM(next.PrivateKeyPath)
		if err != nil {
			return err
		}
		if _, err := tls.X509KeyPair(cert, key); err != nil {
			return diagnostic.New("diagnostic.settings.tlsKeyPair", "HTTPS requires a matching PEM certificate and private key")
		}
	}
	socket := a.listenerPath(next.UnixSocket)
	if socket == "" {
		return nil
	}
	dir, err := os.Stat(filepath.Dir(socket))
	if err != nil {
		return diagnostic.New("diagnostic.settings.socketPrivate", "the Unix socket requires an existing private directory accessible only to the Mihomo account")
	}
	uid := os.Geteuid()
	if s := a.systemService(); s != nil {
		uid, _, err = s.Identity()
		if err != nil {
			return err
		}
	}
	if !dir.IsDir() || dir.Mode().Perm()&0077 != 0 {
		return diagnostic.New("diagnostic.settings.socketPrivate", "the Unix socket requires an existing private directory accessible only to the Mihomo account")
	}
	stat, ok := dir.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid {
		return diagnostic.New("diagnostic.settings.socketPrivate", "the Unix socket requires an existing private directory accessible only to the Mihomo account")
	}
	info, err := os.Lstat(socket)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	occupied := diagnostic.New("diagnostic.settings.socketOccupied", "the Unix socket path already exists and does not belong to this listener")
	if err != nil || info.Mode()&os.ModeSocket == 0 || socket != a.listenerPath(current.Listeners.UnixSocket) {
		return occupied
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != uid {
		return occupied
	}
	d := net.Dialer{Timeout: 250 * time.Millisecond}
	conn, err := d.DialContext(ctx, "unix", socket)
	if errors.Is(err, syscall.ECONNREFUSED) {
		return nil
	} // stale socket from the saved listener
	if err != nil {
		return occupied
	}
	defer conn.Close()
	status, err := a.Service.Inspect(ctx)
	if err != nil {
		return err
	}
	if !status.Active || status.PID <= 0 || socketPeerPID(conn.(*net.UnixConn)) != status.PID {
		return occupied
	}
	return nil
}

// In system scope the manager is root and runtime belongs to the service
// account. Confine data reads and reject devices, pipes and oversized inputs.
func (a *App) readControllerPEM(name string) ([]byte, error) {
	path := a.listenerPath(name)
	var f *os.File
	var err error
	if a.systemService() != nil {
		_, _, data := a.paths()
		relative, pathErr := filepath.Rel(data, path)
		if pathErr != nil || !filepath.IsLocal(relative) {
			return nil, diagnostic.New("diagnostic.settings.systemTLSPath", "put the system controller certificate and private key inside the Mihomo runtime directory")
		}
		root, rootErr := os.OpenRoot(data)
		if rootErr != nil {
			return nil, rootErr
		}
		defer root.Close()
		f, err = root.OpenFile(relative, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	} else {
		f, err = os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	}
	if err != nil {
		return nil, diagnostic.New("diagnostic.settings.tlsFile", "HTTPS certificate and private key must refer to existing regular files")
	}
	defer f.Close()
	const limit = 1 << 20
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, diagnostic.New("diagnostic.settings.tlsFile", "HTTPS certificate and private key must refer to existing regular files")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || len(data) > limit {
		return nil, diagnostic.New("diagnostic.settings.tlsFile", "HTTPS certificate and private key must refer to existing regular files")
	}
	return data, nil
}

func (a *App) listenersReady(ctx context.Context, m manifest, pid int) bool {
	m.Listeners = m.Listeners.SupportedListeners()
	for _, port := range []int{m.Port, m.Listeners.HTTPPort, m.Listeners.SOCKSPort, m.Listeners.RedirPort, m.Listeners.TProxyPort} {
		if port != 0 && a.probeProxy(ctx, port) != nil {
			return false
		}
	}
	udpReady := a.ProbeUDP
	if udpReady == nil {
		udpReady = processHasUDPListeners
	}
	if !udpReady(pid, []int{m.Port, m.Listeners.SOCKSPort, m.Listeners.TProxyPort}) {
		return false
	}
	d := net.Dialer{Timeout: 250 * time.Millisecond}
	if address := m.Listeners.HTTPSAddress; address != "" {
		target, err := mihomo.ControllerTarget(address)
		if err != nil {
			return false
		}
		// This loopback/local interface check only waits for a TLS handshake.
		// No Authorization header or application data is sent.
		probe, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		conn, err := (&tls.Dialer{NetDialer: &d, Config: &tls.Config{InsecureSkipVerify: true}}).DialContext(probe, "tcp", target)
		cancel()
		if err != nil {
			return false
		}
		_ = conn.Close()
	}
	if path := a.listenerPath(m.Listeners.UnixSocket); path != "" {
		conn, err := d.DialContext(ctx, "unix", path)
		if err != nil {
			return false
		}
		_ = conn.Close()
	}
	return true
}

// Linux exposes UDP sockets through the process's network namespace. Intersect
// their inodes with its open descriptors so a foreign socket cannot satisfy
// readiness. This works without sending traffic or temporarily binding a port.
func processHasUDPListeners(pid int, ports []int) bool {
	missing := make(map[int]bool)
	for _, port := range ports {
		if port != 0 {
			missing[port] = true
		}
	}
	if len(missing) == 0 {
		return true
	}
	proc := filepath.Join("/proc", strconv.Itoa(pid))
	candidates := map[string]int{}
	for _, table := range []string{"udp", "udp6"} {
		f, err := os.Open(filepath.Join(proc, "net", table))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 10 || fields[3] != "07" {
				continue
			}
			_, hex, ok := strings.Cut(fields[1], ":")
			if !ok {
				continue
			}
			port, err := strconv.ParseUint(hex, 16, 16)
			if err == nil && missing[int(port)] {
				candidates["socket:["+fields[9]+"]"] = int(port)
			}
		}
		_ = f.Close()
	}
	if len(candidates) < len(missing) {
		return false
	}
	fds, err := os.ReadDir(filepath.Join(proc, "fd"))
	if err != nil {
		return false
	}
	for _, fd := range fds {
		name, _ := os.Readlink(filepath.Join(proc, "fd", fd.Name()))
		if port, ok := candidates[name]; ok {
			delete(missing, port)
			if len(missing) == 0 {
				return true
			}
		}
	}
	return false
}
