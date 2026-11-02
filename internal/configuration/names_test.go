package configuration

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestResolveNameUsesProviderPrecedenceAndURLFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name, explicit, address, title, disposition, want, source string
	}{
		{name: "explicit", explicit: "  用户\t名称  ", title: "服务端", disposition: `attachment; filename="ignored.yaml"`, address: "https://example.com/path.yml", want: "用户 名称", source: NameCustom},
		{name: "plain title", title: "机场 A · 香港 & 东京", disposition: `attachment; filename="ignored.yaml"`, want: "机场 A · 香港 & 东京", source: NameProvider},
		{name: "base64 title", title: "base64:" + base64.StdEncoding.EncodeToString([]byte("机场 B 🚀")), want: "机场 B 🚀", source: NameProvider},
		{name: "unpadded title", title: "base64:" + base64.RawStdEncoding.EncodeToString([]byte("机场 B 🚀")), want: "机场 B 🚀", source: NameProvider},
		{name: "invalid UTF8 title to filename", title: "\xff", disposition: `attachment; filename="Airport UTF8.yaml"`, want: "Airport UTF8.yaml", source: NameProvider},
		{name: "invalid decoded UTF8 title to filename", title: "base64:/w==", disposition: `attachment; filename="Airport Base64.yaml"`, want: "Airport Base64.yaml", source: NameProvider},
		{name: "extended filename first", disposition: `attachment; filename="ignored.yaml"; filename*=UTF-8'zh'%E6%9C%BA%E5%9C%BA%20C.yaml`, want: "机场 C.yaml", source: NameProvider},
		{name: "quoted extended filename", disposition: `attachment; filename*="UTF-8''%E6%9C%BA%E5%9C%BA%20C.yaml"`, want: "机场 C.yaml", source: NameProvider},
		{name: "quoted legacy filename", disposition: `attachment; filename="Airport D; special\"name.yaml"`, want: `Airport D; special"name.yaml`, source: NameProvider},
		{name: "unquoted unicode legacy", disposition: `attachment; filename=机场 D 配置.yaml`, want: "机场 D 配置.yaml", source: NameProvider},
		{name: "invalid title to filename", title: "base64:!invalid", disposition: `attachment; filename="Airport E.yaml"`, want: "Airport E.yaml", source: NameProvider},
		{name: "empty title to filename", title: " \t ", disposition: `attachment; filename="Airport F.yml"`, want: "Airport F.yml", source: NameProvider},
		{name: "invalid extended to legacy", disposition: `attachment; filename*=UTF-8''bad%ZZ; filename="Airport G.yaml"`, want: "Airport G.yaml", source: NameProvider},
		{name: "invalid extended encoding to legacy", disposition: `attachment; filename*=unknown''name; filename="Airport H.yaml"`, want: "Airport H.yaml", source: NameProvider},
		{name: "utf8 invalid extended to legacy", disposition: `attachment; filename*=UTF-8''%FF; filename="Airport I.yaml"`, want: "Airport I.yaml", source: NameProvider},
		{name: "extended Latin1", disposition: `attachment; filename*=ISO-8859-1''caf%E9.yaml`, want: "café.yaml", source: NameProvider},
		{name: "provider filename path is label only", disposition: `attachment; filename="../../private/机场.yaml"`, want: "机场.yaml", source: NameProvider},
		{name: "Windows filename path is label only", disposition: `attachment; filename="..\\..\\private\\机场.yaml"`, want: "机场.yaml", source: NameProvider},
		{name: "provider title controls cleaned", title: "\x1b[31m  机场\u202e\t A \x1b[0m", want: "机场 A", source: NameProvider},
		{name: "URL unicode path", address: "https://user:password@example.com/%E6%9C%BA%E5%9C%BA%20A.YAML?token=never-in-name#secret", want: "机场 A", source: NameURL},
		{name: "URL trailing slashes", address: "https://example.com/subscription///?token=never-in-name#secret", want: "subscription", source: NameURL},
		{name: "URL encoded slash belongs to last segment", address: "https://example.com/first/second%2Fname.yml", want: "second/name", source: NameURL},
		{name: "URL literal plus", address: "https://example.com/A+B.yaml", want: "A+B", source: NameURL},
		{name: "URL Host", address: "https://user:password@example.com:443/?token=never-in-name#secret", want: "example.com", source: NameURL},
		{name: "URL empty decoded segment", address: "https://example.com/%20%09/?token=never-in-name", want: "example.com", source: NameURL},
		{name: "invalid headers to URL", title: "base64:!", disposition: `attachment; filename*="UTF-8''bad%ZZ"`, address: "https://example.com/fallback.yml", want: "fallback", source: NameURL},
		{name: "empty headers to Host", title: " ", disposition: `attachment; filename=" "`, address: "https://example.com", want: "example.com", source: NameURL},
		{name: "fixed fallback", address: "", want: FallbackName, source: NameFallback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{"profile-title": []string{tc.title}, "Content-Disposition": []string{tc.disposition}}
			got := ResolveName(tc.explicit, headers, tc.address)
			if got.Name != tc.want || got.Source != tc.source {
				t.Fatalf("ResolveName = %#v, want %q (%s)", got, tc.want, tc.source)
			}
		})
	}
}

func TestProviderNameWithoutValidHeaderDoesNotResetToURL(t *testing.T) {
	for _, headers := range []http.Header{nil, {"Profile-Title": {"base64:invalid!"}}, {"Content-Disposition": {`attachment; filename*=UTF-8''bad%ZZ`}}, {"Profile-Title": {"\x00\u202e"}}} {
		if got := ProviderName(headers); got != "" {
			t.Fatalf("optional invalid header became a name: %q", got)
		}
	}
}

func TestResolveNameUsesLiteralLocalBasename(t *testing.T) {
	for _, tc := range []struct{ filename, want string }{
		{filename: "配置#1?.yaml", want: "配置#1?"},
		{filename: "100%.yaml", want: "100%"},
		{filename: "literal%20.yaml", want: "literal%20"},
		{filename: "中文 文件.YML", want: "中文 文件"},
		{filename: "Cafe\u0301#%?.yaml", want: "Café#%?"},
	} {
		t.Run(tc.filename, func(t *testing.T) {
			address := filepath.Join(t.TempDir(), "parent#with?delimiters%", tc.filename)
			got := ResolveName("", nil, address)
			if got.Name != tc.want || got.Source != NameURL {
				t.Fatalf("local name = %#v, want %q (%s)", got, tc.want, NameURL)
			}
		})
	}
	address := filepath.Join(t.TempDir(), "配置#1?.yaml")
	headers := http.Header{"Profile-Title": {"Provider name"}}
	for _, tc := range []struct{ explicit, want, source string }{
		{explicit: "", want: "Provider name", source: NameProvider},
		{explicit: "Custom name", want: "Custom name", source: NameCustom},
	} {
		got := ResolveName(tc.explicit, headers, address)
		if got.Name != tc.want || got.Source != tc.source {
			t.Fatalf("local naming precedence = %#v, want %q (%s)", got, tc.want, tc.source)
		}
	}
}

func TestNamesCleanControlsNormalizeUnicodeAndStayBounded(t *testing.T) {
	if got := CleanName("\x1b[31m  Cafe\u0301 \t  机场\u202e \x1b[0m"); got != "Café 机场" {
		t.Fatalf("unsafe or noncanonical name: %q", got)
	}
	name := CleanName(strings.Repeat("机场", 200))
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) != MaxNameRunes {
		t.Fatal("name was not limited at Unicode character boundaries")
	}
	if NameKey("  Café 机场  ") != NameKey("CAFE\u0301\t机场") || NameKey("STRASSE") != NameKey("Straße") {
		t.Fatal("name equality omitted casefold, whitespace or NFC")
	}
}

func TestUniqueNameKeepsIdentityAndAvoidsAccumulatingSuffixes(t *testing.T) {
	profiles := map[string]ProfileRecord{
		"stable-first": {Name: "机场 A", NameSource: NameProvider, NameBase: "机场 A"},
	}
	second := UniqueName("机场 A", profiles, "")
	if second != "机场 A (1)" {
		t.Fatal("first duplicate was not disambiguated", second)
	}
	profiles["stable-second"] = ProfileRecord{Name: second, NameSource: NameProvider, NameBase: "机场 A"}
	if third := UniqueName("机场 A", profiles, ""); third != "机场 A (2)" {
		t.Fatal("later duplicate was not disambiguated", third)
	}
	for range 3 {
		entry := profiles["stable-second"]
		entry.Name = UniqueName(entry.NameBase, profiles, "stable-second")
		profiles["stable-second"] = entry
		if entry.Name != "机场 A (1)" {
			t.Fatal("update accumulated duplicate suffixes", entry.Name)
		}
	}
	if got := UniqueName("DEFAULT", profiles, ""); got != "DEFAULT (1)" {
		t.Fatal("built-in profile was not included in uniqueness", got)
	}
	profiles["stable-first"] = ProfileRecord{Name: "Café Name", NameSource: NameCustom, NameBase: "Café Name"}
	if got := UniqueName("CAFE\u0301\tNAME", profiles, ""); got != "CAFÉ NAME (1)" {
		t.Fatal("visually ambiguous normalized duplicate was accepted", got)
	}
	base := strings.Repeat("长", MaxNameRunes)
	profiles["stable-first"] = ProfileRecord{Name: base, NameSource: NameCustom, NameBase: base}
	if got := UniqueName(base, profiles, ""); utf8.RuneCountInString(got) != MaxNameRunes || !strings.HasSuffix(got, " (1)") {
		t.Fatal("suffix exceeded the name length limit", got)
	}
}

func TestProfileMetadataRoundTripKeepsIDsBlobsAndCAS(t *testing.T) {
	r := Repository{Root: filepath.Join(t.TempDir(), "configuration")}
	revision, err := r.Put([]byte("rules: ['MATCH,DIRECT']\n"))
	if err != nil {
		t.Fatal(err)
	}
	lib := Library{Version: LibraryVersion,
		Profiles: map[string]ProfileRecord{
			"stable-id": {Spec: Profile{Core: "mihomo", Revision: revision}, Name: "机场 A (1)", NameSource: NameProvider, NameBase: "机场 A"},
		},
	}
	if err := r.Save(lib, nil); err != nil {
		t.Fatal(err)
	}
	reopened, expected, err := r.Load()
	if err != nil || reopened.Version != LibraryVersion || !reflect.DeepEqual(reopened.Profiles["stable-id"], lib.Profiles["stable-id"]) {
		t.Fatal("current metadata did not round-trip", err)
	}
	path := filepath.Join(r.Root, "library.json")
	onDisk, _ := os.ReadFile(path)
	if !bytes.Equal(onDisk, expected) {
		t.Fatal("loaded CAS bytes differ from the stored index")
	}
	entry := reopened.Profiles["stable-id"]
	entry.Name, entry.NameSource, entry.NameBase = "用户名称", NameCustom, "用户名称"
	reopened.Profiles["stable-id"] = entry
	if err := r.Save(reopened, expected); err != nil {
		t.Fatal(err)
	}
	published, _, err := r.Load()
	if err != nil || len(published.Profiles) != 1 || published.Profiles["stable-id"].Name != "用户名称" || published.Profiles["stable-id"].Spec.Revision != revision {
		t.Fatal("display metadata change affected stable IDs or recipe relationships", err)
	}
	if _, err := r.Read(revision); err != nil {
		t.Fatal("display metadata change moved immutable configuration files", err)
	}
	if err := r.Save(lib, expected); err == nil {
		t.Fatal("metadata save bypassed the compare-and-swap guard")
	}
}

func TestRepositoryRejectsUnsupportedSchemaAndMissingNameMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Library)
	}{
		{"outdated version", func(lib *Library) { lib.Version = LibraryVersion - 1 }},
		{"future version", func(lib *Library) { lib.Version = LibraryVersion + 1 }},
		{"missing display name", func(lib *Library) {
			entry := lib.Profiles["primary"]
			entry.Name = ""
			lib.Profiles["primary"] = entry
		}},
		{"missing name source", func(lib *Library) {
			entry := lib.Profiles["primary"]
			entry.NameSource = ""
			lib.Profiles["primary"] = entry
		}},
		{"missing candidate name", func(lib *Library) {
			entry := lib.Profiles["primary"]
			entry.NameBase = ""
			lib.Profiles["primary"] = entry
		}},
		{"missing all name metadata", func(lib *Library) {
			entry := lib.Profiles["primary"]
			entry.Name, entry.NameSource, entry.NameBase = "", "", ""
			lib.Profiles["primary"] = entry
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, lib, original, _ := publishedRepository(t)
			tc.edit(&lib)
			if err := r.Save(lib, original); err == nil {
				t.Fatal("unsupported schema or incomplete metadata was saved")
			}
			path := filepath.Join(r.Root, "library.json")
			onDisk, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(onDisk, original) {
				t.Fatal("rejected save changed the committed index", err)
			}
			invalid, err := json.Marshal(lib)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, invalid, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := r.Load(); err == nil {
				t.Fatal("unsupported schema or incomplete metadata was loaded")
			}
			onDisk, err = os.ReadFile(path)
			if err != nil || !bytes.Equal(onDisk, invalid) {
				t.Fatal("rejected load rewrote the index", err)
			}
		})
	}
}
