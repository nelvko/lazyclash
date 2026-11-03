package mihomo

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
)

// Download shares the release resolver, bounded extraction, executable checks,
// and no-replace publication with explicit-version installs.
func Download(ctx context.Context, dest string, progress func(string)) error {
	release, err := Resolve(ctx, "latest", downloadnet.Config{})
	if err != nil {
		return err
	}
	return DownloadRelease(ctx, dest, release, downloadnet.Config{}, func(p Progress) {
		if progress != nil && p.Stage != "download" {
			progress(p.Detail)
		}
	})
}

func DownloadRelease(ctx context.Context, dest string, release Release, cfg downloadnet.Config, progress func(Progress)) error {
	if err := validRelease(release); err != nil {
		return err
	}
	client, err := downloadnet.NewClient(cfg, 5*time.Minute)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	source, err := cfg.URL(release.URL)
	if err != nil {
		return err
	}
	return transfer(ctx, dest, source, release.Size, release.Version, client, progress)
}

type downloadWriter struct {
	writer            io.Writer
	report            func(Progress)
	downloaded, total int64
	started, last     time.Time
}

func (w *downloadWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.downloaded += int64(n)
	now := time.Now()
	if now.Sub(w.last) >= 100*time.Millisecond {
		w.last = now
		w.report(Progress{Stage: "download", Downloaded: w.downloaded, Total: w.total, BytesPerSecond: float64(w.downloaded) / now.Sub(w.started).Seconds(), Detail: "Downloading Mihomo release bytes"})
	}
	return n, err
}

func transfer(ctx context.Context, dest string, source string, expectedSize int64, expectedVersion string, client *http.Client, progress func(Progress)) error {
	report := func(p Progress) {
		if progress != nil {
			progress(p)
		}
	}
	if _, err := os.Lstat(dest); err == nil {
		return diagnostic.New("diagnostic.download.noReplace", "refusing to replace an existing binary")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	report(Progress{Stage: "query", Total: expectedSize, Detail: "Connecting to selected release-file source"})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return diagnostic.New("diagnostic.download.request", "cannot construct release-file request")
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return downloadnet.Failure(ctx, "release-file download", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return downloadnet.HTTPError("release-file download", resp.StatusCode)
	}
	if badArchiveMedia(resp.Header.Get("Content-Type")) {
		return diagnostic.New("diagnostic.download.contentType", "release-file source returned HTML or JSON instead of gzip")
	}
	if expectedSize > 0 && resp.ContentLength >= 0 && resp.ContentLength != expectedSize {
		return diagnostic.New("diagnostic.download.contentLength", "release-file Content-Length differs from the probed release size")
	}
	if resp.ContentLength > MaxArchiveSize {
		return diagnostic.New("diagnostic.download.archiveLimit", "Mihomo archive exceeds 64 MiB")
	}
	archive, err := os.CreateTemp(filepath.Dir(dest), ".mihomo-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	total := expectedSize
	if total == 0 && resp.ContentLength > 0 {
		total = resp.ContentLength
	}
	started := time.Now()
	writer := &downloadWriter{writer: archive, report: report, total: total, started: started, last: started}
	report(Progress{Stage: "download", Total: total, Detail: "Downloading Mihomo release bytes"})
	n, err := io.Copy(writer, io.LimitReader(resp.Body, MaxArchiveSize+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return downloadnet.Failure(ctx, "release-file download", err)
	}
	if n > MaxArchiveSize {
		return diagnostic.New("diagnostic.download.archiveLimit", "Mihomo archive exceeds 64 MiB")
	}
	if expectedSize > 0 && n != expectedSize {
		return diagnostic.New("diagnostic.download.assetSize", "release-file size differs from the probed release size")
	}
	elapsed := time.Since(writer.started).Seconds()
	report(Progress{Stage: "download", Downloaded: n, Total: total, BytesPerSecond: float64(n) / elapsed, Detail: "Release-file transfer complete"})
	if _, err = archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return diagnostic.New("diagnostic.download.invalidArchive", "Mihomo release archive is invalid")
	}
	defer gz.Close()
	report(Progress{Stage: "extract", Downloaded: n, Total: total, Detail: "Extracting and checking gzip integrity"})
	out, err := os.CreateTemp(filepath.Dir(dest), ".mihomo-binary-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	const binaryLimit = 128 << 20
	extracted, err := io.Copy(out, io.LimitReader(gz, binaryLimit+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || extracted == 0 {
		return diagnostic.New("diagnostic.download.extract", "cannot extract Mihomo release")
	}
	if extracted > binaryLimit {
		return diagnostic.New("diagnostic.download.executableLimit", "Mihomo executable exceeds 128 MiB")
	}
	if err = out.Chmod(0700); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	report(Progress{Stage: "verify", Downloaded: n, Total: total, Detail: "Verifying Mihomo executable"})
	version, err := BinaryVersion(ctx, out.Name())
	if err != nil {
		return err
	}
	if version != expectedVersion {
		return diagnostic.New("diagnostic.download.versionMismatch", "downloaded Mihomo version does not match the selected release")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Link(out.Name(), dest); err != nil {
		return diagnostic.New("diagnostic.download.commit", "commit downloaded Mihomo: %w", err)
	}
	report(Progress{Stage: "done", Downloaded: n, Total: total, Detail: "Mihomo release downloaded and checked"})
	return nil
}
