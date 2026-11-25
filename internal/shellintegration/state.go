package shellintegration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/store"
)

const pendingTTL = 7 * 24 * time.Hour

func (m *Manager) pendingPath(owner Identity) string {
	data, _ := json.Marshal(owner)
	sum := sha256.Sum256(data)
	return filepath.Join(m.StateDir, "pending", hex.EncodeToString(sum[:])+".json")
}
func (m *Manager) sessionDir(id string) string { return filepath.Join(m.StateDir, "sessions", id) }
func (m *Manager) stateLock() (func(), error) {
	if err := m.ensureState(); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(filepath.Join(m.StateDir, ".lock")); err == nil {
		if !info.Mode().IsRegular() || !owned(info) || info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("unsafe shell integration lock")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return store.Lock(ctx, m.StateDir)
}

func (m *Manager) session(owner Identity, id string) (ownerRecord, error) {
	var record ownerRecord
	if !validID(id) {
		return record, fmt.Errorf("invalid shell session identifier")
	}
	err := readJSON(filepath.Join(m.sessionDir(id), "owner.json"), &record)
	if err != nil {
		return record, err
	}
	if record.SessionID != id || record.Owner != owner {
		return ownerRecord{}, fmt.Errorf("shell session belongs to a different shell process")
	}
	return record, nil
}

func (m *Manager) loadedSession(owner Identity) (ownerRecord, bool) {
	record, loaded, _ := m.checkedLoadedSession(owner)
	return record, loaded
}

func (m *Manager) checkedLoadedSession(owner Identity) (ownerRecord, bool, error) {
	if os.Getenv(LoadedVariable) != "1" {
		return ownerRecord{}, false, nil
	}
	if marker := os.Getenv("LAZYCLASH_SHELL_PID"); marker != "" {
		pid, err := strconv.Atoi(marker)
		if err != nil {
			return ownerRecord{}, false, fmt.Errorf("invalid loaded Shell Integration PID marker")
		}
		if pid != owner.PID {
			return ownerRecord{}, false, nil
		}
	}
	id := os.Getenv(SessionVariable)
	if !validID(id) {
		return ownerRecord{}, false, fmt.Errorf("invalid loaded Shell Integration marker; preserving the current environment")
	}
	var record ownerRecord
	if err := readJSON(filepath.Join(m.sessionDir(id), "owner.json"), &record); err != nil {
		return ownerRecord{}, false, fmt.Errorf("loaded Shell Integration state is unavailable; preserving the current environment: %w", err)
	}
	if record.SessionID != id {
		return ownerRecord{}, false, fmt.Errorf("invalid loaded Shell Integration owner record")
	}
	if record.Owner != owner {
		return ownerRecord{}, false, nil
	}
	return record, true, nil
}

func (m *Manager) readPending(owner Identity) (pendingRecord, error) {
	var pending pendingRecord
	err := readJSON(m.pendingPath(owner), &pending)
	if err != nil {
		return pending, err
	}
	if pending.Owner != owner || pending.UpdatedAt.IsZero() || time.Since(pending.UpdatedAt) > pendingTTL || pending.UpdatedAt.After(time.Now().Add(time.Minute)) {
		return pendingRecord{}, os.ErrNotExist
	}
	return pending, nil
}

func endpointRecord(endpoint Endpoint) []byte {
	return fmt.Appendf(nil, "1 %d %s %d\n", time.Now().UnixNano(), endpoint.Host, endpoint.Port)
}
func validEndpoint(endpoint Endpoint, enabled bool) error {
	if endpoint.Host != "127.0.0.1" || endpoint.Port < 0 || endpoint.Port > 65535 || (enabled && endpoint.Port == 0) {
		return fmt.Errorf("terminal proxy requires a valid loopback mixed port")
	}
	if len(endpoint.Namespace) > 256 || strings.ContainsAny(endpoint.Namespace, "\r\n\x00") {
		return fmt.Errorf("invalid endpoint namespace")
	}
	return nil
}

func (m *Manager) Current() (Status, error) {
	owner, err := m.currentOwner()
	if err != nil {
		return Status{State: "unsupported", Detail: err.Error()}, nil
	}
	status := Status{Shell: owner.Shell, State: "off"}
	if ticket, err := m.findUninstallTicket(owner); err == nil && ticket.SessionID != "" {
		status.Detail = "Shell Integration uninstall is pending shell restoration and cleanup"
		if ticket.Reason != "" {
			status.Detail += ": " + ticket.Reason
		}
	} else if err != nil && !os.IsNotExist(err) {
		return status, err
	}
	installation, err := m.installationState(owner.Shell)
	if err != nil && installation != "conflict" {
		return status, err
	}
	status.Installed = installation == "installed"
	if !status.Installed {
		status.State = installation
		if err != nil {
			status.Detail, status.Diagnostic = err.Error(), err
		}
	}
	if record, loaded, loadErr := m.checkedLoadedSession(owner); loadErr != nil {
		status.State = "error"
		status.Detail = loadErr.Error()
		return status, nil
	} else if loaded {
		status.Loaded = true
		status.SessionID = record.SessionID
		request, err := m.readRequest(record.SessionID)
		if err != nil {
			return status, err
		}
		status.Desired = request.Enabled
		status.Revision = request.Revision
		status.MixedPort = request.Port
		endpointData, err := privateRead(filepath.Join(m.sessionDir(record.SessionID), "endpoint"))
		if err != nil {
			return status, err
		}
		endpoint, err := parseEndpoint(endpointData)
		if err != nil {
			return status, err
		}
		if request.Enabled {
			if endpoint.Port == 0 {
				return status, fmt.Errorf("enabled terminal proxy has no mixed port")
			}
			status.MixedPort = endpoint.Port
		}
		ack, err := m.readAcknowledgment(record.SessionID)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			status.State = "error"
			status.Detail = err.Error()
			return status, nil
		}
		if err == nil {
			status.HookConfirmed = true
			status.Applied = ack.Enabled && ack.Error == 0 && appliedEnvironment(ack.Port)
			if ack.Error != 0 {
				status.State = "error"
				status.Detail = "the shell hook could not apply or restore proxy variables"
				return status, nil
			}
			if request.Detach && ack.Detached {
				status.State = "off"
				return status, nil
			}
			if ack.Revision == request.Revision && ack.Enabled == request.Enabled && (!request.Enabled || ack.Port == status.MixedPort) {
				if !request.Enabled {
					status.State = "off"
					return status, nil
				}
				if status.Applied {
					status.State = "on"
					return status, nil
				}
				status.State = "error"
				status.Detail = "proxy environment differs from the last shell acknowledgment"
				return status, nil
			}
		}
		if request.Enabled {
			status.State = "enabling"
		} else {
			status.State = "disabling"
		}
		return status, nil
	}
	if installation == "incomplete" || installation == "conflict" {
		return status, nil
	}
	pending, err := m.readPending(owner)
	if err == nil {
		status.Pending = true
		status.Desired = pending.Enabled
		status.Revision = pending.Revision
		status.MixedPort = pending.Endpoint.Port
		if pending.Enabled {
			status.State = "pending"
			status.ActivationCommand = m.ActivationCommand(owner.Shell)
		}
		return status, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return status, err
	}
	return status, nil
}

func appliedEnvironment(port int) bool {
	http := fmt.Sprintf("http://127.0.0.1:%d", port)
	socks := fmt.Sprintf("socks5h://127.0.0.1:%d", port)
	for _, name := range []string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY"} {
		if os.Getenv(name) != http {
			return false
		}
	}
	for _, name := range []string{"all_proxy", "ALL_PROXY"} {
		if os.Getenv(name) != socks {
			return false
		}
	}
	bypass := os.Getenv("no_proxy")
	if bypass != os.Getenv("NO_PROXY") {
		return false
	}
	seen := map[string]bool{}
	for _, item := range strings.Split(bypass, ",") {
		seen[strings.TrimSpace(item)] = true
	}
	for _, item := range []string{"localhost", "127.0.0.1", "::1"} {
		if !seen[item] {
			return false
		}
	}
	return true
}

func (m *Manager) readRequest(id string) (Request, error) {
	data, err := privateRead(filepath.Join(m.sessionDir(id), "request"))
	if err != nil {
		return Request{}, err
	}
	return parseRequest(data)
}
func (m *Manager) readAcknowledgment(id string) (Acknowledgment, error) {
	data, err := privateRead(filepath.Join(m.sessionDir(id), "ack"))
	if err != nil {
		return Acknowledgment{}, err
	}
	return parseACK(data)
}

func (m *Manager) Set(enabled bool, endpoint Endpoint) error {
	owner, err := m.currentOwner()
	if err != nil {
		return err
	}
	if err = validEndpoint(endpoint, enabled); err != nil {
		return err
	}
	unlock, err := m.stateLock()
	if err != nil {
		return err
	}
	defer unlock()
	if ticket, err := m.findUninstallTicket(owner); err == nil && ticket.SessionID != "" {
		return fmt.Errorf("Shell Integration uninstall is pending; retry shell uninstall before changing terminal proxy")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	record, loaded, loadErr := m.checkedLoadedSession(owner)
	if loadErr != nil {
		return loadErr
	}
	if enabled && !loaded {
		installed, err := m.Installation(owner.Shell)
		if err != nil {
			return err
		}
		if !installed {
			return fmt.Errorf("Shell Integration is not installed")
		}
	}
	m.gcLocked()
	if loaded {
		request, err := m.readRequest(record.SessionID)
		if err != nil {
			return err
		}
		request.Revision++
		request.Enabled = enabled
		request.Detach = false
		request.Host = endpoint.Host
		request.Port = endpoint.Port
		record.Namespace = endpoint.Namespace
		record.UpdatedAt = time.Now().UTC()
		if err = jsonFile(filepath.Join(m.sessionDir(record.SessionID), "owner.json"), record); err != nil {
			return err
		}
		if err = atomicFile(filepath.Join(m.sessionDir(record.SessionID), "endpoint"), endpointRecord(endpoint)); err != nil {
			return err
		}
		return atomicFile(filepath.Join(m.sessionDir(record.SessionID), "request"), requestBytes(request))
	}
	pending, err := m.readPending(owner)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	pending.Owner = owner
	pending.Enabled = enabled
	pending.Endpoint = endpoint
	pending.Revision++
	pending.UpdatedAt = time.Now().UTC()
	return jsonFile(m.pendingPath(owner), pending)
}

// Attach runs only while an initialization script is being sourced. The caller
// passes an existing identifier only when its private in-process state exists.
func (m *Manager) Attach(shell string, pid int, existingSession ...string) (Attachment, error) {
	owner, err := m.sourceOwner(shell, pid)
	if err != nil {
		return Attachment{}, err
	}
	unlock, err := m.stateLock()
	if err != nil {
		return Attachment{}, err
	}
	defer unlock()
	m.gcLocked()
	if len(existingSession) > 0 && existingSession[0] != "" {
		if record, err := m.session(owner, existingSession[0]); err == nil {
			return Attachment{record.SessionID, m.sessionDir(record.SessionID), filepath.Join(m.sessionDir(record.SessionID), "endpoint")}, nil
		}
		return Attachment{}, fmt.Errorf("existing shell integration session is no longer valid; keep the current environment and check shell status")
	}
	id, err := randomID()
	if err != nil {
		return Attachment{}, err
	}
	pending, err := m.readPending(owner)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Attachment{}, err
	}
	endpoint := pending.Endpoint
	if endpoint.Host == "" {
		endpoint.Host = "127.0.0.1"
	}
	if err = validEndpoint(endpoint, pending.Enabled); err != nil {
		return Attachment{}, err
	}
	revision := pending.Revision
	if revision == 0 {
		revision = 1
	}
	record := ownerRecord{SessionID: id, Owner: owner, Namespace: endpoint.Namespace, UpdatedAt: time.Now().UTC()}
	dir := m.sessionDir(id)
	if err = secureDir(dir); err != nil {
		return Attachment{}, err
	}
	if err = jsonFile(filepath.Join(dir, "owner.json"), record); err != nil {
		return Attachment{}, err
	}
	if err = atomicFile(filepath.Join(dir, "endpoint"), endpointRecord(endpoint)); err != nil {
		return Attachment{}, err
	}
	if err = atomicFile(filepath.Join(dir, "request"), requestBytes(Request{Revision: revision, Enabled: pending.Enabled, Host: endpoint.Host, Port: endpoint.Port})); err != nil {
		return Attachment{}, err
	}
	// The durable intent is retained until the hook ACKs successful synchronization.
	// On the next observation GC completes that migration; failures stay recoverable.
	return Attachment{id, dir, filepath.Join(dir, "endpoint")}, nil
}

// FinalizeAttach is called once after source has synchronized and acknowledged.
// Prompt hooks never invoke it. A failed application deliberately keeps pending.
func (m *Manager) FinalizeAttach(id string) error {
	if !validID(id) {
		return fmt.Errorf("invalid shell session")
	}
	var record ownerRecord
	if err := readJSON(filepath.Join(m.sessionDir(id), "owner.json"), &record); err != nil {
		return err
	}
	owner, err := m.sourceOwner(record.Owner.Shell, record.Owner.PID)
	if err != nil {
		return err
	}
	if owner != record.Owner || record.SessionID != id {
		return fmt.Errorf("shell session owner changed")
	}
	unlock, err := m.stateLock()
	if err != nil {
		return err
	}
	defer unlock()
	ack, err := m.readAcknowledgment(id)
	if err != nil {
		return err
	}
	request, err := m.readRequest(id)
	if err != nil {
		return err
	}
	if ack.Error != 0 || ack.Revision != request.Revision || ack.Enabled != request.Enabled {
		return fmt.Errorf("shell has not acknowledged successful initialization")
	}
	pending, err := m.readPending(owner)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if pending.Revision != ack.Revision || pending.Enabled != ack.Enabled {
		return nil
	}
	return os.Remove(m.pendingPath(owner))
}

func (m *Manager) TakeNotice() (string, error) {
	owner, err := m.currentOwner()
	if err != nil {
		return "", nil
	}
	if _, loaded := m.loadedSession(owner); loaded {
		return "", nil
	}
	pending, err := m.readPending(owner)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if pending.NoticeAttempted || !pending.Enabled {
		return "", nil
	}
	unlock, err := m.stateLock()
	if err != nil {
		return "", err
	}
	defer unlock()
	pending, err = m.readPending(owner)
	if err != nil {
		return "", err
	}
	if pending.NoticeAttempted || !pending.Enabled {
		return "", nil
	}
	if installed, err := m.Installation(owner.Shell); err != nil || !installed {
		return "", err
	}
	pending.NoticeAttempted = true
	if err = jsonFile(m.pendingPath(owner), pending); err != nil {
		return "", err
	}
	return m.ActivationCommand(owner.Shell), nil
}

// PublishEndpoint changes port information for existing sessions in this scope;
// it never changes a session's enabled intent or creates a new namespace.
func (m *Manager) PublishEndpoint(endpoint Endpoint) error {
	if err := validEndpoint(endpoint, true); err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(m.StateDir, "sessions")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	unlock, err := m.stateLock()
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := os.ReadDir(filepath.Join(m.StateDir, "sessions"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		var owner ownerRecord
		if err := readJSON(filepath.Join(m.sessionDir(entry.Name()), "owner.json"), &owner); err != nil {
			continue
		}
		if owner.Namespace != endpoint.Namespace || !m.isOwnerAlive(owner.Owner) {
			continue
		}
		if raw, err := privateRead(filepath.Join(m.sessionDir(entry.Name()), "endpoint")); err == nil {
			if previous, err := parseEndpoint(raw); err == nil && previous.Host == endpoint.Host && previous.Port == endpoint.Port {
				continue
			}
		}
		if err := atomicFile(filepath.Join(m.sessionDir(entry.Name()), "endpoint"), endpointRecord(endpoint)); err != nil {
			return err
		}
	}
	// Pending requests must also use the most recent successfully committed port.
	pendingEntries, err := os.ReadDir(filepath.Join(m.StateDir, "pending"))
	if err != nil {
		return err
	}
	for _, entry := range pendingEntries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(m.StateDir, "pending", entry.Name())
		var p pendingRecord
		if readJSON(path, &p) != nil {
			continue
		}
		if p.Endpoint.Namespace == endpoint.Namespace && m.isOwnerAlive(p.Owner) {
			if p.Endpoint.Host == endpoint.Host && p.Endpoint.Port == endpoint.Port {
				continue
			}
			p.Endpoint = endpoint
			p.Revision++
			if err := jsonFile(path, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// RequestDetach only addresses this shell. An active shell must ACK restoration
// before an uninstall worker may remove its on-disk initialization files.
func (m *Manager) RequestDetach() (string, bool, error) {
	owner, err := m.currentOwner()
	if err != nil {
		return "", false, err
	}
	unlock, err := m.stateLock()
	if err != nil {
		return "", false, err
	}
	defer unlock()
	if ticket, err := m.findUninstallTicket(owner); err == nil {
		r, err := m.readRequest(ticket.SessionID)
		if err != nil {
			return "", false, err
		}
		if !r.Detach || r.Revision != ticket.Revision {
			r.Revision = ticket.Revision
			r.Enabled = false
			r.Detach = true
			if err := atomicFile(filepath.Join(m.sessionDir(ticket.SessionID), "request"), requestBytes(r)); err != nil {
				return "", false, err
			}
		}
		return ticket.SessionID, true, nil
	} else if !os.IsNotExist(err) {
		return "", false, err
	}
	if record, loaded, loadErr := m.checkedLoadedSession(owner); loadErr != nil {
		return "", false, loadErr
	} else if loaded {
		r, err := m.readRequest(record.SessionID)
		if err != nil {
			return "", false, err
		}
		r.Revision++
		r.Enabled = false
		r.Detach = true
		ticket := uninstallTicket{SessionID: record.SessionID, Owner: owner, Revision: r.Revision, RequestedAt: time.Now().UTC()}
		if err = jsonFile(m.uninstallTicketPath(record.SessionID), ticket); err != nil {
			return "", false, err
		}
		if err = atomicFile(filepath.Join(m.sessionDir(record.SessionID), "request"), requestBytes(r)); err != nil {
			return "", false, err
		}
		if err = os.Remove(m.pendingPath(owner)); err != nil && !os.IsNotExist(err) {
			return "", false, err
		}
		return record.SessionID, true, nil
	}
	if err = os.Remove(m.pendingPath(owner)); err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	return "", false, nil
}

func (m *Manager) UninstallReady(id string) (bool, error) {
	if !validID(id) {
		return false, fmt.Errorf("invalid uninstall session")
	}
	var ticket uninstallTicket
	if err := readJSON(m.uninstallTicketPath(id), &ticket); err != nil {
		return false, err
	}
	if ticket.SessionID != id || ticket.Revision == 0 {
		return false, fmt.Errorf("invalid uninstall ticket")
	}
	if !m.isOwnerAlive(ticket.Owner) {
		return true, nil
	}
	r, err := m.readRequest(id)
	if err != nil {
		return false, err
	}
	ack, err := m.readAcknowledgment(id)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return r.Detach && r.Revision == ticket.Revision && ack.Revision == r.Revision && !ack.Enabled && ack.Detached && ack.Error == 0, nil
}

func (m *Manager) gcLocked() {
	entries, _ := os.ReadDir(filepath.Join(m.StateDir, "pending"))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(m.StateDir, "pending", entry.Name())
		var p pendingRecord
		if readJSON(path, &p) != nil {
			continue
		}
		if !m.isOwnerAlive(p.Owner) || time.Since(p.UpdatedAt) > pendingTTL {
			_ = os.Remove(path)
		}
	}
	sessions, _ := os.ReadDir(filepath.Join(m.StateDir, "sessions"))
	for _, entry := range sessions {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		dir := m.sessionDir(entry.Name())
		var owner ownerRecord
		if err := readJSON(filepath.Join(dir, "owner.json"), &owner); err != nil {
			if os.IsNotExist(err) {
				_ = removeSessionFiles(dir, true)
			}
			continue
		}
		if owner.SessionID != entry.Name() || owner.Owner.PID <= 1 || owner.Owner.Start == "" || owner.Owner.BootID == "" || owner.Owner.TTY == 0 {
			continue
		}
		if !m.isOwnerAlive(owner.Owner) {
			_ = removeSessionFiles(dir, false)
			continue
		}
		ack, err := m.readAcknowledgment(owner.SessionID)
		if err == nil && ack.Error == 0 {
			pending, err := m.readPending(owner.Owner)
			if err == nil && ack.Revision == pending.Revision && ack.Enabled == pending.Enabled {
				_ = os.Remove(m.pendingPath(owner.Owner))
			}
		}
	}
}

func (m *Manager) currentOwner() (Identity, error) {
	if m.caller != nil {
		return m.caller()
	}
	return callerIdentity()
}
func (m *Manager) sourceOwner(shell string, pid int) (Identity, error) {
	if m.attachCaller != nil {
		return m.attachCaller(shell, pid)
	}
	return attachIdentity(shell, pid)
}
func (m *Manager) isOwnerAlive(owner Identity) bool {
	if m.alive != nil {
		return m.alive(owner)
	}
	return ownerAlive(owner)
}
