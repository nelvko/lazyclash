package mihomo

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Version and digests are pinned to official MetaCubeX/mihomo release assets.
// Source: https://api.github.com/repos/MetaCubeX/mihomo/releases/tags/v1.19.33
const Version = "v1.19.33"

type artifact struct{ URL, SHA256 string }

func releaseArtifact(arch string) (artifact, error) {
	var name, digest string
	switch arch {
	case "arm64":
		name = "mihomo-linux-arm64-" + Version + ".gz"
		digest = "ba408b317080ee265c46cb923a50018c1b82cbd708551cdd46a1e3e307396187"
	case "amd64":
		name = "mihomo-linux-amd64-compatible-" + Version + ".gz"
		digest = "386508bfb1c72c43b6a0eda73708ecdce82546581b0ebcb16366c53757c622b6"
	default:
		return artifact{}, fmt.Errorf("Mihomo download does not support Linux %s", arch)
	}
	return artifact{"https://github.com/MetaCubeX/mihomo/releases/download/" + Version + "/" + name, digest}, nil
}

// Download writes a new destination only after digest and executable validation.
// The caller stages its installation transaction in an owned temporary directory.
func Download(ctx context.Context, dest string, progress func(string)) error {
	art, err := releaseArtifact(runtime.GOARCH)
	if err != nil {
		return err
	}
	return download(ctx, dest, art, &http.Client{Timeout: 5 * time.Minute}, progress)
}

func download(ctx context.Context, dest string, art artifact, client *http.Client, progress func(string)) error {
	report := func(s string) {
		if progress != nil {
			progress(s)
		}
	}
	if _, err := os.Lstat(dest); err == nil {
		return errors.New("refusing to replace an existing binary")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	report("Downloading Mihomo " + Version + " from official GitHub release…")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, art.URL, nil)
	if err != nil {
		return errors.New("cannot construct release request")
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Mihomo release download failed; check network access to GitHub")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Mihomo release download returned HTTP %d", resp.StatusCode)
	}
	archive, err := os.CreateTemp(filepath.Dir(dest), ".mihomo-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	const archiveLimit = 64 << 20
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(archive, h), io.LimitReader(resp.Body, archiveLimit+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return errors.New("Mihomo release download was interrupted")
	}
	if n > archiveLimit {
		return errors.New("Mihomo archive exceeds 64 MiB")
	}
	if hex.EncodeToString(h.Sum(nil)) != art.SHA256 {
		return errors.New("Mihomo release SHA256 checksum mismatch; nothing installed")
	}
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return errors.New("Mihomo release archive is invalid")
	}
	defer gz.Close()
	out, err := os.CreateTemp(filepath.Dir(dest), ".mihomo-binary-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	const binaryLimit = 128 << 20
	n, err = io.Copy(out, io.LimitReader(gz, binaryLimit+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || n == 0 {
		return errors.New("cannot extract Mihomo release")
	}
	if n > binaryLimit {
		return errors.New("Mihomo executable exceeds 128 MiB")
	}
	if err = out.Chmod(0o700); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	report("Verifying Mihomo executable…")
	if _, err = BinaryVersion(ctx, out.Name()); err != nil {
		return err
	}
	// Link is an atomic no-replace commit even if another writer created dest.
	if err = os.Link(out.Name(), dest); err != nil {
		return fmt.Errorf("commit downloaded Mihomo: %w", err)
	}
	return nil
}
