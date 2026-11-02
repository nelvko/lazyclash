package mihomo

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
)

const stableVersionURL = "https://github.com/MetaCubeX/mihomo/releases/latest/download/version.txt"
const alphaVersionURL = "https://github.com/MetaCubeX/mihomo/releases/download/Prerelease-Alpha/version.txt"

func LatestVersion(ctx context.Context, channel Channel, cfg downloadnet.Config) (string, error) {
	return (Resolver{}).LatestVersion(ctx, channel, cfg)
}

// LatestVersion reads official version.txt through the selected download method.
// It downloads no core assets and keeps no persistent cache.
func (r Resolver) LatestVersion(ctx context.Context, channel Channel, cfg downloadnet.Config) (string, error) {
	var endpoint string
	switch channel {
	case ChannelStable:
		endpoint = stableVersionURL
	case ChannelAlpha:
		endpoint = alphaVersionURL
	default:
		return "", diagnostic.New("diagnostic.update.channel", "choose the stable or alpha Mihomo channel")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := r.client(cfg, 30*time.Second)
	if err != nil {
		return "", err
	}
	if r.HTTP == nil {
		defer client.CloseIdleConnections()
	}
	endpoint, err = cfg.URL(endpoint)
	if err != nil {
		return "", err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Accept", "text/plain, application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return "", downloadnet.Failure(ctx, "official version query", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", downloadnet.HTTPError("official version query", resp.StatusCode)
	}
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if media == "text/html" || media == "application/xhtml+xml" || media == "application/json" || strings.HasSuffix(media, "+json") {
		return "", diagnostic.New("diagnostic.update.contentType", "official version query returned HTML or JSON instead of a version")
	}
	// One short token plus the trailing newline is expected. Bound even invalid
	// responses before reading them into memory, without retaining error bodies.
	const responseLimit = 256
	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil {
		return "", downloadnet.Failure(ctx, "official version response", err)
	}
	if err := ctx.Err(); err != nil {
		return "", downloadnet.Failure(ctx, "official version response", err)
	}
	if len(raw) > responseLimit {
		return "", diagnostic.New("diagnostic.update.responseLimit", "official version response exceeds 256 bytes")
	}
	// Only HTTP text whitespace around the token is permitted, never internal
	// whitespace, terminal controls, a BOM, HTML, or URL/path punctuation.
	version := strings.Trim(string(raw), " \t\r\n")
	if !validVersionToken(version) {
		return "", diagnostic.New("diagnostic.update.invalidVersion", "official version query returned an invalid Mihomo version")
	}
	if channel == ChannelStable && ChannelForVersion(version) == ChannelAlpha || channel == ChannelAlpha && ChannelForVersion(version) == ChannelStable {
		return "", diagnostic.New("diagnostic.update.channelMismatch", "official version response does not match the requested channel")
	}
	return version, nil
}
