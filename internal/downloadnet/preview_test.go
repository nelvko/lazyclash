package downloadnet

import (
	"strings"
	"testing"
)

func TestFilePreviewKeepsActualOfficialAndNestedPrefixAddresses(t *testing.T) {
	version := "v1.19.33"
	asset := "mihomo-linux-arm64-" + version + ".gz"
	original := "https://github.com/MetaCubeX/mihomo/releases/download/" + version + "/" + asset
	for _, tc := range []struct {
		cfg  Config
		want string
	}{
		{Config{}, original},
		{Config{Source: "prefix", Prefix: "https://accelerator.example.invalid/"}, "https://accelerator.example.invalid/" + original},
		{Config{Source: "prefix", Prefix: "https://accelerator.example.invalid/proxy/"}, "https://accelerator.example.invalid/proxy/" + original},
		{Config{Source: "mirror", Mirror: "https://mirror.example.invalid/releases/download/{version}/{asset}"}, "https://mirror.example.invalid/releases/download/" + version + "/" + asset},
	} {
		preview, err := tc.cfg.FilePreview(original, version, asset)
		if err != nil || preview != tc.want {
			t.Fatalf("preview=%q err=%v want=%q", preview, err, tc.want)
		}
		actual, err := tc.cfg.AssetURL(original, version, asset)
		if err != nil || actual != tc.want {
			t.Fatalf("preview changed download semantics: %q %v", actual, err)
		}
	}
}

func TestFilePreviewMasksStaticTokensUserinfoAndQueryValues(t *testing.T) {
	version := "v1.19.33"
	asset := "mihomo-linux-arm64-" + version + ".gz"
	original := "https://github.com/MetaCubeX/mihomo/releases/download/" + version + "/" + asset
	cases := []struct {
		cfg            Config
		original, want string
	}{
		{Config{Source: "prefix", Prefix: "https://private-user:private-pass@accelerator.example.invalid/private-path/proxy/"}, original, "https://accelerator.example.invalid/[redacted]/proxy/" + original},
		{Config{Source: "mirror", Mirror: "https://private-user:private-pass@mirror.example.invalid/private-path/{version}/{asset}?token=private-query"}, original, "https://mirror.example.invalid/[redacted]/" + version + "/" + asset + "?[redacted]"},
		{Config{}, original + "?token=private-query", original + "?[redacted]"},
		{Config{Source: "prefix", Prefix: "https://accelerator.example.invalid/"}, "https://private-user:private-pass@files.example.invalid/private-path?token=private-query", "https://accelerator.example.invalid/https://files.example.invalid/[redacted]?[redacted]"},
	}
	for _, tc := range cases {
		preview, err := tc.cfg.FilePreview(tc.original, version, asset)
		if err != nil || preview != tc.want {
			t.Fatalf("preview=%q err=%v want=%q", preview, err, tc.want)
		}
		if strings.Contains(preview, "private-") {
			t.Fatal("preview leaked a credential or private path")
		}
		actual, err := tc.cfg.AssetURL(tc.original, version, asset)
		if err != nil || !strings.Contains(actual, "private-") {
			t.Fatal("display masking altered the actual request URL", actual, err)
		}
	}
}

func TestFilePreviewMasksEncodedAndUnrecognizedPathsWithoutJoining(t *testing.T) {
	cfg := Config{Source: "mirror", Mirror: "https://mirror.example.invalid/%70rivate-token/releases/{version}/{asset}"}
	preview, err := cfg.FilePreview("https://github.com", "v1.2.3", "mihomo-linux-arm64-v1.2.3.gz")
	if err != nil || strings.Contains(preview, "private") || strings.Contains(preview, "%70") || !strings.Contains(preview, "/[redacted]/releases/v1.2.3/") {
		t.Fatalf("encoded token was not masked: %q %v", preview, err)
	}
	cfg = Config{Source: "prefix", Prefix: "https://accelerator.example.invalid/cache///", ProxyMode: "custom", ProxyURL: ""}
	original := "https://github.com/MetaCubeX/mihomo/releases/download/v1.2.3/mihomo-linux-arm64-v1.2.3.gz"
	preview, err = cfg.FilePreview(original, "v1.2.3", "mihomo-linux-arm64-v1.2.3.gz")
	if err != nil || preview != "https://accelerator.example.invalid/cache/"+original {
		t.Fatalf("invalid connection draft prevented independent source preview: %q %v", preview, err)
	}
	if _, err := (Config{Source: "prefix", Prefix: "invalid-private-token"}).FilePreview(original, "v1.2.3", "mihomo.gz"); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("unsafe invalid preview error: %v", err)
	}
}
