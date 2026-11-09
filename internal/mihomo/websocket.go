package mihomo

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/nelvko/lazyclash/internal/diagnostic"
)

const metricFrameTimeout = 8 * time.Second

// streamJSON owns one local authenticated WebSocket. A source's owner decides
// when to reconnect. Metrics have a frame deadline; quiet logs have none.
// The read limit bounds each message, rather than the lifetime of the stream.
func (c Client) streamJSON(ctx context.Context, path string, limit int64, frameTimeout time.Duration, consume func([]byte) error) error {
	base, err := url.Parse(c.BaseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return diagnostic.New("diagnostic.api.controllerURL", "invalid local controller URL")
	}
	if err := loopbackAddress(base.Host); err != nil {
		return err
	}
	client := &http.Client{}
	if c.HTTP != nil {
		*client = *c.HTTP
	}
	// Only establishing a connection is bounded. An idle log stream may have
	// no messages for hours; the session context owns its lifetime.
	client.Timeout = 0
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	closeIdle := func() {}
	if tr, ok := transport.(*http.Transport); ok {
		tr = tr.Clone()
		tr.Proxy = nil
		tr.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
		tr.TLSHandshakeTimeout = 5 * time.Second
		tr.ResponseHeaderTimeout = 5 * time.Second
		transport, closeIdle = tr, tr.CloseIdleConnections
	}
	defer closeIdle()
	client.Transport = transport
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	dialCtx, cancelDial := context.WithTimeout(ctx, 5*time.Second)
	conn, response, err := websocket.Dial(dialCtx, strings.TrimRight(c.BaseURL, "/")+path, &websocket.DialOptions{
		HTTPClient:      client,
		HTTPHeader:      http.Header{"Authorization": {"Bearer " + c.Secret}},
		CompressionMode: websocket.CompressionDisabled,
	})
	cancelDial()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if response != nil {
			code := response.StatusCode
			if code == http.StatusUnauthorized || code == http.StatusForbidden {
				return &apiHTTPError{status: code, err: diagnostic.New("diagnostic.api.httpCredentials", "Mihomo API returned HTTP %d; verify the managed listener and credentials", code)}
			}
		}
		// Errors and server close reasons can contain URLs or raw body text.
		return diagnostic.New("diagnostic.api.unreachable", "cannot reach local Mihomo API; check the service and controller port")
	}
	defer conn.CloseNow()
	conn.SetReadLimit(limit)
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.StreamReady != nil {
		c.StreamReady()
	}
	invalid := func() error {
		return diagnostic.New("diagnostic.runtime.response", "invalid or interrupted Mihomo %s response", strings.TrimPrefix(strings.SplitN(path, "?", 2)[0], "/"))
	}
	for {
		readCtx, cancelRead := ctx, func() {}
		if frameTimeout > 0 {
			readCtx, cancelRead = context.WithTimeout(ctx, frameTimeout)
		}
		kind, raw, err := conn.Read(readCtx)
		timedOut := errors.Is(readCtx.Err(), context.DeadlineExceeded)
		cancelRead()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if timedOut {
				return diagnostic.New("diagnostic.api.sampleTimeout", "Mihomo API sample timed out")
			}
			return invalid()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if kind != websocket.MessageText {
			return invalid()
		}
		if err := consume(raw); err != nil {
			return err
		}
	}
}

func (c Client) StreamTraffic(ctx context.Context, emit func(Traffic)) error {
	if emit == nil {
		return diagnostic.New("diagnostic.api.consumer", "a live data consumer is required")
	}
	return c.streamJSON(ctx, "/traffic", maxRuntimeSample, metricFrameTimeout, func(raw []byte) error {
		value, err := decodeTraffic(raw, true)
		if err == nil {
			emit(value)
		}
		return err
	})
}

func (c Client) StreamMemory(ctx context.Context, emit func(uint64)) error {
	if emit == nil {
		return diagnostic.New("diagnostic.api.consumer", "a live data consumer is required")
	}
	first := true
	return c.streamJSON(ctx, "/memory", maxRuntimeSample, metricFrameTimeout, func(raw []byte) error {
		value, err := decodeMemory(raw)
		if err != nil {
			return err
		}
		if first {
			first = false
			return nil
		}
		emit(value)
		return nil
	})
}

func (c Client) StreamConnections(ctx context.Context, emit func(ConnectionSnapshot)) error {
	if emit == nil {
		return diagnostic.New("diagnostic.api.consumer", "a live data consumer is required")
	}
	return c.streamJSON(ctx, "/connections?interval=1000", MaxConfigSize, metricFrameTimeout, func(raw []byte) error {
		value, err := decodeConnections(raw)
		if err == nil {
			emit(value)
		}
		return err
	})
}
