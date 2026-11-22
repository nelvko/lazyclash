package configuration

import (
	"math"
	"net/http"
	"testing"
)

func TestParseSubscriptionQuota(t *testing.T) {
	for _, tt := range []struct {
		name   string
		values []string
		want   SubscriptionQuota
	}{
		{"case and spacing", []string{" EXPIRE = 2000000000; ToTaL=100;download=3 ; unused=yes; UpLoad=5"}, SubscriptionQuota{8, 100, 2000000000}},
		{"zero usage", []string{"upload=0;download=0;total=100;expire=0"}, SubscriptionQuota{0, 100, 0}},
		{"expiry only", []string{"expire=2000000000"}, SubscriptionQuota{ExpiresAt: 2000000000}},
		{"missing usage", []string{"download=1;total=100;expire=2000000000"}, SubscriptionQuota{ExpiresAt: 2000000000}},
		{"zero total", []string{"upload=1;download=2;total=0;expire=2000000000"}, SubscriptionQuota{ExpiresAt: 2000000000}},
		{"no total", []string{"download=50;upload=10"}, SubscriptionQuota{}},
		{"negative", []string{"download=-5;upload=0;total=60;expire=-1"}, SubscriptionQuota{}},
		{"malformed", []string{"garbage;upload=1KB;download=1;total=4"}, SubscriptionQuota{}},
		{"overflow", []string{"upload=18446744073709551616;download=0;total=5"}, SubscriptionQuota{}},
		{"sum overflow", []string{"upload=18446744073709551615;download=1;total=18446744073709551615;expire=123"}, SubscriptionQuota{ExpiresAt: 123}},
		{"uint64 bound", []string{"upload=0;download=18446744073709551615;total=18446744073709551615"}, SubscriptionQuota{math.MaxUint64, math.MaxUint64, 0}},
		{"over allowance", []string{"upload=0;download=101;total=100"}, SubscriptionQuota{101, 100, 0}},
		{"invalid timestamp", []string{"expire=2000000000000;total=5;download=1;upload=2"}, SubscriptionQuota{3, 5, 0}},
		{"split header", []string{"upload=2;download=3", "total=100"}, SubscriptionQuota{5, 100, 0}},
		{"conflicting counter", []string{"upload=1;upload=2;download=1;total=10;expire=123"}, SubscriptionQuota{ExpiresAt: 123}},
		{"absent", nil, SubscriptionQuota{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSubscriptionQuota(http.Header{"sUbScRiPtIoN-uSeRiNfO": tt.values})
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
