package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// TestRuleManagementPTYFixture is opt-in and uses no managed storage, core,
// network, or system services.
func TestRuleManagementPTYFixture(t *testing.T) {
	report := os.Getenv("LAZYCLASH_RULE_MANAGEMENT_PTY_REPORT")
	if report == "" {
		t.Skip("explicit rule management PTY report path required")
	}
	b := newRuleManagementPTYBackend(report)
	language := os.Getenv("LAZYCLASH_RULE_MANAGEMENT_PTY_LANGUAGE")
	if language == "" {
		language = "en"
	}
	if err := Run(context.Background(), b, language); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writeReport(); err != nil {
		t.Fatal(err)
	}
}

type ruleManagementPTYBackend struct {
	*fakeBackend
	mu        sync.Mutex
	report    string
	providers []mihomo.RuleProvider
	calls     ruleManagementPTYReport
	fileFails bool
}

type ruleManagementPTYReport struct {
	ProviderReads    int      `json:"provider_reads"`
	ProviderAttempts []string `json:"provider_attempts"`
	ProviderUpdates  []string `json:"provider_updates"`
	ProviderFailures []string `json:"provider_failures"`
	ServiceChanges   int      `json:"service_changes"`
	Modes            int      `json:"modes"`
	Selections       int      `json:"selections"`
}

func newRuleManagementPTYBackend(report string) *ruleManagementPTYBackend {
	b := &ruleManagementPTYBackend{
		fakeBackend: &fakeBackend{snapshot: app.Snapshot{
			Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
			Applied: "work", AppliedName: "PTY rule providers", Generation: "rule-management-pty",
			Service:      service.Status{Active: true, State: "active", PID: 4242},
			Mode:         app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
			Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
		}},
		report:    report,
		fileFails: true,
		providers: []mihomo.RuleProvider{
			{Name: "remote_rules", Type: "Rule", VehicleType: "HTTP", Behavior: "domain", Format: "yaml", RuleCount: 20, UpdatedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)},
			{Name: "local_rules", Type: "Rule", VehicleType: "File", Behavior: "classical", Format: "text", RuleCount: 12},
			{Name: "inline_rules", Type: "Rule", VehicleType: "Inline", Behavior: "domain", RuleCount: 4},
		},
	}
	for index := range 15 {
		b.providers = append(b.providers, mihomo.RuleProvider{Name: fmt.Sprintf("inline-extra-%02d", index), Type: "Rule", VehicleType: "Inline", Behavior: "domain", RuleCount: 100 + index})
	}
	return b
}

// Caller holds mu. Replacing the file lets the PTY inspect completed operations
// while the application is still running, without reading a partial report.
func (b *ruleManagementPTYBackend) writeReport() error {
	raw, err := json.Marshal(b.calls)
	if err != nil {
		return err
	}
	if err := os.WriteFile(b.report+".tmp", raw, 0600); err != nil {
		return err
	}
	return os.Rename(b.report+".tmp", b.report)
}

func (b *ruleManagementPTYBackend) Rules(ctx context.Context) (app.RuleSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	rules := []mihomo.Rule{{Index: 0, Type: "Domain", Payload: "source.example.invalid", Proxy: "PROXY", Size: -1}}
	for index := 1; index < 60; index++ {
		rules = append(rules, mihomo.Rule{Index: index, Type: "DomainSuffix", Payload: fmt.Sprintf("source-%02d.example.invalid", index), Proxy: "PROXY", Size: -1})
	}
	return app.RuleSnapshot{State: "ready", Rules: rules, ObservedAt: time.Now()}, ctx.Err()
}

func (b *ruleManagementPTYBackend) RuleProviders(ctx context.Context) (app.RuleProviderSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls.ProviderReads++
	return app.RuleProviderSnapshot{State: "ready", Providers: slices.Clone(b.providers), ObservedAt: time.Now()}, ctx.Err()
}

func (b *ruleManagementPTYBackend) UpdateRuleProvider(ctx context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	b.calls.ProviderAttempts = append(b.calls.ProviderAttempts, name)
	for i := range b.providers {
		if b.providers[i].Name != name {
			continue
		}
		if b.providers[i].VehicleType == "Inline" {
			return errors.New("fixture inline rules cannot update")
		}
		if b.providers[i].VehicleType == "File" && b.fileFails {
			b.fileFails = false
			b.calls.ProviderFailures = append(b.calls.ProviderFailures, name)
			if err := b.writeReport(); err != nil {
				return err
			}
			return errors.New("fixture local source read failed")
		}
		b.calls.ProviderUpdates = append(b.calls.ProviderUpdates, name)
		b.providers[i].UpdatedAt = time.Now()
		b.providers[i].RuleCount++
		return b.writeReport()
	}
	return fmt.Errorf("unknown fixture rule provider %q", name)
}

func (b *ruleManagementPTYBackend) Runtime(ctx context.Context) (app.RuntimeSnapshot, error) {
	return app.RuntimeSnapshot{State: "ready", Service: b.snapshot.Service, RunningVersion: b.snapshot.RunningVersion}, ctx.Err()
}

func (b *ruleManagementPTYBackend) Control(ctx context.Context, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls.ServiceChanges++
	return ctx.Err()
}

func (b *ruleManagementPTYBackend) SetMode(ctx context.Context, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls.Modes++
	return ctx.Err()
}

func (b *ruleManagementPTYBackend) Select(ctx context.Context, _, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls.Selections++
	return ctx.Err()
}

func (b *ruleManagementPTYBackend) StreamRuntime(ctx context.Context, emit func(app.RuntimeEvent)) error {
	return streamRuntimeFixture(ctx, b, emit)
}
