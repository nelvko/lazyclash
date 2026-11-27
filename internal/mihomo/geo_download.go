package mihomo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/downloadnet"
	"github.com/oschwald/maxminddb-golang"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/encoding/protowire"
)

const maxDefaultGeoSize = 64 << 20

// DownloadDefaultGeo verifies an official asset in a private staging directory.
// Publishing these bytes in a service-writable runtime is the caller's transaction.
func DownloadDefaultGeo(ctx context.Context, kind, dest string, cfg downloadnet.Config, progress func(Progress)) error {
	if kind != "mmdb" && kind != "geosite" {
		return diagnostic.New("diagnostic.geo.seedKind", "unsupported default GEO asset")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	client, err := downloadnet.NewClient(cfg, 5*time.Minute)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	official := DefaultGeoURLs()[kind]
	asset := path.Base(official)
	label := asset
	if kind == "geosite" {
		label = "GeoSite.dat"
	}
	report := func(p Progress) {
		if progress != nil {
			progress(p)
		}
	}
	report(Progress{Stage: "geodata", Detail: label})
	sumResponse, err := fetchGeo(ctx, client, cfg, official+".sha256sum")
	if err != nil {
		return err
	}
	const maxChecksum = 512
	sum, err := io.ReadAll(io.LimitReader(sumResponse.Body, maxChecksum+1))
	sumResponse.Body.Close()
	if err != nil {
		return downloadnet.Failure(ctx, "GEO data download", err)
	}
	fields := strings.Fields(string(sum))
	if len(sum) > maxChecksum || len(fields) != 2 || fields[1] != asset {
		return diagnostic.New("diagnostic.geo.seedChecksum", "invalid official GEO checksum file")
	}
	want, err := hex.DecodeString(fields[0])
	if err != nil || len(want) != sha256.Size {
		return diagnostic.New("diagnostic.geo.seedChecksum", "invalid official GEO checksum file")
	}

	resp, err := fetchGeo(ctx, client, cfg, official)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxDefaultGeoSize {
		return diagnostic.New("diagnostic.geo.seedSize", "default GEO file exceeds 64 MiB")
	}
	file, err := os.CreateTemp(filepath.Dir(dest), ".geo-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	h := sha256.New()
	start := time.Now()
	total := max(resp.ContentLength, 0)
	writer := &downloadWriter{writer: io.MultiWriter(file, h), report: report, stage: "geodata", detail: label, total: total, started: start, last: start}
	n, err := io.Copy(writer, io.LimitReader(resp.Body, maxDefaultGeoSize+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return downloadnet.Failure(ctx, "GEO data download", err)
	}
	if n > maxDefaultGeoSize {
		return diagnostic.New("diagnostic.geo.seedSize", "default GEO file exceeds 64 MiB")
	}
	if !bytes.Equal(want, h.Sum(nil)) {
		return diagnostic.New("diagnostic.geo.seedMismatch", "downloaded GEO data differs from the official checksum")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ValidateDefaultGeo(ctx, kind, file.Name()); err != nil {
		return err
	}
	// No replace: a concurrent writer may have filled this destination.
	if err := os.Link(file.Name(), dest); err != nil {
		return err
	}
	report(Progress{Stage: "geodata", Detail: label, Downloaded: n, Total: total, BytesPerSecond: float64(n) / time.Since(start).Seconds()})
	return nil
}

func fetchGeo(ctx context.Context, client *http.Client, cfg downloadnet.Config, official string) (*http.Response, error) {
	source, err := cfg.URL(official)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, diagnostic.New("diagnostic.geo.seedRequest", "cannot construct GEO asset request")
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return nil, downloadnet.Failure(ctx, "GEO data download", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, downloadnet.HTTPError("GEO data download", resp.StatusCode)
	}
	return resp, nil
}

// ValidateDefaultGeo does not reach the network. Validate a bounded, stable
// copy of the file, not a memory mapping that the running core could truncate.
func ValidateDefaultGeo(ctx context.Context, kind, filename string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if kind != "mmdb" && kind != "geosite" {
		return diagnostic.New("diagnostic.geo.seedKind", "unsupported default GEO asset")
	}
	f, err := os.OpenFile(filename, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 {
		return diagnostic.New("diagnostic.geo.file", "GEO assets must be regular files without links")
	}
	if info.Size() > maxDefaultGeoSize {
		return diagnostic.New("diagnostic.geo.seedSize", "default GEO file exceeds 64 MiB")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxDefaultGeoSize+1))
	if err != nil {
		return err
	}
	if len(raw) > maxDefaultGeoSize {
		return diagnostic.New("diagnostic.geo.seedSize", "default GEO file exceeds 64 MiB")
	}
	if kind == "mmdb" {
		db, err := maxminddb.FromBytes(raw)
		if err != nil {
			return diagnostic.New("diagnostic.geo.seedCountry", "invalid default GEO country database")
		}
		defer db.Close()
		// The official Meta-geoip0 has optional empty description metadata,
		// which maxminddb.Verify rejects. Read a country record as Mihomo does.
		networks := db.Networks()
		if !networks.Next() {
			return diagnostic.New("diagnostic.geo.seedCountry", "invalid default GEO country database")
		}
		var record any
		if _, err := networks.Network(&record); err != nil || !isCountryRecord(record) {
			return diagnostic.New("diagnostic.geo.seedCountry", "invalid default GEO country database")
		}
	} else if !validDefaultGeoSite(ctx, raw) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return diagnostic.New("diagnostic.geo.seedSite", "invalid default GeoSite database or missing CN domains")
	}
	return ctx.Err()
}

func isCountryRecord(record any) bool {
	switch value := record.(type) {
	case string: // Meta-geoip0 or sing-geoip
		return value != ""
	case []any: // Meta-geoip0 can associate an IP with several codes.
		for _, code := range value {
			if str, ok := code.(string); !ok || str == "" {
				return false
			}
		}
		return len(value) > 0
	case map[string]any: // MaxMind country database, including Country.mmdb.
		country, _ := value["country"].(map[string]any)
		code, _ := country["iso_code"].(string)
		return code != ""
	default:
		return false
	}
}

// GeoSiteList.entry = 1, GeoSite.country_code = 1, GeoSite.domain = 2,
// Domain.value = 2. GeoIP.dat shares the outer layout but its nested CIDR
// records have a varint prefix instead of a string Domain.value.
func validDefaultGeoSite(ctx context.Context, raw []byte) bool {
	found := false
	valid := walkGeoBytes(raw, func(tag protowire.Number, entry []byte) bool {
		if ctx.Err() != nil {
			return false
		}
		if tag != 1 {
			return true
		}
		var cn, hasDomain bool
		valid := walkGeoBytes(entry, func(tag protowire.Number, value []byte) bool {
			switch tag {
			case 1:
				cn = bytes.EqualFold(value, []byte("cn"))
			case 2:
				return walkGeoBytes(value, func(tag protowire.Number, domain []byte) bool {
					if tag == 2 && len(domain) != 0 && utf8.Valid(domain) {
						hasDomain = true
					}
					return true
				})
			}
			return true
		})
		found = found || cn && hasDomain
		return valid
	})
	return valid && found
}

// Visit length-delimited fields and verify every protobuf field boundary.
func walkGeoBytes(raw []byte, visit func(protowire.Number, []byte) bool) bool {
	for len(raw) > 0 {
		tag, typ, n := protowire.ConsumeTag(raw)
		if n < 0 {
			return false
		}
		raw = raw[n:]
		if typ == protowire.BytesType {
			var value []byte
			value, n = protowire.ConsumeBytes(raw)
			if n < 0 || !visit(tag, value) {
				return false
			}
		} else {
			n = protowire.ConsumeFieldValue(tag, typ, raw)
			if n < 0 {
				return false
			}
		}
		raw = raw[n:]
	}
	return true
}
