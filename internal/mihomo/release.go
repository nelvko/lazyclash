package mihomo

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
)

const releaseAPI = "https://api.github.com/repos/MetaCubeX/mihomo/releases/"
const releaseFiles = "https://github.com/MetaCubeX/mihomo/releases/download/"
const archiveLimit = 64 << 20

var stableTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

type Release struct {
	Version        string `json:"version"`
	Asset          string `json:"asset"`
	URL            string `json:"url"`
	SHA256         string `json:"sha256"`
	ChecksumSource string `json:"checksum_source"`
	Size           int64  `json:"size"`
	Platform       string `json:"platform"`
}

type Progress struct {
	Stage             string
	Downloaded, Total int64
	BytesPerSecond    float64
	Detail            string
}

// Resolver exposes HTTP and architecture seams for local fixtures. Production
// always requests the official API URL; source settings only rewrite files.
type Resolver struct {
	HTTP *http.Client
	Arch string
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
		return "mihomo-linux-amd64-compatible-" + version + ".gz", nil
	default:
		return "", diagnostic.New("diagnostic.release.architecture", "Mihomo release download does not support Linux %s", arch)
	}
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
	client, err := r.client(cfg, 30*time.Second)
	if err != nil {
		return Release{}, err
	}
	if r.HTTP == nil {
		defer client.CloseIdleConnections()
	}
	endpoint := releaseAPI + "latest"
	if selection != "" && selection != "latest" {
		if !strings.HasPrefix(selection, "v") {
			selection = "v" + selection
		}
		if !stableTag.MatchString(selection) {
			return Release{}, diagnostic.New("diagnostic.release.versionChoice", "choose latest or a stable Mihomo version such as v1.19.33")
		}
		endpoint = releaseAPI + "tags/" + selection
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	// Release metadata must be answered at this official endpoint. A file
	// source may redirect to a CDN; the digest authority may not redirect.
	queryClient := *client
	queryClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := queryClient.Do(req)
	if err != nil {
		return Release{}, downloadnet.Failure(ctx, "official version query", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, downloadnet.HTTPError("official version query", resp.StatusCode)
	}
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if media != "" && media != "application/json" && media != "application/vnd.github+json" {
		return Release{}, diagnostic.New("diagnostic.release.contentType", "official version query returned an unexpected content type")
	}
	const metadataLimit = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, metadataLimit+1))
	if err != nil {
		return Release{}, downloadnet.Failure(ctx, "official version response", err)
	}
	if len(raw) > metadataLimit {
		return Release{}, diagnostic.New("diagnostic.release.metadataLimit", "official release metadata exceeds 4 MiB")
	}
	var result struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"assets"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return Release{}, diagnostic.New("diagnostic.release.invalidJSON", "official version query did not return valid release JSON")
	}
	if result.Draft || result.Prerelease || !stableTag.MatchString(result.Tag) {
		return Release{}, diagnostic.New("diagnostic.release.stableOnly", "only published stable Mihomo releases are supported")
	}
	if selection != "" && selection != "latest" && selection != result.Tag {
		return Release{}, diagnostic.New("diagnostic.release.versionMismatch", "official release response does not match the requested version")
	}
	arch := r.Arch
	if arch == "" {
		arch = runtime.GOARCH
	}
	asset, err := releaseAsset(result.Tag, arch)
	if err != nil {
		return Release{}, err
	}
	var release Release
	for _, a := range result.Assets {
		if a.Name != asset {
			continue
		}
		if release.Asset != "" {
			return Release{}, diagnostic.New("diagnostic.release.duplicateAsset", "official release contains duplicate matching assets")
		}
		release = Release{Version: result.Tag, Asset: a.Name, URL: a.URL, SHA256: strings.TrimPrefix(a.Digest, "sha256:"), ChecksumSource: endpoint + "#asset-digest:" + asset, Size: a.Size, Platform: "linux/" + arch}
		if !strings.HasPrefix(a.Digest, "sha256:") {
			return Release{}, diagnostic.New("diagnostic.release.missingDigest", "official GitHub release asset has no SHA256 digest; refusing unverifiable bytes")
		}
	}
	if release.Asset == "" {
		return Release{}, diagnostic.New("diagnostic.release.missingArchitecture", "requested Linux architecture asset is missing from the official release")
	}
	if err := validRelease(release); err != nil {
		return Release{}, err
	}
	release.SHA256 = strings.ToLower(release.SHA256)
	return release, nil
}
func validRelease(r Release) error {
	if !stableTag.MatchString(r.Version) {
		return diagnostic.New("diagnostic.release.invalidVersion", "invalid stable release version")
	}
	arch := strings.TrimPrefix(r.Platform, "linux/")
	asset, err := releaseAsset(r.Version, arch)
	if err != nil || r.Platform != "linux/"+arch || asset != r.Asset {
		return diagnostic.New("diagnostic.release.platformMismatch", "release asset does not match its Linux platform")
	}
	if r.URL != releaseFiles+r.Version+"/"+r.Asset {
		return diagnostic.New("diagnostic.release.unofficialURL", "release file URL is not the expected official GitHub asset")
	}
	hash, err := hex.DecodeString(r.SHA256)
	if err != nil || len(hash) != 32 {
		return diagnostic.New("diagnostic.release.invalidDigest", "official GitHub release asset has no valid SHA256 digest")
	}
	expectedSuffix := "#asset-digest:" + r.Asset
	source := strings.TrimSuffix(r.ChecksumSource, expectedSuffix)
	if source == r.ChecksumSource || (source != releaseAPI+"latest" && source != releaseAPI+"tags/"+r.Version) {
		return diagnostic.New("diagnostic.release.digestProvenance", "release checksum provenance is not the official GitHub API")
	}
	if r.Size <= 0 || r.Size > archiveLimit {
		return diagnostic.New("diagnostic.release.assetSize", "official release asset size is unavailable or exceeds 64 MiB")
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
	source, err := cfg.AssetURL(release.URL, release.Version, release.Asset)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	req.Header.Set("Range", "bytes=0-4095")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return downloadnet.Failure(ctx, "release-file connection test", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return downloadnet.HTTPError("release-file connection test", resp.StatusCode)
	}
	if badArchiveMedia(resp.Header.Get("Content-Type")) {
		return diagnostic.New("diagnostic.release.testContentType", "release-file connection test returned HTML or JSON instead of gzip")
	}
	// Never consume more than 4 KiB, even when a server ignores Range. A magic
	// probe confirms an archive-like response; SHA256 is checked during install.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return downloadnet.Failure(ctx, "release-file connection test", err)
	}
	if len(raw) < 2 || !bytes.Equal(raw[:2], []byte{0x1f, 0x8b}) {
		return diagnostic.New("diagnostic.release.testGzip", "release-file connection test did not return gzip bytes")
	}
	return nil
}
func badArchiveMedia(value string) bool {
	media, _, _ := mime.ParseMediaType(value)
	return media == "text/html" || media == "application/json" || strings.HasSuffix(media, "+json")
}
