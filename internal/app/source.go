package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/configuration"
	"github.com/nelvko/lazyclash/internal/store"
)

type downloadedSource struct {
	Raw     []byte
	Headers http.Header
}

func (a *App) downloadSource(ctx context.Context, source string) (downloadedSource, error) {
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		req, err := http.NewRequestWithContext(ctx, "GET", source, nil)
		if err != nil {
			return downloadedSource{}, errors.New("invalid subscription URL")
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		defer tr.CloseIdleConnections()
		client := &http.Client{Timeout: 45 * time.Second, Transport: tr}
		if a.HTTP != nil {
			copied := *a.HTTP
			client = &copied
		}
		client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			next.Header.Del("Referer")
			if len(via) >= 10 {
				return errors.New("too many subscription redirects")
			}
			if via[0].URL.Scheme == "https" && next.URL.Scheme != "https" {
				return errors.New("subscription redirect would downgrade HTTPS")
			}
			return nil
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return downloadedSource{}, ctx.Err()
			}
			return downloadedSource{}, errors.New("subscription download failed (URL withheld)")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return downloadedSource{}, fmt.Errorf("subscription download returned HTTP %d", resp.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, configuration.MaxDocumentSize+1))
		if err != nil {
			if ctx.Err() != nil {
				return downloadedSource{}, ctx.Err()
			}
			return downloadedSource{}, errors.New("cannot read subscription response")
		}
		if len(raw) > configuration.MaxDocumentSize {
			return downloadedSource{}, errors.New("subscription exceeds 8 MiB")
		}
		return downloadedSource{Raw: raw, Headers: resp.Header.Clone()}, nil
	}
	if !filepath.IsAbs(source) {
		return downloadedSource{}, errors.New("subscription source must be an HTTP(S) URL or absolute file path")
	}
	raw, err := store.Read(source)
	if err != nil {
		return downloadedSource{}, errors.New("cannot read subscription file")
	}
	return downloadedSource{Raw: raw}, nil
}
func (a *App) validateConfig(ctx context.Context, m manifest, config []byte) error {
	bin, _, data := a.paths()
	stage, err := os.MkdirTemp(data, ".validate-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	path := filepath.Join(stage, "config.yaml")
	if err := store.Replace(path, config, nil, 0600); err != nil {
		return err
	}
	return a.Validate(ctx, bin, path, data)
}
