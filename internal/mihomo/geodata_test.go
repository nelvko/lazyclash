package mihomo

import (
	"maps"
	"testing"
)

func TestGeoSourcesRespectPartialOverridesAndCredentials(t *testing.T) {
	defaults, err := GeoSources([]byte("{}"))
	if err != nil || len(defaults) != 4 {
		t.Fatalf("default source identities: %v", err)
	}
	sources := func(raw string) map[string]string {
		t.Helper()
		result, err := GeoSources([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if !maps.Equal(defaults, sources("geox-url: {mmdb: "+DefaultGeoURLs()["mmdb"]+"}")) {
		t.Fatal("explicit official source should be reusable")
	}
	a := sources("geox-url: {geosite: 'https://example.invalid/private?key=a'}")
	b := sources("geox-url: {geosite: 'https://example.invalid/private?key=b'}")
	for kind, original := range defaults {
		if kind == "geosite" {
			if a[kind] == original || a[kind] == b[kind] || len(a[kind]) != 64 {
				t.Fatal("distinct private sources share identity")
			}
		} else if a[kind] != original || b[kind] != original {
			t.Fatalf("unrelated %s source changed", kind)
		}
	}
}
