package app

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type WebsiteTestResult struct {
	StatusCode int
	Duration   time.Duration
}

// TestWebsite measures the time to receive the first site's response headers
// using the current routing rules. Redirects are returned to the caller.
func (a *App) TestWebsite(ctx context.Context, site Website) (WebsiteTestResult, error) {
	site, err := site.normalized()
	if err != nil {
		return WebsiteTestResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client, err := a.runtimeProxyClient(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return WebsiteTestResult{}, ctx.Err()
		}
		return WebsiteTestResult{}, err
	}
	defer client.CloseIdleConnections()
	return probeWebsite(ctx, client, site.URL)
}

func probeWebsite(ctx context.Context, client *http.Client, endpoint string) (WebsiteTestResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return WebsiteTestResult{}, err
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return WebsiteTestResult{}, ctx.Err()
		}
		return WebsiteTestResult{}, errors.New("website request through Mihomo failed")
	}
	defer resp.Body.Close()
	return WebsiteTestResult{StatusCode: resp.StatusCode, Duration: time.Since(started)}, nil
}
