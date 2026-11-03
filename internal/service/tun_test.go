package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/mihomo"
)

type tunNetwork struct {
	links     []tunLink
	rules     map[string][]map[string]any
	routes    map[string][]map[string]any
	mutations []string
}

func (n *tunNetwork) run(_ context.Context, args ...string) ([]byte, error) {
	if slices.Equal(args, []string{"-j", "-d", "link", "show"}) {
		return json.Marshal(n.links)
	}
	if len(args) >= 4 && args[0] == "-j" && args[2] == "rule" {
		return json.Marshal(n.rules[args[1]])
	}
	if len(args) >= 4 && args[0] == "-j" && args[2] == "route" {
		return json.Marshal(n.routes[args[1]])
	}
	n.mutations = append(n.mutations, strings.Join(args, " "))
	if len(args) == 6 && args[0] == "link" && args[1] == "set" {
		for i := range n.links {
			if n.links[i].Name == args[3] {
				n.links[i].Alias = args[5]
			}
		}
		return nil, nil
	}
	if len(args) == 5 && args[1] == "rule" && args[2] == "delete" {
		priority, _ := strconv.Atoi(args[4])
		for i, rule := range n.rules[args[0]] {
			if rule["priority"] == priority {
				n.rules[args[0]] = slices.Delete(n.rules[args[0]], i, i+1)
				return nil, nil
			}
		}
		return nil, fmt.Errorf("missing rule")
	}
	if len(args) >= 3 && args[1] == "route" && args[2] == "flush" {
		n.routes[args[0]] = nil
		return nil, nil
	}
	if len(args) == 4 && args[0] == "link" && args[1] == "delete" {
		n.links = slices.DeleteFunc(n.links, func(l tunLink) bool { return l.Name == args[3] })
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected ip command %v", args)
}

func tunFixture(t *testing.T) (TUNLifecycle, *tunNetwork, mihomo.TUNResources) {
	t.Helper()
	n := &tunNetwork{rules: map[string][]map[string]any{}, routes: map[string][]map[string]any{}}
	l := TUNLifecycle{Home: "/var/lib/lazyclash/test", Name: "lazyclash-test", InstanceID: strings.Repeat("a", 24), Enabled: true, root: filepath.Join(t.TempDir(), "run"), invocation: strings.Repeat("b", 32), namespace: "net:[1234]", privileged: func() bool { return true }, run: n.run}
	r, err := mihomo.ManagedTUN(l.Name)
	if err != nil {
		t.Fatal(err)
	}
	return l, n, r
}

func populateTUN(n *tunNetwork, r mihomo.TUNResources) {
	link := tunLink{Index: 12, Name: r.Device}
	link.Info.Kind = "tun"
	n.links = []tunLink{link}
	for _, family := range []string{"-4", "-6"} {
		n.rules[family] = []map[string]any{{"priority": r.Rule, "table": r.Table}}
		n.routes[family] = []map[string]any{{"dst": "default", "dev": r.Device, "table": r.Table}}
	}
}

func TestTUNExclusiveOwnershipAcrossInvocations(t *testing.T) {
	l, n, r := tunFixture(t)
	ctx := context.Background()
	if err := l.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	other := l
	other.Name, other.InstanceID, other.invocation = "lazyclash-other", strings.Repeat("c", 24), strings.Repeat("d", 32)
	if err := other.Prepare(ctx); err == nil || !strings.Contains(err.Error(), l.Name) {
		t.Fatalf("missing owner conflict: %v", err)
	}
	if err := other.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.claim(); err != nil {
		t.Fatal("competing cleanup removed owner", err)
	}
	populateTUN(n, r)
	if err := l.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	path, _ := l.path()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("claim remains: %v", err)
	}
	if len(n.links)+len(n.rules["-4"])+len(n.rules["-6"])+len(n.routes["-4"])+len(n.routes["-6"]) != 0 {
		t.Fatal("network resources remain")
	}
	if err := other.Prepare(ctx); err != nil {
		t.Fatal("handoff failed", err)
	}
}

func TestTUNCleanupAfterKilledCorePreservesForeignChanges(t *testing.T) {
	for _, change := range []string{"vanished-device", "changed-device", "foreign-rule", "foreign-route"} {
		t.Run(change, func(t *testing.T) {
			l, n, r := tunFixture(t)
			ctx := context.Background()
			if err := l.Prepare(ctx); err != nil {
				t.Fatal(err)
			}
			populateTUN(n, r)
			if err := l.Ready(ctx); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "vanished-device":
				n.links = nil
				n.routes["-4"], n.routes["-6"] = nil, nil
			case "changed-device":
				n.links[0].Index++
			case "foreign-rule":
				n.rules["-4"] = append(n.rules["-4"], map[string]any{"priority": r.Rule, "table": 123})
			case "foreign-route":
				n.routes["-4"] = append(n.routes["-4"], map[string]any{"dev": "eth0", "dst": "10.0.0.0/8", "table": r.Table})
			}
			err := l.Cleanup(ctx)
			if change == "vanished-device" {
				if err != nil {
					t.Fatal(err)
				}
				if len(n.rules["-4"])+len(n.rules["-6"]) != 0 {
					t.Fatal("orphan policy rules remain after SIGKILL")
				}
			} else {
				if err == nil {
					t.Fatal("foreign network changes were accepted")
				}
				if _, _, err := l.claim(); err != nil {
					t.Fatal("claim lost after unsafe cleanup", err)
				}
			}
		})
	}
}

func TestTUNRejectsOccupiedInterfacesAndRuleRanges(t *testing.T) {
	for _, occupied := range []string{"tun", "rules", "table"} {
		t.Run(occupied, func(t *testing.T) {
			l, n, r := tunFixture(t)
			switch occupied {
			case "tun":
				link := tunLink{Name: "other-vpn"}
				link.Info.Kind = "tun"
				n.links = []tunLink{link}
			case "rules":
				n.rules["-4"] = []map[string]any{{"priority": r.Rule + 10, "table": 123}}
			case "table":
				n.routes["-6"] = []map[string]any{{"dev": "eth0", "table": r.Table}}
			}
			if err := l.Prepare(context.Background()); err == nil {
				t.Fatal("occupied resources accepted")
			}
			if len(n.mutations) != 0 {
				t.Fatal("preflight changed network")
			}
		})
	}
}

func TestTUNFailedStartupAndDisabledPolicy(t *testing.T) {
	l, n, r := tunFixture(t)
	ctx := context.Background()
	if err := l.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	n.rules["-4"] = []map[string]any{{"priority": r.Rule, "src": "all", "dst": "198.18.0.0", "dstlen": 30, "table": r.Table}}
	if err := l.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if len(n.rules["-4"]) != 0 {
		t.Fatal("partial startup rules remain")
	}
	l.Enabled = false
	l.invocation = ""
	if err := l.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	l.Enabled = true
	if err := l.Prepare(ctx); err == nil {
		t.Fatal("direct invocation without systemd accepted")
	}
}

func TestTUNRefusesCorruptResourceOwnershipWithoutNetworkMutation(t *testing.T) {
	l, n, _ := tunFixture(t)
	ctx := context.Background()
	if err := l.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	c, old, err := l.claim()
	if err != nil {
		t.Fatal(err)
	}
	c.Resources.Table = 254
	if err := l.save(c, old); err != nil {
		t.Fatal(err)
	}
	if err := l.Cleanup(ctx); err == nil {
		t.Fatal("corrupt resource ownership was accepted")
	}
	if len(n.mutations) != 0 {
		t.Fatal("corrupt claim changed network resources")
	}
}

func TestTUNFailedStartupPreservesForeignRules(t *testing.T) {
	l, n, r := tunFixture(t)
	ctx := context.Background()
	if err := l.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	n.rules["-4"] = []map[string]any{{"priority": r.Rule, "table": 123}}
	if err := l.Cleanup(ctx); err == nil {
		t.Fatal("foreign startup rule was accepted")
	}
	if len(n.mutations) != 0 {
		t.Fatal("foreign startup rule was deleted")
	}
	if _, _, err := l.claim(); err != nil {
		t.Fatal("claim was lost", err)
	}
}

func TestTUNRulesNormalizeDetachedInterfacesAndNumericTableNames(t *testing.T) {
	l, n, r := tunFixture(t)
	n.rules["-4"] = []map[string]any{{"priority": r.Rule + 1, "src": "all", "iif": r.Device, "iif_detached": nil, "goto": r.Rule + 10}}
	rules, err := l.rules(context.Background(), "-4", r)
	if err != nil || !startupRulesOwned(rules, "-4", r) {
		t.Fatalf("detached managed rule lost: %s %v", rules, err)
	}
	n.routes["-4"] = []map[string]any{{"table": strconv.Itoa(r.Table), "dev": r.Device, "dst": "default"}}
	routes, err := l.routes(context.Background(), "-4", r)
	if err != nil || len(routes) != 1 {
		t.Fatalf("numeric routing table name lost: %s %v", routes, err)
	}
}

func TestTUNFailedStartupPreservesUnrecordedInterface(t *testing.T) {
	l, n, r := tunFixture(t)
	ctx := context.Background()
	if err := l.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	link := tunLink{Index: 99, Name: r.Device}
	link.Info.Kind = "tun"
	n.links = []tunLink{link}
	if err := l.Cleanup(ctx); err == nil {
		t.Fatal("unrecorded interface was accepted")
	}
	if len(n.mutations) != 0 {
		t.Fatal("unrecorded interface was deleted")
	}
	if _, _, err := l.claim(); err != nil {
		t.Fatal("claim lost after unsafe cleanup", err)
	}
}
