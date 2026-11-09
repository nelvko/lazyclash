package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

// Traffic contains the latest per-second rates and the core's cumulative byte
// counters. Totals are never integrated from potentially missing observations.
type Traffic struct {
	Up        int64 `json:"up"`
	Down      int64 `json:"down"`
	UpTotal   int64 `json:"upTotal"`
	DownTotal int64 `json:"downTotal"`
}

type ConnectionMetadata struct {
	Network         string `json:"network"`
	Type            string `json:"type"`
	SourceIP        string `json:"sourceIP"`
	SourcePort      string `json:"sourcePort"`
	DestinationIP   string `json:"destinationIP"`
	DestinationPort string `json:"destinationPort"`
	Host            string `json:"host"`
	DNSMode         string `json:"dnsMode"`
	Process         string `json:"process"`
	ProcessPath     string `json:"processPath"`
}

type Connection struct {
	ID          string             `json:"id"`
	Metadata    ConnectionMetadata `json:"metadata"`
	Upload      int64              `json:"upload"`
	Download    int64              `json:"download"`
	Start       time.Time          `json:"start"`
	Chains      []string           `json:"chains"`
	Rule        string             `json:"rule"`
	RulePayload string             `json:"rulePayload"`
}

// ConnectionSnapshot totals belong to the current core's in-memory counters.
// Closed connections remain included; restarting the core resets the totals.
type ConnectionSnapshot struct {
	UploadTotal   int64        `json:"uploadTotal"`
	DownloadTotal int64        `json:"downloadTotal"`
	Connections   []Connection `json:"connections"`
	Memory        uint64       `json:"memory"`
}

// ValidConnectionID accepts only one opaque connection identifier, never a
// path, query or the empty identifier used by the close-all route.
func ValidConnectionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func (c Client) Connections(ctx context.Context) (ConnectionSnapshot, error) {
	raw, err := c.request(ctx, http.MethodGet, "/connections", nil)
	if err != nil {
		return ConnectionSnapshot{}, err
	}
	return decodeConnections(raw)
}

func decodeConnections(raw []byte) (ConnectionSnapshot, error) {
	var value struct {
		UploadTotal   *int64          `json:"uploadTotal"`
		DownloadTotal *int64          `json:"downloadTotal"`
		Connections   json.RawMessage `json:"connections"`
		Memory        uint64          `json:"memory"`
	}
	invalid := diagnostic.New("diagnostic.api.connectionsResponse", "invalid Mihomo connections response")
	if json.Unmarshal(raw, &value) != nil || value.UploadTotal == nil || value.DownloadTotal == nil || *value.UploadTotal < 0 || *value.DownloadTotal < 0 || len(value.Connections) == 0 {
		return ConnectionSnapshot{}, invalid
	}
	var entries []struct {
		ID          string              `json:"id"`
		Metadata    *ConnectionMetadata `json:"metadata"`
		Upload      *int64              `json:"upload"`
		Download    *int64              `json:"download"`
		Start       time.Time           `json:"start"`
		Chains      []string            `json:"chains"`
		Rule        string              `json:"rule"`
		RulePayload string              `json:"rulePayload"`
	}
	if json.Unmarshal(value.Connections, &entries) != nil {
		return ConnectionSnapshot{}, invalid
	}
	result := ConnectionSnapshot{UploadTotal: *value.UploadTotal, DownloadTotal: *value.DownloadTotal, Connections: []Connection{}, Memory: value.Memory}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if !ValidConnectionID(entry.ID) || seen[entry.ID] || entry.Metadata == nil || entry.Upload == nil || entry.Download == nil || *entry.Upload < 0 || *entry.Download < 0 {
			return ConnectionSnapshot{}, invalid
		}
		for _, port := range []string{entry.Metadata.SourcePort, entry.Metadata.DestinationPort} {
			if port != "" {
				if _, err := strconv.ParseUint(port, 10, 16); err != nil {
					return ConnectionSnapshot{}, invalid
				}
			}
		}
		seen[entry.ID] = true
		result.Connections = append(result.Connections, Connection{ID: entry.ID, Metadata: *entry.Metadata, Upload: *entry.Upload, Download: *entry.Download, Start: entry.Start, Chains: entry.Chains, Rule: entry.Rule, RulePayload: entry.RulePayload})
	}
	return result, nil
}

// CloseConnection deliberately has no empty-ID or close-all variant.
func (c Client) CloseConnection(ctx context.Context, id string) error {
	if !ValidConnectionID(id) {
		return diagnostic.New("diagnostic.api.connectionID", "invalid connection ID; refresh the connections list")
	}
	_, err := c.request(ctx, http.MethodDelete, "/connections/"+url.PathEscape(id), nil)
	return err
}

const maxRuntimeSample = 64 << 10

// sample reads a bounded number of JSON frames then closes the HTTP stream.
// Mihomo uses newline-separated JSON over HTTP, including chunked responses.
func (c Client) sample(ctx context.Context, path string, frames int) (json.RawMessage, error) {
	body, err := c.open(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	decoder := json.NewDecoder(io.LimitReader(body, int64(frames*maxRuntimeSample+1)))
	var raw json.RawMessage
	for i := 0; i < frames; i++ {
		before := decoder.InputOffset()
		if err := decoder.Decode(&raw); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				return nil, diagnostic.New("diagnostic.api.sampleTimeout", "Mihomo API sample timed out")
			}
			return nil, diagnostic.New("diagnostic.runtime.response", "invalid or interrupted Mihomo %s response", path[1:])
		}
		if decoder.InputOffset()-before > maxRuntimeSample {
			return nil, diagnostic.New("diagnostic.api.sampleSize", "Mihomo API sample exceeds 64 KiB")
		}
	}
	return raw, nil
}

func (c Client) Traffic(ctx context.Context) (Traffic, error) {
	raw, err := c.sample(ctx, "/traffic", 1)
	if err != nil {
		return Traffic{}, err
	}
	return decodeTraffic(raw, false)
}

func decodeTraffic(raw []byte, requireTotals bool) (Traffic, error) {
	var value struct {
		Up        *int64 `json:"up"`
		Down      *int64 `json:"down"`
		UpTotal   *int64 `json:"upTotal"`
		DownTotal *int64 `json:"downTotal"`
	}
	invalid := diagnostic.New("diagnostic.api.trafficResponse", "invalid Mihomo traffic response")
	if json.Unmarshal(raw, &value) != nil || value.Up == nil || value.Down == nil || *value.Up < 0 || *value.Down < 0 || requireTotals && (value.UpTotal == nil || value.DownTotal == nil) {
		return Traffic{}, invalid
	}
	result := Traffic{Up: *value.Up, Down: *value.Down}
	if value.UpTotal != nil {
		if *value.UpTotal < 0 {
			return Traffic{}, invalid
		}
		result.UpTotal = *value.UpTotal
	}
	if value.DownTotal != nil {
		if *value.DownTotal < 0 {
			return Traffic{}, invalid
		}
		result.DownTotal = *value.DownTotal
	}
	return result, nil
}

func decodeMemory(raw []byte) (uint64, error) {
	var value struct {
		Inuse *uint64 `json:"inuse"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Inuse == nil {
		return 0, diagnostic.New("diagnostic.api.memoryResponse", "invalid Mihomo memory response")
	}
	return *value.Inuse, nil
}

func (c Client) Memory(ctx context.Context) (uint64, error) {
	// Mihomo emits a synthetic zero as the first /memory frame. A second frame
	// is required to report actual RSS rather than displaying that placeholder.
	raw, err := c.sample(ctx, "/memory", 2)
	if err != nil {
		return 0, err
	}
	return decodeMemory(raw)
}
