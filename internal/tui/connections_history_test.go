package tui

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/nelvko/lazyclash/internal/app"
	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/service"
)

func historySnapshot(at time.Time, rows ...mihomo.Connection) app.RuntimeSnapshot {
	return app.RuntimeSnapshot{
		State: "ready", ObservedAt: at, Service: service.Status{PID: 123}, RunningVersion: "v1.19.33",
		Connections: &mihomo.ConnectionSnapshot{UploadTotal: 1000, DownloadTotal: 2000, Connections: rows},
	}
}

func TestConnectionHistoryRetainsLatestObservedValuesAndClosureTime(t *testing.T) {
	at := time.Unix(1000, 0)
	one := mihomo.Connection{ID: "one", Start: at.Add(-time.Minute), Upload: 10, Download: 20, Chains: []string{"DIRECT"}}
	two := mihomo.Connection{ID: "two", Start: at.Add(-time.Minute)}
	var history connectionHistoryState
	history.observe(historySnapshot(at, two, one), at.Add(time.Hour), 1, "installation")
	if len(history.entries()) != 0 {
		t.Fatal("first observation fabricated closed connections")
	}
	one.Upload, one.Download = 100, 200
	history.observe(historySnapshot(at.Add(time.Second), two, one), at.Add(time.Hour), 1, "installation")
	closedAt := at.Add(2 * time.Second)
	history.observe(historySnapshot(closedAt, two), at.Add(time.Hour), 1, "installation")
	entries := history.entries()
	if len(entries) != 1 || entries[0].Connection.ID != "one" || entries[0].Connection.Upload != 100 || entries[0].Connection.Download != 200 || !entries[0].ClosedAt.Equal(closedAt) {
		t.Fatalf("incorrect closed observation: %+v", entries)
	}
	lastAt := at.Add(3 * time.Second)
	history.observe(historySnapshot(lastAt), at, 1, "installation")
	entries = history.entries()
	if len(entries) != 2 || entries[0].Connection.ID != "two" || !entries[0].ClosedAt.Equal(lastAt) || entries[1].Connection.ID != "one" {
		t.Fatalf("history is not ordered by latest closure: %+v", entries)
	}
	history.observe(historySnapshot(lastAt.Add(time.Second)), at, 1, "installation")
	if len(history.entries()) != 2 {
		t.Fatal("unchanged empty observation duplicated closures")
	}
}

func TestConnectionHistoryDoesNotAliasSnapshotsOrReturnedEntries(t *testing.T) {
	at := time.Unix(1000, 0)
	row := mihomo.Connection{ID: "one", Start: at, Download: 20, Chains: []string{"PROXY", "node"}}
	initial := historySnapshot(at, row)
	var history connectionHistoryState
	history.observe(initial, at, 1, "installation")
	initial.Connections.Connections[0].Download = 999
	initial.Connections.Connections[0].Chains[0] = "MUTATED"
	history.observe(historySnapshot(at.Add(time.Second)), at, 1, "installation")
	entries := history.entries()
	if entries[0].Connection.Download != 20 || entries[0].Connection.Chains[0] != "PROXY" {
		t.Fatalf("snapshot mutation changed stored history: %+v", entries[0])
	}
	entries[0].Connection.Download = 888
	entries[0].Connection.Chains[0] = "RETURNED"
	entries[0].ClosedAt = time.Time{}
	again := history.entries()
	if again[0].Connection.Download != 20 || again[0].Connection.Chains[0] != "PROXY" || again[0].ClosedAt.IsZero() {
		t.Fatal("returned entries alias retained history")
	}
}

func TestConnectionHistoryIgnoresDuplicateAndOutOfOrderSuccessfulSamples(t *testing.T) {
	at := time.Unix(1000, 0)
	one := mihomo.Connection{ID: "one", Start: at}
	var history connectionHistoryState
	history.observe(historySnapshot(at, one), at, 1, "installation")
	history.observe(historySnapshot(at), at, 1, "installation")
	history.observe(historySnapshot(at.Add(-time.Second)), at, 1, "installation")
	stale := historySnapshot(at.Add(-2 * time.Second))
	stale.Connections.UploadTotal, stale.Connections.DownloadTotal = 0, 0
	history.observe(stale, at, 1, "installation")
	if len(history.entries()) != 0 || len(history.active) != 1 {
		t.Fatal("same or older timestamp changed successful baseline")
	}
	history.observe(historySnapshot(at.Add(time.Second)), at, 1, "installation")
	if entries := history.entries(); len(entries) != 1 || entries[0].Connection.ID != "one" {
		t.Fatal("new successful sample did not observe closure")
	}
	history.observe(historySnapshot(at.Add(time.Second), one), at, 1, "installation")
	history.observe(historySnapshot(at.Add(2*time.Second)), at, 1, "installation")
	if len(history.entries()) != 1 {
		t.Fatal("replayed same-time sample reintroduced a closed connection")
	}
}

func TestConnectionHistoryInterruptionsPreserveHistoryWithoutInferringClosures(t *testing.T) {
	at := time.Unix(1000, 0)
	one := mihomo.Connection{ID: "one", Start: at}
	two := mihomo.Connection{ID: "two", Start: at}
	for _, state := range []string{"stopped", "uninstalled", "unsupported", "unavailable", "missing-connections"} {
		t.Run(state, func(t *testing.T) {
			var history connectionHistoryState
			history.observe(historySnapshot(at, one, two), at, 1, "installation")
			history.observe(historySnapshot(at.Add(time.Second), two), at, 1, "installation")
			gap := app.RuntimeSnapshot{State: state, ObservedAt: at.Add(2 * time.Second)}
			if state == "missing-connections" {
				gap.State = "ready"
			}
			history.observe(gap, at, 0, "")
			if len(history.entries()) != 1 || len(history.active) != 0 {
				t.Fatal("observation gap cleared history or inferred active closures")
			}
			history.observe(historySnapshot(at.Add(3*time.Second)), at, 1, "installation")
			if len(history.entries()) != 1 {
				t.Fatal("first recovery sample inferred closures across the gap")
			}
			history.observe(historySnapshot(at.Add(4*time.Second), two), at, 1, "installation")
			history.observe(historySnapshot(at.Add(5*time.Second)), at, 1, "installation")
			if entries := history.entries(); len(entries) != 2 || entries[0].Connection.ID != "two" {
				t.Fatal("consecutive successful observations after gap did not recover")
			}
		})
	}
}

func TestConnectionHistoryClearsForNewCoreIdentity(t *testing.T) {
	at := time.Unix(1000, 0)
	one := mihomo.Connection{ID: "one", Start: at}
	two := mihomo.Connection{ID: "two", Start: at}
	for _, changed := range []string{"pid", "version", "instance", "installation", "upload-reset", "download-reset"} {
		t.Run(changed, func(t *testing.T) {
			var history connectionHistoryState
			history.observe(historySnapshot(at, one, two), at, 1, "installation")
			history.observe(historySnapshot(at.Add(time.Second), two), at, 1, "installation")
			next := historySnapshot(at.Add(2 * time.Second))
			instance, installation := uint64(1), "installation"
			switch changed {
			case "pid":
				next.Service.PID++
			case "version":
				next.RunningVersion = "v1.20.0"
			case "instance":
				instance++
			case "installation":
				installation = "replacement"
			case "upload-reset":
				next.Connections.UploadTotal = 0
			case "download-reset":
				next.Connections.DownloadTotal = 0
			}
			history.observe(next, at, instance, installation)
			if len(history.entries()) != 0 || history.generation != 1 {
				t.Fatal("new core inherited previous history or closed its active rows")
			}
		})
	}
}

func TestConnectionHistoryFindMatchesIDAndStartAndClearInvalidatesGeneration(t *testing.T) {
	at := time.Unix(1000, 0)
	one := mihomo.Connection{ID: "same", Start: at.Add(-time.Minute), Download: 100}
	two := mihomo.Connection{ID: "same", Start: at, Download: 20}
	var history connectionHistoryState
	history.observe(historySnapshot(at, one), at, 1, "installation")
	history.observe(historySnapshot(at.Add(time.Second), two), at, 1, "installation")
	history.observe(historySnapshot(at.Add(2*time.Second)), at, 1, "installation")
	if found, ok := history.find(one); !ok || found.Connection.Download != 100 {
		t.Fatal("find confused original connection with newer lifetime")
	}
	if found, ok := history.find(two); !ok || found.Connection.Download != 20 {
		t.Fatal("find did not retrieve the later lifetime")
	}
	if _, ok := history.find(mihomo.Connection{ID: "same", Start: at.Add(time.Hour)}); ok {
		t.Fatal("find ignored the connection start time")
	}
	if history.generation != 0 {
		t.Fatal("ordinary closures invalidated the core generation")
	}
	history.clear()
	if len(history.entries()) != 0 || history.active != nil || history.observed || history.generation != 1 {
		t.Fatal("clear did not reset history and baseline or invalidate the generation")
	}
}

func TestConnectionHistoryArchivesReusedIDAsSeparateLifetimes(t *testing.T) {
	at := time.Unix(1000, 0)
	one := mihomo.Connection{ID: "same", Start: at.Add(-time.Minute), Download: 100}
	var history connectionHistoryState
	history.observe(historySnapshot(at, one), at, 1, "installation")
	two := mihomo.Connection{ID: "same", Start: at.Add(time.Second), Download: 20}
	history.observe(historySnapshot(at.Add(time.Second), two), at, 1, "installation")
	if entries := history.entries(); len(entries) != 1 || !entries[0].Connection.Start.Equal(one.Start) || entries[0].Connection.Download != 100 {
		t.Fatal("replacement lifetime overwrote the original connection")
	}
	history.observe(historySnapshot(at.Add(2*time.Second)), at, 1, "installation")
	entries := history.entries()
	if len(entries) != 2 || !entries[0].Connection.Start.Equal(two.Start) || entries[0].Connection.Download != 20 {
		t.Fatal("reused ID did not produce its own closed entry")
	}
	// A previously absent identifier may be observed again and close later.
	three := mihomo.Connection{ID: "same", Start: at.Add(3 * time.Second), Download: 30}
	history.observe(historySnapshot(at.Add(3*time.Second), three), at, 1, "installation")
	history.observe(historySnapshot(at.Add(4*time.Second)), at, 1, "installation")
	if entries := history.entries(); len(entries) != 3 || !entries[0].Connection.Start.Equal(three.Start) {
		t.Fatal("later reuse was deduplicated against an earlier lifetime")
	}
}

func TestConnectionHistoryReappearingLifetimeUpdatesItsExistingEntry(t *testing.T) {
	at := time.Unix(1000, 0)
	one := mihomo.Connection{ID: "same", Start: at.Add(-time.Minute), Download: 100, Chains: []string{"old-node"}}
	other := mihomo.Connection{ID: "other", Start: at}
	var history connectionHistoryState
	history.observe(historySnapshot(at, one, other), at, 1, "installation")
	history.observe(historySnapshot(at.Add(time.Second), other), at, 1, "installation")
	history.observe(historySnapshot(at.Add(2*time.Second)), at, 1, "installation")
	one.Download, one.Chains = 300, []string{"new-node"}
	history.observe(historySnapshot(at.Add(3*time.Second), one), at, 1, "installation")
	closedAt := at.Add(4 * time.Second)
	history.observe(historySnapshot(closedAt), at, 1, "installation")
	entries := history.entries()
	if len(entries) != 2 || entries[0].Connection.ID != "same" || entries[0].Connection.Download != 300 || entries[0].Connection.Chains[0] != "new-node" || !entries[0].ClosedAt.Equal(closedAt) || entries[1].Connection.ID != "other" {
		t.Fatalf("reappearing lifetime was duplicated or failed to update and move to newest: %+v", entries)
	}
	if found, ok := history.find(one); !ok || found.Connection.Download != 300 || !found.ClosedAt.Equal(closedAt) {
		t.Fatal("find returned the replaced observation of the same lifetime")
	}
	// A distinct lifetime with the same identifier still gets a separate row.
	two := mihomo.Connection{ID: "same", Start: at.Add(5 * time.Second)}
	history.observe(historySnapshot(at.Add(5*time.Second), two), at, 1, "installation")
	history.observe(historySnapshot(at.Add(6*time.Second)), at, 1, "installation")
	if entries := history.entries(); len(entries) != 3 || !entries[0].Connection.Start.Equal(two.Start) || !entries[1].Connection.Start.Equal(one.Start) {
		t.Fatal("lifetime deduplication removed a distinct connection with the same ID")
	}
}

func TestConnectionHistoryRetainsOnlyNewestFiveHundred(t *testing.T) {
	at := time.Unix(1000, 0)
	var history connectionHistoryState
	for i := 0; i < connectionHistoryLimit+3; i++ {
		current := at.Add(time.Duration(2*i) * time.Second)
		row := mihomo.Connection{ID: fmt.Sprintf("row-%03d", i), Start: current}
		history.observe(historySnapshot(current, row), current, 1, "installation")
		history.observe(historySnapshot(current.Add(time.Second)), current, 1, "installation")
	}
	entries := history.entries()
	if len(entries) != connectionHistoryLimit || entries[0].Connection.ID != "row-502" || entries[len(entries)-1].Connection.ID != "row-003" {
		t.Fatalf("incorrect retained history boundaries: count=%d first=%s last=%s", len(entries), entries[0].Connection.ID, entries[len(entries)-1].Connection.ID)
	}
	// A single large batch also releases entries beyond the retained limit.
	var rows []mihomo.Connection
	for i := 0; i < 2*connectionHistoryLimit; i++ {
		rows = append(rows, mihomo.Connection{ID: fmt.Sprintf("batch-%04d", i), Start: at})
	}
	history = connectionHistoryState{}
	history.observe(historySnapshot(at, rows...), at, 1, "installation")
	history.observe(historySnapshot(at.Add(time.Second)), at, 1, "installation")
	if len(history.closed) != connectionHistoryLimit || cap(history.closed) >= len(rows) {
		t.Fatal("large closure batch retained backing storage for discarded entries")
	}
}

func TestConnectionHistoryDeterministicBatchOrderAndReceiptTimestampFallback(t *testing.T) {
	at := time.Unix(1000, 0)
	var history connectionHistoryState
	history.observe(historySnapshot(time.Time{}, mihomo.Connection{ID: "z"}, mihomo.Connection{ID: "a"}), at, 1, "installation")
	closedAt := at.Add(time.Second)
	history.observe(historySnapshot(time.Time{}), closedAt, 1, "installation")
	entries := history.entries()
	var ids []string
	for _, entry := range entries {
		ids = append(ids, entry.Connection.ID)
		if !entry.ClosedAt.Equal(closedAt) {
			t.Fatal("missing source timestamp did not use receipt timestamp")
		}
	}
	if !reflect.DeepEqual(ids, []string{"a", "z"}) {
		t.Fatalf("unstable same-time closure order: %v", ids)
	}
}
