// Package configuration stores configuration inputs independently of a core's
// runtime. Callers serialize mutations with the application lock.
package configuration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/nelvko/lazyclash/internal/store"
)

const MaxDocumentSize = 8 << 20
const LibraryVersion = 3

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,47}$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidName(name string) bool { return namePattern.MatchString(name) && name != "default" }

// Profile pins one complete downloaded or imported configuration. Overrides
// are user data; the original snapshot is never edited in place.
type Profile struct {
	Core      string `json:"core"`
	Revision  string `json:"revision"`
	Overrides string `json:"overrides,omitempty"`
}

type ProfileRecord struct {
	Spec       Profile   `json:"spec"`
	Validated  bool      `json:"validated"` // engine validation, not network readiness
	Name       string    `json:"name"`
	NameSource string    `json:"name_source"`
	NameBase   string    `json:"name_base"`
	Address    string    `json:"address,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// One atomic index commits each profile's name, address and revision together.
// Blobs are written first; interrupted writes may leave an unreferenced private
// blob, but cannot publish only part of a profile update.
type Library struct {
	Version  int                      `json:"version"`
	Profiles map[string]ProfileRecord `json:"profiles"`
}

type Repository struct{ Root string }

func Hash(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func (r Repository) Load() (Library, []byte, error) {
	lib := Library{Version: LibraryVersion, Profiles: map[string]ProfileRecord{}}
	info, err := os.Lstat(r.Root)
	if errors.Is(err, os.ErrNotExist) {
		return lib, nil, nil
	}
	if err != nil {
		return lib, nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return lib, nil, errors.New("configuration library is not a real directory")
	}
	raw, err := store.Read(filepath.Join(r.Root, "library.json"))
	if errors.Is(err, os.ErrNotExist) {
		return lib, nil, nil
	}
	if err != nil {
		return lib, nil, err
	}
	var saved Library
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&saved) != nil || saved.validate() != nil || decoder.Decode(new(any)) != io.EOF {
		return Library{}, nil, errors.New("invalid configuration library; preserving it for inspection")
	}
	return saved, raw, nil
}

func (l Library) validate() error {
	invalid := errors.New("invalid configuration library")
	if l.Version != LibraryVersion || l.Profiles == nil {
		return invalid
	}
	used := map[string]bool{NameKey("default"): true}
	for name, record := range l.Profiles {
		p := record.Spec
		if !ValidName(name) || p.Core == "" || !revisionPattern.MatchString(p.Revision) || len(p.Overrides) > MaxDocumentSize {
			return invalid
		}
		if record.Name == "" || record.NameBase == "" || record.Name != CleanName(record.Name) || record.NameBase != CleanName(record.NameBase) || used[NameKey(record.Name)] {
			return invalid
		}
		switch record.NameSource {
		case NameCustom, NameProvider, NameURL, NameFallback:
		default:
			return invalid
		}
		used[NameKey(record.Name)] = true
	}
	return nil
}

// Save publishes an index after the caller has Put/Read its snapshots and
// validated affected profiles. It does not read every blob: unrelated corruption
// must not prevent removing a profile from the library.
func (r Repository) Save(lib Library, expected []byte) error {
	if err := lib.validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > 16<<20 {
		return errors.New("configuration library exceeds 16 MiB")
	}
	if err := store.Dir(r.Root); err != nil {
		return err
	}
	return store.Replace(filepath.Join(r.Root, "library.json"), raw, expected, 0600)
}

func (r Repository) Put(raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > MaxDocumentSize {
		return "", errors.New("configuration must be nonempty and at most 8 MiB")
	}
	if err := store.Dir(r.Root); err != nil {
		return "", err
	}
	dir := filepath.Join(r.Root, "snapshots")
	if err := store.Dir(dir); err != nil {
		return "", err
	}
	revision := Hash(raw)
	path := filepath.Join(dir, revision+".yaml")
	old, err := store.Read(path)
	if err == nil {
		if Hash(old) != revision {
			return "", errors.New("configuration snapshot was modified; preserving it")
		}
		return revision, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return revision, store.Replace(path, raw, nil, 0600)
}

func (r Repository) Read(revision string) ([]byte, error) {
	if !revisionPattern.MatchString(revision) {
		return nil, errors.New("invalid configuration revision")
	}
	for _, path := range []string{r.Root, filepath.Join(r.Root, "snapshots")} {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("configuration snapshot directory is unsafe")
		}
	}
	raw, err := store.Read(filepath.Join(r.Root, "snapshots", revision+".yaml"))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxDocumentSize || Hash(raw) != revision {
		return nil, errors.New("configuration snapshot was modified; preserving it")
	}
	return raw, nil
}
