package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

// TestRuleManagementPTYFixture is opt-in and uses no managed storage, core,
// network, or system services. The report distinguishes Save from Apply.
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
	draft     app.CustomRulesDraft
	applied   []string
	providers []mihomo.RuleProvider
	calls     ruleManagementPTYReport
	fileFails bool
}

type ruleManagementPTYReport struct {
	Loads            int      `json:"loads"`
	Saves            int      `json:"saves"`
	Applies          int      `json:"applies"`
	ProviderReads    int      `json:"provider_reads"`
	ProviderAttempts []string `json:"provider_attempts"`
	ProviderUpdates  []string `json:"provider_updates"`
	ProviderFailures []string `json:"provider_failures"`
	SavedRules       []string `json:"saved_rules"`
	AppliedRules     []string `json:"applied_rules"`
	ServiceChanges   int      `json:"service_changes"`
	Modes            int      `json:"modes"`
	Selections       int      `json:"selections"`
}

func newRuleManagementPTYBackend(report string) *ruleManagementPTYBackend {
	rules := []string{"DOMAIN,one.example.invalid,DIRECT", "DOMAIN-SUFFIX,two.example.invalid,PROXY"}
	return &ruleManagementPTYBackend{
		fakeBackend: &fakeBackend{snapshot: app.Snapshot{
			Installed: true, ConfigReady: true, HasRun: true, Version: "v1.19.33", RunningVersion: "v1.19.33",
			Applied: "work", AppliedName: "PTY custom rules", Generation: "rule-management-pty",
			Service:      service.Status{Active: true, State: "active", PID: 4242},
			Mode:         app.ModeStatus{Desired: "rule", Actual: "rule", Running: true},
			Capabilities: app.Capabilities{Scope: "user", Runtime: app.Capability{Supported: true}},
		}},
		report:    report,
		fileFails: true,
		draft: app.CustomRulesDraft{
			ProfileID: "work", ProfileName: "PTY custom rules", Rules: slices.Clone(rules),
			Token: "recipe-1", Applied: true, Validated: true,
		},
		applied: slices.Clone(rules),
		providers: []mihomo.RuleProvider{
			{Name: "remote_rules", Type: "Rule", VehicleType: "HTTP", Behavior: "domain", Format: "yaml", RuleCount: 20, UpdatedAt: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)},
			{Name: "local_rules", Type: "Rule", VehicleType: "File", Behavior: "classical", Format: "text", RuleCount: 12},
			{Name: "inline_rules", Type: "Rule", VehicleType: "Inline", Behavior: "domain", RuleCount: 4},
		},
	}
}

// Caller holds mu. Replacing the file lets the PTY inspect completed operations
// while the application is still running, without reading a partial report.
func (b *ruleManagementPTYBackend) writeReport() error {
	b.calls.SavedRules = slices.Clone(b.draft.Rules)
	b.calls.AppliedRules = slices.Clone(b.applied)
	raw, err := json.Marshal(b.calls)
	if err != nil {
		return err
	}
	if err := os.WriteFile(b.report+".tmp", raw, 0600); err != nil {
		return err
	}
	return os.Rename(b.report+".tmp", b.report)
}

func (b *ruleManagementPTYBackend) GetCustomRules(ctx context.Context, id string) (app.CustomRulesDraft, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return app.CustomRulesDraft{}, err
	}
	if id != b.draft.ProfileID {
		return app.CustomRulesDraft{}, fmt.Errorf("unexpected fixture profile %q", id)
	}
	b.calls.Loads++
	draft := b.draft
	draft.Rules = slices.Clone(draft.Rules)
	return draft, b.writeReport()
}

func (b *ruleManagementPTYBackend) SaveCustomRules(ctx context.Context, draft app.CustomRulesDraft) (app.CustomRulesDraft, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return app.CustomRulesDraft{}, err
	}
	if draft.ProfileID != b.draft.ProfileID || draft.Token != b.draft.Token {
		return app.CustomRulesDraft{}, errors.New("fixture draft changed")
	}
	b.calls.Saves++
	b.draft.Rules = slices.Clone(draft.Rules)
	b.draft.Token = fmt.Sprintf("saved-%d", b.calls.Saves)
	b.draft.Pending = true
	draft = b.draft
	draft.Rules = slices.Clone(draft.Rules)
	return draft, b.writeReport()
}

func (b *ruleManagementPTYBackend) ApplyCustomRules(ctx context.Context, draft app.CustomRulesDraft) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if draft.Token != b.draft.Token || !slices.Equal(draft.Rules, b.draft.Rules) {
		return errors.New("fixture saved recipe changed")
	}
	b.calls.Applies++
	b.applied = slices.Clone(b.draft.Rules)
	b.draft.Pending = false
	return b.writeReport()
}

func (b *ruleManagementPTYBackend) Rules(ctx context.Context) (app.RuleSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	rules := []mihomo.Rule{}
	for _, raw := range b.applied {
		parts := strings.Split(raw, ",")
		if len(parts) != 3 {
			return app.RuleSnapshot{}, fmt.Errorf("unexpected fixture rule %q", raw)
		}
		kind := "Domain"
		if parts[0] == "DOMAIN-SUFFIX" {
			kind = "DomainSuffix"
		}
		rules = append(rules, mihomo.Rule{Index: len(rules), Type: kind, Payload: parts[1], Proxy: parts[2], Size: -1})
	}
	rules = append(rules, mihomo.Rule{Index: len(rules), Type: "Domain", Payload: "source.example.invalid", Proxy: "PROXY", Size: -1})
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
