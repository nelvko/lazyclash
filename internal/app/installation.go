package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

type manifest struct {
	ID         string `json:"id"`
	Service    string `json:"service"`
	Version    string `json:"version"`
	Controller string `json:"controller"`
	Secret     string `json:"secret"`
	Port       int    `json:"mixed_port"`
	Applied    string `json:"applied"`
	SHA        string `json:"config_sha256"`
	BinarySHA  string `json:"binary_sha256"`
}

var generationName = regexp.MustCompile(`^generations/g-[a-f0-9]{24}$`)

func (a *App) paths() (string, string, string) {
	return filepath.Join(a.Home, "installation", "mihomo"), filepath.Join(a.Home, "installation", "current", "config.yaml"), filepath.Join(a.Home, "installation", "runtime")
}
func randomID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), err
}

func (a *App) load() (manifest, string, error) {
	dir := filepath.Join(a.Home, "installation")
	if _, err := os.Lstat(dir); err != nil {
		return manifest{}, "", err
	}
	for _, p := range []string{dir, filepath.Join(dir, "generations"), filepath.Join(dir, "runtime")} {
		if err := existingDir(p); err != nil {
			return manifest{}, "", fmt.Errorf("installation is incomplete or unsafe: %v", err)
		}
	}
	target, err := os.Readlink(filepath.Join(dir, "current"))
	if err != nil {
		return manifest{}, "", errors.New("installation has no valid current generation; refusing to modify")
	}
	if !generationName.MatchString(target) {
		return manifest{}, "", errors.New("invalid current generation target")
	}
	if err := existingDir(filepath.Join(dir, target)); err != nil {
		return manifest{}, "", fmt.Errorf("installation generation is incomplete or unsafe: %v", err)
	}
	raw, err := store.Read(filepath.Join(dir, target, "manifest.json"))
	if err != nil {
		return manifest{}, "", fmt.Errorf("installation manifest is unavailable: %v", err)
	}
	var m manifest
	if json.Unmarshal(raw, &m) != nil || m.ID == "" || m.Secret == "" || m.Service != a.Name {
		return m, "", errors.New("invalid installation manifest or service name mismatch")
	}
	cfg, err := store.Read(filepath.Join(dir, target, "config.yaml"))
	if err != nil {
		return m, "", fmt.Errorf("installation configuration is unavailable: %v", err)
	}
	if digest(cfg) != m.SHA {
		return m, "", errors.New("active configuration was modified; refusing to replace user changes")
	}
	bin, _, _ := a.paths()
	info, err := os.Lstat(bin)
	if err != nil {
		return m, "", fmt.Errorf("installation binary is unavailable: %v", err)
	}
	if !info.Mode().IsRegular() {
		return m, "", errors.New("installed binary is not a regular file")
	}
	hash, err := fileDigest(bin)
	if err != nil {
		return m, "", err
	}
	if hash != m.BinarySHA {
		return m, "", errors.New("installed binary was modified; refusing to operate")
	}
	// Validate stored connection settings without exposing credentials.
	if _, err := mihomo.Default(m.Controller, m.Secret, m.Port); err != nil {
		return m, "", errors.New("invalid managed controller settings")
	}
	return m, target, nil
}
func (a *App) writeGeneration(base string, m manifest, config []byte) (string, error) {
	target := "generations/g-" + randomID()
	dir := filepath.Join(base, target)
	if err := store.Dir(filepath.Join(base, "generations")); err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(dir)
		}
	}()
	m.SHA = digest(config)
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	if err := store.Replace(filepath.Join(dir, "config.yaml"), config, nil, 0600); err != nil {
		return "", err
	}
	if err := store.Replace(filepath.Join(dir, "manifest.json"), raw, nil, 0600); err != nil {
		return "", err
	}
	complete = true
	return target, nil
}

// existingDir is deliberately read-only. A damaged installation must not be
// repaired implicitly by a status query or mistaken for a fresh installation.
func existingDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("managed path is not a real directory")
	}
	return nil
}

// installationTree identifies the exact files produced before publishing an
// install. Recovery preserves the whole tree when later user changes appear.
func installationTree(base string) (map[string]string, error) {
	tree := make(map[string]string)
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			tree[rel] = "directory"
		case entry.Type()&os.ModeSymlink != 0:
			if rel != "current" {
				return errors.New("installation contains an unexpected symbolic link")
			}
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if !generationName.MatchString(target) {
				return errors.New("installation current target is invalid")
			}
			tree[rel] = "symlink:" + target
		case entry.Type().IsRegular():
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Size() > 256<<20 {
				return errors.New("installation file exceeds size limit")
			}
			hash, err := fileDigest(path)
			if err != nil {
				return err
			}
			tree[rel] = "file:" + hash
		default:
			return errors.New("installation contains a non-regular file")
		}
		return nil
	})
	return tree, err
}
