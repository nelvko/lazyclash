package shellintegration

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

func New() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	cache := os.Getenv("XDG_CACHE_HOME")
	// sudo must never install into the invoking user's inherited HOME/XDG paths.
	// Plain root sessions may still use an explicitly isolated root-owned home.
	if os.Geteuid() == 0 && os.Getenv("SUDO_UID") != "" {
		account, err := user.LookupId("0")
		if err != nil {
			return nil, err
		}
		home = account.HomeDir
		config = ""
		cache = ""
	}
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	if cache == "" {
		cache = filepath.Join(home, ".cache")
	}
	for _, path := range []string{home, config, cache} {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
			return nil, fmt.Errorf("shell integration paths must be absolute and contain no line breaks")
		}
	}
	return &Manager{HomeDir: home, ConfigDir: filepath.Join(config, "lazyclash", "shell"), StateDir: filepath.Join(cache, "lazyclash", "shell")}, nil
}

// secureDir rejects symlink boundaries before creating any managed directory.
// Only the final managed directory must be private; existing XDG parents may
// legitimately have normal 0755 permissions.
func secureDir(path string) error {
	return managedDir(path, true)
}

func managedDir(path string, create bool) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("managed directory must be absolute")
	}
	clean := filepath.Clean(path)
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(clean, current), string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) && create {
			if err = os.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe managed directory: %s", current)
		}
		if index == len(parts)-1 && (!owned(info) || info.Mode().Perm()&0077 != 0) {
			return fmt.Errorf("managed directory must be private and owned by the current user: %s", current)
		}
	}
	return nil
}

func privateRead(path string) ([]byte, error) {
	if err := managedDir(filepath.Dir(path), false); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !owned(info) || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("unsafe shell integration file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, after) {
		return nil, fmt.Errorf("shell integration file changed while reading")
	}
	data, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64*1024 {
		return nil, fmt.Errorf("oversized shell integration state")
	}
	return data, nil
}

func atomicFile(path string, data []byte) error {
	if err := secureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || !owned(info) || info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("unsafe shell integration target: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".lazyclash-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func jsonFile(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return atomicFile(path, append(data, '\n'))
}
func readJSON(path string, value any) error {
	data, err := privateRead(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
func randomID() (string, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
func validID(id string) bool {
	if len(id) != 48 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (m *Manager) ensureState() error {
	if strings.ContainsAny(m.StateDir, "\r\n\x00") || strings.ContainsAny(m.ConfigDir, "\r\n\x00") {
		return fmt.Errorf("shell integration paths cannot contain line breaks")
	}
	for _, dir := range []string{m.StateDir, filepath.Join(m.StateDir, "pending"), filepath.Join(m.StateDir, "sessions"), filepath.Join(m.StateDir, "metadata")} {
		if err := secureDir(dir); err != nil {
			return err
		}
	}
	return nil
}
