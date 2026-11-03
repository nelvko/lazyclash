package mihomo

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"mime"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
)

const releaseFiles = "https://github.com/MetaCubeX/mihomo/releases/download/"

// MaxArchiveSize bounds compressed downloads and installation disk estimates.
const MaxArchiveSize = 64 << 20
const probeLimit = 4096

var stableTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
var probeRange = regexp.MustCompile(`^bytes 0-([0-9]+)/([0-9]+|\*)$`)

type Release struct {
	Version  string `json:"version"`
	Asset    string `json:"asset"`
	URL      string `json:"url"`
	Size     int64  `json:"size"` // Observed HTTP transfer size; zero means unknown.
	Platform string `json:"platform"`
}

type Progress struct {
	Stage             string
	Downloaded, Total int64
	BytesPerSecond    float64
	Detail            string
}

// Resolver exposes HTTP and architecture seams for local fixtures. Production
// starts from official URLs and applies the selected method to every request.
type Resolver struct {
	HTTP       *http.Client
	Arch       string
	AMD64Level int // 1–3 overrides detection; an explicit Arch defaults to v1.
}

func Resolve(ctx context.Context, selection string, cfg downloadnet.Config) (Release, error) {
	return (Resolver{}).Resolve(ctx, selection, cfg)
}
func TestVersion(ctx context.Context, selection string, cfg downloadnet.Config) (Release, error) {
	return Resolve(ctx, selection, cfg)
}

func releaseAsset(version, arch string) (string, error) {
	switch arch {
	case "arm64":
		return "mihomo-linux-arm64-" + version + ".gz", nil
	case "amd64":
		return "mihomo-linux-amd64-v1-" + version + ".gz", nil
	default:
		return "", diagnostic.New("diagnostic.release.architecture", "Mihomo release download does not support Linux %s", arch)
	}
}

func releaseAssets(version, arch string, amd64Level int) ([]string, error) {
	asset, err := releaseAsset(version, arch)
	if err != nil {
		return nil, err
	}
	if arch != "amd64" {
		return []string{asset}, nil
	}
	var candidates []string
	if amd64Level >= 3 {
		candidates = append(candidates, "mihomo-linux-amd64-v3-"+version+".gz", "mihomo-linux-amd64-"+version+".gz")
	}
	if amd64Level >= 2 {
		candidates = append(candidates, "mihomo-linux-amd64-v2-"+version+".gz")
	}
	// Upstream is retiring compatible; both it and v1 use GOAMD64=v1.
	return append(candidates, asset, "mihomo-linux-amd64-compatible-"+version+".gz"), nil
}
func (r Resolver) client(cfg downloadnet.Config, timeout time.Duration) (*http.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if r.HTTP != nil {
		return r.HTTP, nil
	}
	return downloadnet.NewClient(cfg, timeout)
}
func (r Resolver) Resolve(ctx context.Context, selection string, cfg downloadnet.Config) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	version := selection
	if version == "" || version == "latest" {
		var err error
		version, err = r.LatestVersion(ctx, ChannelStable, cfg)
		if err != nil {
			return Release{}, err
		}
	} else if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if !stableTag.MatchString(version) {
		return Release{}, diagnostic.New("diagnostic.release.versionChoice", "choose latest or a stable Mihomo version such as v1.19.33")
	}
	arch := r.Arch
	amd64Level := r.AMD64Level
	if arch == "" {
		arch = runtime.GOARCH
		if amd64Level == 0 {
			amd64Level = nativeAMD64Level()
		}
	}
	candidates, err := releaseAssets(version, arch, amd64Level)
	if err != nil {
		return Release{}, err
	}
	client, err := r.client(cfg, 30*time.Second)
	if err != nil {
		return Release{}, err
	}
	if r.HTTP == nil {
		defer client.CloseIdleConnections()
	}
	for _, asset := range candidates {
		release := Release{Version: version, Asset: asset, URL: releaseFiles + version + "/" + asset, Platform: "linux/" + arch}
		size, missing, err := probeFile(ctx, client, release.URL, cfg)
		if err != nil {
			return Release{}, err
		}
		if missing {
			continue
		}
		release.Size = size
		return release, nil
	}
	return Release{}, diagnostic.New("diagnostic.release.missingArchitecture", "requested Linux architecture asset is missing from the official release")
}
func validRelease(r Release) error {
	if !stableTag.MatchString(r.Version) {
		return diagnostic.New("diagnostic.release.invalidVersion", "invalid stable release version")
	}
	arch := strings.TrimPrefix(r.Platform, "linux/")
	assets, err := releaseAssets(r.Version, arch, 3)
	matched := false
	for _, asset := range assets {
		matched = matched || asset == r.Asset
	}
	if err != nil || r.Platform != "linux/"+arch || !matched {
		return diagnostic.New("diagnostic.release.platformMismatch", "release asset does not match its Linux platform")
	}
	if r.URL != releaseFiles+r.Version+"/"+r.Asset {
		return diagnostic.New("diagnostic.release.unofficialURL", "release file URL is not the expected official GitHub asset")
	}
	if r.Size < 0 || r.Size > MaxArchiveSize {
		return diagnostic.New("diagnostic.release.assetSize", "release file size is invalid or exceeds 64 MiB")
	}
	return nil
}

func TestFile(ctx context.Context, release Release, cfg downloadnet.Config) error {
	return (Resolver{}).TestFile(ctx, release, cfg)
}
func (r Resolver) TestFile(ctx context.Context, release Release, cfg downloadnet.Config) error {
	if err := validRelease(release); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client, err := r.client(cfg, 20*time.Second)
	if err != nil {
		return err
	}
	if r.HTTP == nil {
		defer client.CloseIdleConnections()
	}
	size, missing, err := probeFile(ctx, client, release.URL, cfg)
	if err != nil {
		return err
	}
	if missing {
		return diagnostic.New("diagnostic.release.missingArchitecture", "requested Linux architecture asset is missing from the official release")
	}
	if release.Size > 0 && size > 0 && size != release.Size {
		return diagnostic.New("diagnostic.download.assetSize", "release-file size differs from the probed release size")
	}
	return nil
}

// probeFile never consumes more than 4 KiB, even when the server ignores Range.
// Only an absent asset permits trying a different compatible build. A response
// header and gzip probe establish availability, not independent authenticity.
func probeFile(ctx context.Context, client *http.Client, official string, cfg downloadnet.Config) (size int64, missing bool, err error) {
	source, err := cfg.URL(official)
	if err != nil {
		return 0, false, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	req.Header.Set("Range", "bytes=0-4095")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return 0, false, downloadnet.Failure(ctx, "release-file connection test", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return 0, true, nil
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, false, downloadnet.HTTPError("release-file connection test", resp.StatusCode)
	}
	if badArchiveMedia(resp.Header.Get("Content-Type")) {
		return 0, false, diagnostic.New("diagnostic.release.testContentType", "release-file connection test returned HTML or JSON instead of gzip")
	}
	size, expected, err := probeSize(resp)
	if err != nil {
		return 0, false, err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, probeLimit))
	if err != nil {
		return 0, false, downloadnet.Failure(ctx, "release-file connection test", err)
	}
	if err := ctx.Err(); err != nil {
		return 0, false, downloadnet.Failure(ctx, "release-file connection test", err)
	}
	if expected >= 0 && int64(len(raw)) != expected {
		return 0, false, downloadnet.Failure(ctx, "release-file connection test", io.ErrUnexpectedEOF)
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return 0, false, diagnostic.New("diagnostic.release.testGzip", "release-file connection test did not return gzip bytes")
	}
	_ = gz.Close()
	return size, false, nil
}

func probeSize(resp *http.Response) (size, expected int64, err error) {
	expected = -1
	if resp.StatusCode == http.StatusPartialContent {
		parts := probeRange.FindStringSubmatch(resp.Header.Get("Content-Range"))
		if parts == nil {
			return 0, 0, diagnostic.New("diagnostic.release.probeRange", "release-file probe returned an invalid Content-Range")
		}
		end, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end >= probeLimit || resp.ContentLength >= 0 && resp.ContentLength != end+1 {
			return 0, 0, diagnostic.New("diagnostic.release.probeRange", "release-file probe returned an invalid Content-Range")
		}
		expected = end + 1
		if parts[2] != "*" {
			size, err = strconv.ParseInt(parts[2], 10, 64)
			if err != nil || size <= end {
				return 0, 0, diagnostic.New("diagnostic.release.probeRange", "release-file probe returned an invalid Content-Range")
			}
		}
	} else if resp.ContentLength >= 0 {
		size = resp.ContentLength
		expected = min(size, probeLimit)
	}
	if size < 0 || size > MaxArchiveSize {
		return 0, 0, diagnostic.New("diagnostic.release.probeSize", "release-file size is invalid or exceeds 64 MiB")
	}
	return size, expected, nil
}

func badArchiveMedia(value string) bool {
	media, _, _ := mime.ParseMediaType(value)
	return media == "text/html" || media == "application/xhtml+xml" || media == "application/json" || strings.HasSuffix(media, "+json")
}
