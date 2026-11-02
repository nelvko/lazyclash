package mihomo

import "testing"

func TestCompareVersions(t *testing.T) {
	for _, test := range []struct {
		name, installed, latest string
		channel                 Channel
		want                    UpdateStatus
	}{
		{"equal", "v1.19.33", "v1.19.33", ChannelStable, UpdateCurrent},
		{"optional-v", "1.19.33", "v1.19.33", ChannelStable, UpdateCurrent},
		{"newer", "v1.9.33", "v1.19.0", ChannelStable, UpdateAvailable},
		{"ahead", "v2.0.0", "v1.99.99", ChannelStable, UpdateAhead},
		{"large-number", "v999999999999999999999.1.1", "v1000000000000000000000.0.0", ChannelStable, UpdateAvailable},
		{"build-ignored", "v1.2.3+local.01", "v1.2.3+official", ChannelStable, UpdateCurrent},
		{"prerelease-before-stable", "v1.2.3-rc.1", "v1.2.3", ChannelStable, UpdateAvailable},
		{"stable-after-prerelease", "v1.2.3", "v1.2.3-rc.1", ChannelStable, UpdateAhead},
		{"numeric-prerelease", "v1.2.3-rc.2", "v1.2.3-rc.10", ChannelStable, UpdateAvailable},
		{"numeric-before-alpha", "v1.2.3-2", "v1.2.3-beta", ChannelStable, UpdateAvailable},
		{"longer-prerelease", "v1.2.3-rc", "v1.2.3-rc.1", ChannelStable, UpdateAvailable},
		{"alpha-equal", "alpha-69c1631", "alpha-69c1631", ChannelAlpha, UpdateCurrent},
		{"alpha-different", "alpha-fffffff", "alpha-0000000", ChannelAlpha, UpdateIncomparable},
		{"different-channel", "alpha-69c1631", "v1.19.33", ChannelStable, UpdateIncomparable},
		{"unknown-future", "v1.2.3.4", "v1.2.3.5", ChannelStable, UpdateIncomparable},
		{"unknown-equal", "v1.2", "v1.2", ChannelStable, UpdateIncomparable},
		{"unknown-channel", "v1.2.3", "v1.2.4", ChannelUnknown, UpdateIncomparable},
		{"leading-zero", "v01.2.3", "v1.2.4", ChannelStable, UpdateIncomparable},
		{"prerelease-leading-zero", "v1.2.3-01", "v1.2.3", ChannelStable, UpdateIncomparable},
		{"invalid-build", "v1.2.3+a+b", "v1.2.3", ChannelStable, UpdateIncomparable},
		{"absent", "", "v1.19.33", ChannelStable, UpdateNotInstalled},
		{"invalid-remote", "v1.2.3", "<html>500</html>", ChannelStable, UpdateIncomparable},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := CompareVersions(test.installed, test.latest, test.channel); got != test.want {
				t.Fatalf("CompareVersions(%q, %q, %q) = %q, want %q", test.installed, test.latest, test.channel, got, test.want)
			}
		})
	}
}

func TestChannelForVersion(t *testing.T) {
	for version, want := range map[string]Channel{
		"v1.19.33": ChannelStable, "1.19.33": ChannelStable,
		"v1.20.0-rc.1+custom": ChannelStable,
		"alpha-69c1631":       ChannelAlpha, "alpha-deadbeef": ChannelAlpha,
		"alpha-future-format": ChannelUnknown, "alpha-short": ChannelUnknown,
		"": ChannelUnknown, "v2.0": ChannelUnknown, "v01.2.3": ChannelUnknown,
		"<html>500</html>": ChannelUnknown,
	} {
		if got := ChannelForVersion(version); got != want {
			t.Errorf("ChannelForVersion(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestBinaryVersionSupportsUpstreamAndUnknownVersionTokens(t *testing.T) {
	for _, version := range []string{"v1.19.33", "1.19.33", "alpha-69c1631", "v2.0", "v1.20.0-rc.1+custom"} {
		for _, identity := range []string{"Mihomo Meta ", "Mihomo "} {
			got, err := BinaryVersion(t.Context(), script(t, "echo '"+identity+version+" linux arm64 with go1.26.0'\n"))
			if err != nil || got != version {
				t.Errorf("version=%q identity=%q: got %q, %v", version, identity, got, err)
			}
		}
	}
	for _, output := range []string{"other Meta v1.2.3", "Mihomo Meta NotFound", "Mihomo Meta <html>500</html>", "Mihomo Meta v1.2.3/private-token"} {
		if _, err := BinaryVersion(t.Context(), script(t, "echo '"+output+"'\n")); err == nil {
			t.Errorf("accepted invalid identity %q", output)
		}
	}
}
