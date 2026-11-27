package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type IPInfo struct {
	IP, Country, Region, City string
	ISP, ASN                  string
}

const ipInfoEndpoint = "https://ipwho.is/?fields=success,ip,country,region,city,connection.isp,connection.asn"

// LookupIPInfo observes the exit used by the current Mihomo rules for ipwho.is.
func (a *App) LookupIPInfo(ctx context.Context) (IPInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client, err := a.runtimeProxyClient(ctx)
	if err != nil {
		return IPInfo{}, err
	}
	defer client.CloseIdleConnections()
	return fetchIPInfo(ctx, client)
}

func fetchIPInfo(ctx context.Context, client *http.Client) (IPInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipInfoEndpoint, nil)
	if err != nil {
		return IPInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return IPInfo{}, ctx.Err()
		}
		return IPInfo{}, errors.New("IP lookup through Mihomo failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return IPInfo{}, errors.New("IP lookup service unavailable")
	}
	const limit = 16 << 10
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if ctx.Err() != nil {
		return IPInfo{}, ctx.Err()
	}
	var result struct {
		Success                   bool
		IP, Country, Region, City string
		Connection                struct {
			ASN uint32
			ISP string
		}
	}
	if err != nil || len(raw) > limit || json.Unmarshal(raw, &result) != nil || !result.Success {
		return IPInfo{}, errors.New("invalid IP lookup response")
	}
	ip, err := netip.ParseAddr(result.IP)
	if err != nil || ip.Zone() != "" {
		return IPInfo{}, errors.New("invalid IP lookup address")
	}
	field := func(s string) string {
		if strings.IndexFunc(s, unicode.IsControl) >= 0 {
			return ""
		}
		return strings.TrimSpace(s)
	}
	info := IPInfo{IP: ip.String(), Country: field(result.Country), Region: field(result.Region), City: field(result.City),
		ISP: field(result.Connection.ISP)}
	if result.Connection.ASN > 0 {
		info.ASN = "AS" + strconv.FormatUint(uint64(result.Connection.ASN), 10)
	}
	return info, nil
}
