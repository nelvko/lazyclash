package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// TestRulesPTYFixture runs the real TUI against an opt-in, in-memory backend.
// It does not read managed storage, contact a core or change system services.
func TestRulesPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_RULES_PTY_REPORT")
	if report == "" {
		t.Skip("explicit Rules PTY report path required")
	}
	count := 60
	if value := os.Getenv("LAZYCLASH_RULES_PTY_COUNT"); value != "" {
		var err error
		count, err = strconv.Atoi(value)
		if err != nil || count < 20 {
			t.Fatal("LAZYCLASH_RULES_PTY_COUNT must be an integer of at least 20")
		}
	}
	b := newRulesPTYBackend(count)
	language := os.Getenv("LAZYCLASH_RULES_PTY_LANGUAGE")
	if language == "" {
		language = "en"
	}
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]int64{
		"queries": b.queries.Load(), "service_changes": b.controls.Load(),
		"modes": b.modes.Load(), "selections": b.selections.Load(),
		"rules": int64(len(b.entries)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

type rulesPTYBackend struct {
	*fakeBackend
	entries                              []mihomo.Rule
	queries, controls, modes, selections atomic.Int64
}

func newRulesPTYBackend(count int) *rulesPTYBackend {
	var entries []mihomo.Rule
	add := func(kind, payload, proxy string, size int) {
		entries = append(entries, mihomo.Rule{Index: len(entries), Type: kind, Payload: payload, Proxy: proxy, Size: size})
	}
	add("RuleSet", "private_ip", "全球直连", 578)
	add("RuleSet", "private_domain", "全球直连", 152)
	add("DomainSuffix", "jumpdesktop.com", "Jump Desktop", -1)
	add("DomainSuffix", "qichiyu.com", "默认代理", -1)
	for _, item := range [][2]string{
		{"proxylite", "默认代理"}, {"apple_domain", "Apple"}, {"github_domain", "GitHub"},
		{"youtube_domain", "YouTube"}, {"google_domain", "Google"}, {"ai", "ChatGPT"},
		{"onedrive_domain", "OneDrive"}, {"microsoft_domain", "Microsoft"},
		{"tiktok_domain", "TikTok"}, {"speedtest_domain", "Speedtest"}, {"telegram_domain", "Telegram"},
	} {
		add("RuleSet", item[0], item[1], 2048)
	}
	add("DomainSuffix", "中文规则.example.invalid", "中文策略组", -1)
	add("Domain", "CaseSensitive.example.invalid", "PROXY", -1)
	add("Domain", "casesensitive.example.invalid", "PROXY", -1)
	add("DomainSuffix", strings.Repeat("long-中文/", 18)+"example.invalid", "Long target 中文策略组", -1)
	entries[len(entries)-1].Extra = &mihomo.RuleExtra{Disabled: true, HitCount: 4, MissCount: 9}
	padding := count - len(entries) - 1
	for i := 0; i < padding; i++ {
		add("DomainSuffix", fmt.Sprintf("rule-%02d.example.invalid", i), "默认代理", -1)
	}
	add("Match", "", "DIRECT", -1)
	return &rulesPTYBackend{
		fakeBackend: &fakeBackend{snapshot: app.Snapshot{
			Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
			Applied: "rules-pty", AppliedName: "Rules preview", Generation: "rules-pty-generation",
			Service:      service.Status{Active: true, State: "active", PID: 4242},
			Mode:         app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
			Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
		}},
		entries: entries,
	}
}

func (b *rulesPTYBackend) Rules(ctx context.Context) (app.RuleSnapshot, error) {
	b.queries.Add(1)
	return app.RuleSnapshot{State: "ready", Rules: append([]mihomo.Rule(nil), b.entries...), ObservedAt: time.Now()}, ctx.Err()
}

func (b *rulesPTYBackend) Runtime(ctx context.Context) (app.RuntimeSnapshot, error) {
	return app.RuntimeSnapshot{State: "ready", Service: b.snapshot.Service, RunningVersion: b.snapshot.RunningVersion}, ctx.Err()
}

func (b *rulesPTYBackend) Control(ctx context.Context, _ string) error {
	b.controls.Add(1)
	return ctx.Err()
}

func (b *rulesPTYBackend) SetMode(ctx context.Context, _ string) error {
	b.modes.Add(1)
	return ctx.Err()
}

func (b *rulesPTYBackend) Select(ctx context.Context, _, _ string) error {
	b.selections.Add(1)
	return ctx.Err()
}
