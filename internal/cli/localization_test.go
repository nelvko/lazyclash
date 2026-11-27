package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/buildinfo"
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
		printCommandError(root, err)
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
			t.Setenv("LAZYCLASH_HOME", home)
			t.Setenv("LAZYCLASH_SERVICE", "invalid/service")
			out, stderr, err := localizedCLI(t, "--lang", test.language, "--help")
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
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		t.Skip("the root system instance deliberately ignores the user data environment")
	}
	home := t.TempDir()
	t.Setenv("LAZYCLASH_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "preferences.json"), []byte(`{"Language":"zh-Hans"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, _, err := localizedCLI(t, "help", "install")
	if err != nil || !strings.Contains(out, "用法：") || !strings.Contains(out, "下载方式") {
		t.Fatalf("out=%s err=%v", out, err)
	}
	out, _, err = localizedCLI(t, "upgrade", "--help")
	if err != nil || !strings.Contains(out, "下载方式") || !strings.Contains(out, "加速模式") {
		t.Fatalf("upgrade help lost shared network translations: out=%s err=%v", out, err)
	}
	out, _, err = localizedCLI(t, "--lang", "en", "install", "--help")
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
		{[]string{"--lang", "zh-Hans", "install", "--bin"}, "需要参数值"},
		{[]string{"--lang", "zh-Hans", "status", "--json=bad-bool"}, "值无效"},
		{[]string{"--lang", "zh-Hans", "__service", "prepare", "extra"}, "需要 1 个参数"},
		{[]string{"--lang", "zh-Hans", "missing-command"}, "未知命令"},
	} {
		out, stderr, err := localizedCLI(t, test.args...)
		if err == nil || out != "" || !strings.Contains(stderr, test.want) {
			t.Fatalf("args=%q out=%q stderr=%q err=%v", test.args, out, stderr, err)
		}
	}
}

func TestCLIErrorOutputStripsTerminalFormattingAndPreservesLines(t *testing.T) {
	const label = "\x1b[32mexample\x1b[0m"
	for _, lang := range []string{"en", "zh-Hans"} {
		for _, args := range [][]string{{"help", label}, {"--" + label}} {
			out, stderr, err := localizedCLI(t, append([]string{"--lang", lang}, args...)...)
			if ExitCode(err) != 2 || out != "" || !strings.Contains(stderr, "example") || strings.Contains(stderr, "\x1b") || strings.Contains(stderr, "[32m") {
				t.Fatalf("language=%s args=%q stdout=%q stderr=%q err=%v", lang, args, out, stderr, err)
			}
		}
		cmd := New("--lang", lang)
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)
		printCommandError(cmd, errors.Join(errors.New(label), errors.New("second\r line")))
		if got := stderr.String(); got != "example\nsecond line\n" {
			t.Fatalf("language=%s diagnostics=%q", lang, got)
		}
	}
}

func TestCLIUnsettingRemainsIndependentOfLanguage(t *testing.T) {
	t.Setenv("LAZYCLASH_LANG", "")
	t.Setenv("LC_ALL", "C")
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "preferences.json"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LAZYCLASH_HOME", home)
	t.Setenv("LAZYCLASH_SERVICE", "invalid/service")
	for _, lang := range []string{"en", "zh-Hans"} {
		out, stderr, err := localizedCLI(t, "--lang", lang, "shell", "env", "--unset")
		if err != nil || stderr != "" || out != Unset+"\n" {
			t.Fatalf("lang=%s out=%q stderr=%q err=%v", lang, out, stderr, err)
		}
	}

}

func TestCLIBootstrapHonorsTerminatorAndValues(t *testing.T) {
	root := New()
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--lang=zh-CN"}, "zh-CN"},
		{[]string{"install", "--", "--lang", "zh-Hans"}, ""},
		{[]string{"upgrade", "--acceleration-prefix", "--lang", "en"}, ""},
		{[]string{"shell", "status", "--lang", "en"}, "en"},
	} {
		if got := bootstrapLanguage(root, test.args); got != test.want {
			t.Fatalf("args=%q lang=%q", test.args, got)
		}
	}
}

func TestCLIVersionCompletionAndManEntryRemainAvailable(t *testing.T) {
	t.Setenv("LAZYCLASH_LANG", "en")
	out, _, err := localizedCLI(t, "--version")
	if err != nil || strings.TrimSpace(out) != buildinfo.Version {
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
