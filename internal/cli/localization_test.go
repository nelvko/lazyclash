package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/i18n"
)

func localizedCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := New(args...)
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	err := root.ExecuteContext(context.Background())
	if err != nil {
		stderr.WriteString(invocationError(commandLocale(root), err))
	}
	return out.String(), stderr.String(), err
}

func TestCLIHelpUsesSelectedLanguageBeforeBackendInitialization(t *testing.T) {
	t.Setenv("LAZYCLASH_LANG", "")
	t.Setenv("LC_ALL", "C")
	for _, test := range []struct{ language, usage, description string }{
		{"en", "Usage:", "Manage Mihomo from a terminal UI"},
		{"zh-Hans", "用法：", "通过终端界面管理 Mihomo"},
	} {
		t.Run(test.language, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "not-created")
			out, stderr, err := localizedCLI(t, "--lang", test.language, "--home", home, "--service-name", "invalid/service", "--help")
			if err != nil || stderr != "" || !strings.Contains(out, test.usage) || !strings.Contains(out, test.description) || !strings.Contains(out, "--lang") {
				t.Fatalf("out=%s stderr=%s err=%v", out, stderr, err)
			}
			if _, err := os.Stat(home); !os.IsNotExist(err) {
				t.Fatal("help created a data directory")
			}
		})
	}
}

func TestCLIHelpRestoresSavedLanguageAndSupportsExplicitOverride(t *testing.T) {
	t.Setenv("LAZYCLASH_LANG", "")
	t.Setenv("LC_ALL", "C")
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "preferences.json"), []byte(`{"Language":"zh-Hans"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, _, err := localizedCLI(t, "--home", home, "help", "install")
	if err != nil || !strings.Contains(out, "用法：") || !strings.Contains(out, "发布文件来源") {
		t.Fatalf("out=%s err=%v", out, err)
	}
	for _, action := range []string{"check", "upgrade"} {
		out, _, err = localizedCLI(t, "--home", home, "install", action, "--help")
		if err != nil || !strings.Contains(out, "发布文件来源") || !strings.Contains(out, "下载连接方式") {
			t.Fatalf("%s help lost shared network translations: out=%s err=%v", action, out, err)
		}
	}
	out, _, err = localizedCLI(t, "--home", home, "--lang", "en", "install", "--help")
	if err != nil || !strings.Contains(out, "Usage:") || strings.Contains(out, "用法：") {
		t.Fatalf("out=%s err=%v", out, err)
	}
}

func TestCLIParseFailuresAreLocalizedAndDoNotWriteStdout(t *testing.T) {
	t.Setenv("LAZYCLASH_LANG", "")
	t.Setenv("LC_ALL", "C")
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--bad-option", "--lang", "zh-Hans"}, "未知选项"},
		{[]string{"--lang", "zh-Hans", "--home"}, "需要参数值"},
		{[]string{"--lang", "zh-Hans", "logs", "--lines", "bad-number"}, "值无效"},
		{[]string{"--lang", "zh-Hans", "select", "only-one"}, "需要 2 个参数"},
		{[]string{"--lang", "zh-Hans", "missing-command"}, "未知命令"},
	} {
		out, stderr, err := localizedCLI(t, test.args...)
		if err == nil || out != "" || !strings.Contains(stderr, test.want) {
			t.Fatalf("args=%q out=%q stderr=%q err=%v", test.args, out, stderr, err)
		}
	}
}

func TestCLIUnsettingAndJSONRemainIndependentOfLanguage(t *testing.T) {
	t.Setenv("LAZYCLASH_LANG", "")
	t.Setenv("LC_ALL", "C")
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "preferences.json"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"en", "zh-Hans"} {
		out, stderr, err := localizedCLI(t, "--lang", lang, "--home", home, "--service-name", "invalid/service", "env", "--unset")
		if err != nil || stderr != "" || out != Unset+"\n" {
			t.Fatalf("lang=%s out=%q stderr=%q err=%v", lang, out, stderr, err)
		}
	}
	var outputs []string
	for _, lang := range []string{"en", "zh-Hans"} {
		out, stderr, err := localizedCLI(t, "--lang", lang, "--home", t.TempDir(), "profile", "list")
		if err != nil || stderr != "" {
			t.Fatalf("lang=%s stderr=%q err=%v", lang, stderr, err)
		}
		var value any
		if err := json.Unmarshal([]byte(out), &value); err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, out)
	}
	if outputs[0] != outputs[1] {
		t.Fatal("JSON changed with interface language")
	}
}

func TestCLIBootstrapHonorsTerminatorAndValues(t *testing.T) {
	root := New()
	for _, test := range []struct {
		args       []string
		home, lang string
	}{
		{[]string{"--lang=zh-CN", "--home", "/tmp/example"}, "/tmp/example", "zh-CN"},
		{[]string{"profile", "add", "--", "--lang", "zh-Hans"}, "", ""},
		{[]string{"install", "--proxy-url", "--lang", "en"}, "", ""},
	} {
		home, lang := bootstrapPresentation(root, test.args)
		if home != test.home || lang != test.lang {
			t.Fatalf("args=%q home=%q lang=%q", test.args, home, lang)
		}
	}
}

func TestCLIVersionCompletionAndManEntryRemainAvailable(t *testing.T) {
	t.Setenv("LAZYCLASH_LANG", "en")
	out, _, err := localizedCLI(t, "--version")
	if err != nil || strings.TrimSpace(out) != cliVersion {
		t.Fatalf("version=%q err=%v", out, err)
	}
	out, _, err = localizedCLI(t, "completion", "bash")
	if err != nil || !strings.Contains(out, "__start_lazyclash") {
		t.Fatalf("completion missing: err=%v", err)
	}
	root := New()
	man, _, err := root.Find([]string{"man"})
	if err != nil || man.Name() != "man" || !man.Hidden {
		t.Fatal("hidden man command lost")
	}
	if commandLocale(root).Language() != i18n.English {
		t.Fatal("explicit environment language lost")
	}
}
