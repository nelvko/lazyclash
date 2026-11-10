package service

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProcessStartedAtUsesMonotonicTimeAndRejectsMissingOrFutureData(t *testing.T) {
	now := time.Now()
	monotonic := 24 * time.Hour
	start := monotonic - 2*time.Hour - 18*time.Minute - 34*time.Second
	anchor := processStartedAt(uint64(start/time.Microsecond), now, monotonic)
	if now.Sub(anchor) != 2*time.Hour+18*time.Minute+34*time.Second {
		t.Fatalf("uptime came from a wall clock or UI start: %v", now.Sub(anchor))
	}
	// Advancing the local monotonic clock advances elapsed time immediately.
	if now.Add(time.Second).Sub(anchor) != now.Sub(anchor)+time.Second {
		t.Fatal("local elapsed time did not advance")
	}
	for _, value := range []uint64{0, uint64(monotonic/time.Microsecond) + 1, math.MaxUint64} {
		if !processStartedAt(value, now, monotonic).IsZero() {
			t.Fatalf("invalid process timestamp created an uptime: %d", value)
		}
	}
	if !processStartedAt(1, now, -time.Second).IsZero() {
		t.Fatal("invalid local clock created an uptime")
	}
}

func TestInspectBindsProcessStartToActiveCurrentPIDInTheExistingQuery(t *testing.T) {
	for _, tc := range []struct {
		name, state, pid, mainPID, start string
		known                            bool
	}{
		{"current", "active", "4321", "4321", "1", true},
		{"stopped last process", "inactive", "0", "4321", "1", false},
		{"failed last process", "failed", "0", "4321", "1", false},
		{"stopping", "deactivating", "4321", "4321", "1", false},
		{"unknown", "unknown", "4321", "4321", "1", false},
		{"different main process", "active", "4321", "1234", "1", false},
		{"no current process", "active", "0", "0", "1", false},
		{"missing main process", "active", "4321", "", "1", false},
		{"missing timestamp", "active", "4321", "4321", "", false},
		{"zero timestamp", "active", "4321", "4321", "0", false},
		{"negative timestamp", "active", "4321", "4321", "-1", false},
		{"corrupt timestamp", "active", "4321", "4321", "unknown", false},
		{"overflow or future", "active", "4321", "4321", strconv.FormatUint(math.MaxUint64, 10), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := testManager(t)
			queries := 0
			s.run = func(_ context.Context, command string, args ...string) ([]byte, error) {
				if command == "loginctl" {
					return []byte("Linger=yes\n"), nil
				}
				queries++
				if !slices.Contains(args, "--property=LoadState,ActiveState,SubState,MainPID,ExecMainPID,ExecMainStartTimestampMonotonic,Result,ExecMainStatus,NRestarts,UnitFileState") {
					t.Fatalf("start time was not part of the existing status query: %v", args)
				}
				return []byte(strings.Join([]string{"LoadState=loaded", "ActiveState=" + tc.state,
					"MainPID=" + tc.pid, "ExecMainPID=" + tc.mainPID, "ExecMainStartTimestampMonotonic=" + tc.start}, "\n")), nil
			}
			before := time.Now()
			status, err := s.Inspect(t.Context())
			if err != nil || queries != 1 || status.State != tc.state {
				t.Fatalf("timing metadata changed service inspection: %+v / %v / %d queries", status, err, queries)
			}
			_, _, clockErr := processClock()
			if tc.known && clockErr == nil {
				if status.StartTimestampMonotonic != 1 || status.StartedAt.IsZero() || status.StartedAt.After(before) {
					t.Fatalf("confirmed process has no real start anchor: %+v", status)
				}
			} else if !status.StartedAt.IsZero() || status.StartTimestampMonotonic != 0 {
				t.Fatalf("unconfirmed process inherited timing metadata: %+v", status)
			}
		})
	}
}
