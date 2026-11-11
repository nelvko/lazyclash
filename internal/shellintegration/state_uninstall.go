package shellintegration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type uninstallTicket struct {
	SessionID   string    `json:"session_id"`
	Owner       Identity  `json:"owner"`
	Revision    uint64    `json:"revision"`
	RequestedAt time.Time `json:"requested_at"`
	Reason      string    `json:"reason,omitempty"`
}

// NoteUninstallFailure keeps a bounded diagnostic for a later status/retry.
// The worker never needs to write into the interactive terminal after return.
func (m *Manager) NoteUninstallFailure(id string, cause error) error {
	if !validID(id) {
		return fmt.Errorf("invalid uninstall session")
	}
	unlock, err := m.stateLock()
	if err != nil {
		return err
	}
	defer unlock()
	var ticket uninstallTicket
	if err = readJSON(m.uninstallTicketPath(id), &ticket); err != nil {
		return err
	}
	if ticket.SessionID != id || ticket.Revision == 0 {
		return fmt.Errorf("invalid uninstall ticket")
	}
	if cause == nil {
		ticket.Reason = ""
	} else {
		ticket.Reason = strings.Join(strings.Fields(cause.Error()), " ")
		if len(ticket.Reason) > 512 {
			ticket.Reason = ticket.Reason[:512]
		}
	}
	return jsonFile(m.uninstallTicketPath(id), ticket)
}

func (m *Manager) uninstallTicketPath(id string) string {
	return filepath.Join(m.StateDir, "metadata", "uninstall-"+id+".json")
}

func (m *Manager) findUninstallTicket(owner Identity) (uninstallTicket, error) {
	entries, err := os.ReadDir(filepath.Join(m.StateDir, "metadata"))
	if err != nil {
		return uninstallTicket{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "uninstall-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "uninstall-"), ".json")
		if !validID(id) {
			continue
		}
		var ticket uninstallTicket
		if err := readJSON(m.uninstallTicketPath(id), &ticket); err != nil {
			return uninstallTicket{}, err
		}
		if ticket.SessionID != id || ticket.Revision == 0 {
			return uninstallTicket{}, fmt.Errorf("invalid uninstall ticket")
		}
		if ticket.Owner == owner {
			return ticket, nil
		}
	}
	return uninstallTicket{}, os.ErrNotExist
}

// FinishDetachedSession is called only after owned integration files have been
// removed. It cleans the acknowledged calling session without touching any
// other live terminal. Failed/unfinished restoration keeps both ticket and state.
func (m *Manager) FinishDetachedSession(id string) error {
	if id == "" {
		return nil
	}
	ready, err := m.UninstallReady(id)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("shell restoration is not acknowledged; uninstall remains pending")
	}
	unlock, err := m.stateLock()
	if err != nil {
		return err
	}
	defer unlock()
	ready, err = m.UninstallReady(id)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("shell restoration changed; preserving uninstall ticket")
	}
	dir := m.sessionDir(id)
	if err := removeSessionFiles(dir, false); err != nil {
		return err
	}
	return os.Remove(m.uninstallTicketPath(id))
}

// removeSessionFiles never follows links or removes unknown user-created data.
// Orphan cleanup is limited to generated temporary files: without owner.json,
// records with lasting meaning are deliberately preserved for diagnosis.
func removeSessionFiles(dir string, orphan bool) error {
	if err := managedDir(dir, false); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		temporary := strings.HasPrefix(name, ".lazyclash-") || strings.HasPrefix(name, "ack.") && strings.HasSuffix(name, ".tmp")
		known := name == "request" || name == "ack" || name == "endpoint" || name == "owner.json"
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || !owned(info) || !temporary && (!known || orphan) {
			return fmt.Errorf("unrecognized file in managed shell session; preserving %s", dir)
		}
	}
	// The owner record remains until all other removals finish, allowing retries.
	for _, entry := range entries {
		if entry.Name() == "owner.json" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if !orphan {
		if err := os.Remove(filepath.Join(dir, "owner.json")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Remove(dir)
}
