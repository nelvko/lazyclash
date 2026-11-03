package downloadnet

import (
	"strings"
	"testing"
)

func TestPreviewKeepsActualGitHubVersionAndFileAddresses(t *testing.T) {
	version := "v1.19.33"
	asset := "mihomo-linux-arm64-" + version + ".gz"
	for _, original := range []string{
		"https://github.com/MetaCubeX/mihomo/releases/latest/download/version.txt",
		"https://github.com/MetaCubeX/mihomo/releases/download/Prerelease-Alpha/version.txt",
		"https://github.com/MetaCubeX/mihomo/releases/download/" + version + "/" + asset,
	} {
		for _, cfg := range []Config{{}, {Method: MethodEnvironment}, {Method: MethodAccelerated, Prefix: "https://accelerator.example.invalid/proxy///"}} {
			want := original
			if cfg.Method == MethodAccelerated {
				want = "https://accelerator.example.invalid/proxy/" + original
			}
			preview, err := cfg.Preview(original)
			if err != nil || preview != want {
				t.Fatalf("preview=%q err=%v want=%q", preview, err, want)
			}
			actual, err := cfg.URL(original)
			if err != nil || actual != want {
				t.Fatalf("preview changed request semantics: %q %v", actual, err)
			}
		}
	}
}

func TestFilePreviewMasksPrivatePathsCredentialsAndQueries(t *testing.T) {
	version := "v1.19.33"
	asset := "mihomo-linux-arm64-" + version + ".gz"
	original := "https://github.com/MetaCubeX/mihomo/releases/download/" + version + "/" + asset
	for _, tc := range []struct {
		cfg            Config
		original, want string
	}{
		{Config{Method: MethodAccelerated, Prefix: "https://accelerator.example.invalid/private-path/proxy/"}, original, "https://accelerator.example.invalid/[redacted]/proxy/" + original},
		{Config{Method: MethodAccelerated, Prefix: "https://accelerator.example.invalid/%70rivate-token/"}, original, "https://accelerator.example.invalid/[redacted]/" + original},
		{Config{}, original + "?token=private-query", original + "?[redacted]"},
		{Config{Method: MethodAccelerated, Prefix: "https://accelerator.example.invalid/"}, "https://private-user:private-pass@files.example.invalid/private-path?token=private-query", "https://accelerator.example.invalid/https://files.example.invalid/[redacted]?[redacted]"},
	} {
		preview, err := tc.cfg.FilePreview(tc.original, version, asset)
		if err != nil || preview != tc.want {
			t.Fatalf("preview=%q err=%v want=%q", preview, err, tc.want)
		}
		if strings.Contains(preview, "private-") || strings.Contains(preview, "%70") {
			t.Fatal("preview leaked a credential or private path")
		}
		actual, err := tc.cfg.URL(tc.original)
		if err != nil || !strings.Contains(actual, "private-") && !strings.Contains(actual, "%70") {
			t.Fatal("display masking altered the actual request URL", actual, err)
		}
	}
	if _, err := (Config{Method: MethodAccelerated, Prefix: "invalid-private-token"}).Preview(original); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("unsafe invalid preview error: %v", err)
	}
}
