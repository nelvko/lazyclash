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

func (a *App) source(ctx context.Context, source string) ([]byte, error) {
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		req, err := http.NewRequestWithContext(ctx, "GET", source, nil)
		if err != nil {
			return nil, errors.New("invalid subscription URL")
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
				return nil, ctx.Err()
			}
			return nil, errors.New("subscription download failed (URL withheld)")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("subscription download returned HTTP %d", resp.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, configuration.MaxDocumentSize+1))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("cannot read subscription response")
		}
		if len(raw) > configuration.MaxDocumentSize {
			return nil, errors.New("subscription exceeds 8 MiB")
		}
		return raw, nil
	}
	if !filepath.IsAbs(source) {
		return nil, errors.New("subscription source must be an HTTP(S) URL or absolute file path")
	}
	raw, err := store.Read(source)
	if err != nil {
		return nil, errors.New("cannot read subscription file")
	}
	return raw, nil
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
