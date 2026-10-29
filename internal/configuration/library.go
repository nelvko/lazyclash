// Package configuration stores configuration inputs independently of a core's
// runtime. Callers serialize mutations with the application lock.
package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/nelvko/lazyclash/internal/store"
)

const MaxDocumentSize = 8 << 20

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,47}$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidName(name string) bool { return namePattern.MatchString(name) && name != "default" }

// Subscription is a download source, not an enabled configuration. Address is
// optional for one-time imports and must never be included in public listings.
type Subscription struct {
	Format    string    `json:"format"`
	Address   string    `json:"address,omitempty"`
	Revision  string    `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Profile is a recipe. Full uses exactly one complete source. Managed uses a
// user template and extracts only inline proxies from the ordered sources.
// Overrides are user data; downloaded snapshots are never edited in place.
type Profile struct {
	Core      string   `json:"core"`
	Mode      string   `json:"mode"`
	Source    string   `json:"source,omitempty"`
	Sources   []string `json:"sources,omitempty"`
	Template  string   `json:"template,omitempty"`
	Overrides string   `json:"overrides,omitempty"`
}

func (p Profile) SourceNames() []string {
	if p.Mode == "full" {
		return []string{p.Source}
	}
	return append([]string(nil), p.Sources...)
}

type ProfileRecord struct {
	Spec      Profile `json:"spec"`
	Validated bool    `json:"validated"` // engine validation, not network readiness
}

// One atomic index commits relationships and revisions together. Blobs are
// written first; an interrupted write can leave an unreferenced private blob,
// but cannot publish half of an updated subscription or a dangling recipe.
type Library struct {
	Version       int                      `json:"version"`
	Subscriptions map[string]Subscription  `json:"subscriptions"`
	Profiles      map[string]ProfileRecord `json:"profiles"`
}

type Revision struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type Repository struct{ Root string }

func Hash(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func (r Repository) Load() (Library, []byte, error) {
	lib := Library{Version: 1, Subscriptions: map[string]Subscription{}, Profiles: map[string]ProfileRecord{}}
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
	if json.Unmarshal(raw, &lib) != nil || lib.validate() != nil {
		return Library{}, nil, errors.New("invalid configuration library; preserving it for inspection")
	}
	return lib, raw, nil
}

func (l Library) validate() error {
	invalid := errors.New("invalid configuration library")
	if l.Version != 1 || l.Subscriptions == nil || l.Profiles == nil {
		return invalid
	}
	for name, source := range l.Subscriptions {
		if !ValidName(name) || source.Format == "" || !revisionPattern.MatchString(source.Revision) {
			return invalid
		}
	}
	for name, record := range l.Profiles {
		p := record.Spec
		if !ValidName(name) || p.Core == "" || len(p.Template) > MaxDocumentSize || len(p.Overrides) > MaxDocumentSize {
			return invalid
		}
		switch p.Mode {
		case "full":
			if p.Source == "" || len(p.Sources) != 0 || p.Template != "" {
				return invalid
			}
		case "managed":
			if p.Source != "" || len(p.Sources) == 0 || p.Template == "" {
				return invalid
			}
		default:
			return invalid
		}
		seen := map[string]bool{}
		for _, source := range p.SourceNames() {
			if _, ok := l.Subscriptions[source]; !ok || seen[source] {
				return invalid
			}
			seen[source] = true
		}
	}
	return nil
}

// Save publishes an index after the caller has Put/Read the referenced input
// revisions and validated affected recipes. It deliberately does not read every
// blob: unrelated corruption must not prevent removing its library references.
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
		return "", errors.New("source must be nonempty and at most 8 MiB")
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
			return "", errors.New("source snapshot was modified; preserving it")
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
		return nil, errors.New("invalid source revision")
	}
	for _, path := range []string{r.Root, filepath.Join(r.Root, "snapshots")} {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("source snapshot directory is unsafe")
		}
	}
	raw, err := store.Read(filepath.Join(r.Root, "snapshots", revision+".yaml"))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxDocumentSize || Hash(raw) != revision {
		return nil, errors.New("source snapshot was modified; preserving it")
	}
	return raw, nil
}
