package configuration

import (
	"math"
	"net/http"
	"strconv"
	"strings"
)

// SubscriptionQuota summarizes the provider's latest usage report. TotalBytes
// is positive only when the limit and both usage counters were reported.
type SubscriptionQuota struct {
	UsedBytes  uint64 `json:"used_bytes,omitempty"`
	TotalBytes uint64 `json:"total_bytes,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"` // Unix seconds, zero when unknown.
}

// ParseSubscriptionQuota keeps traffic and expiry independent so a malformed
// optional field cannot prevent importing the profile or discard a valid date.
func ParseSubscriptionQuota(headers http.Header) SubscriptionQuota {
	fields := make(map[string]string, 4)
	for _, line := range headerValues(headers, "subscription-userinfo") {
		for part := range strings.SplitSeq(line, ";") {
			key, value, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			key = strings.ToLower(strings.TrimSpace(key))
			switch key {
			case "upload", "download", "total", "expire":
				value = strings.TrimSpace(value)
				if previous, seen := fields[key]; seen && previous != value {
					value = "" // Conflicting reports for the same counter.
				}
				fields[key] = value
			}
		}
	}
	var quota SubscriptionQuota
	upload, uploadErr := strconv.ParseUint(fields["upload"], 10, 64)
	download, downloadErr := strconv.ParseUint(fields["download"], 10, 64)
	total, totalErr := strconv.ParseUint(fields["total"], 10, 64)
	if uploadErr == nil && downloadErr == nil && totalErr == nil && total > 0 && upload <= math.MaxUint64-download {
		quota.UsedBytes, quota.TotalBytes = upload+download, total
	}
	// The UI displays a calendar date. Ignore millisecond timestamps and
	// out-of-range years beyond 9999-12-31 rather than showing a bogus date.
	if expire, err := strconv.ParseUint(fields["expire"], 10, 64); err == nil && expire <= 253402300799 {
		quota.ExpiresAt = int64(expire)
	}
	return quota
}
