package app

import (
	"encoding/json"
	"testing"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

func TestPresentationDiagnosticsDoNotChangeMachineOutput(t *testing.T) {
	err := diagnostic.New("diagnostic.api.http", "Mihomo API returned HTTP %d", 503)
	snapshot := Snapshot{ServiceError: err.Error(), APIError: err.Error(), Mode: ModeStatus{Error: err.Error()}}
	withDiagnostics := snapshot
	withDiagnostics.ServiceDiagnostic, withDiagnostics.APIDiagnostic, withDiagnostics.Mode.ErrorDiagnostic = err, err, err
	runtime := RuntimeSnapshot{Error: err.Error(), TrafficError: err.Error(), ConnectionsError: err.Error(), MemoryError: err.Error()}
	withRuntimeDiagnostics := runtime
	withRuntimeDiagnostics.Diagnostic, withRuntimeDiagnostics.TrafficDiagnostic = err, err
	withRuntimeDiagnostics.ConnectionsDiagnostic, withRuntimeDiagnostics.MemoryDiagnostic = err, err
	rules := RuleSnapshot{Error: err.Error()}
	withRulesDiagnostics := rules
	withRulesDiagnostics.Diagnostic = err
	plan := InstallPlan{BlockedReason: err.Error()}
	withPlanDiagnostic := plan
	withPlanDiagnostic.BlockedDiagnostic = err
	for _, pair := range []struct {
		name          string
		before, after any
	}{
		{"snapshot", snapshot, withDiagnostics}, {"runtime", runtime, withRuntimeDiagnostics},
		{"rules", rules, withRulesDiagnostics}, {"plan", plan, withPlanDiagnostic},
	} {
		t.Run(pair.name, func(t *testing.T) {
			before, err := json.Marshal(pair.before)
			if err != nil {
				t.Fatal(err)
			}
			after, err := json.Marshal(pair.after)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatalf("machine output changed:\n%s\n%s", before, after)
			}
		})
	}
	if planSeal(plan) != planSeal(withPlanDiagnostic) {
		t.Fatal("presentation-only information changed install plan authorization")
	}
}
