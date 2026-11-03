package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
	"github.com/nelvko/lazyclash/internal/store"
)

// TUNLifecycle is invoked by the system unit, including direct systemctl starts
// and automatic restarts. Its short-lived hooks share a persistent claim; a
// process-scoped lock in ExecStartPre would end before the core starts.
type TUNLifecycle struct {
	Home, Name, InstanceID string
	Enabled                bool
	root                   string
	invocation             string
	namespace              string
	privileged             func() bool
	run                    func(context.Context, ...string) ([]byte, error)
}

type tunClaim struct {
	Home, Service, Instance, Invocation string
	Resources                           mihomo.TUNResources
	IfIndex                             int
	Alias                               string
	Ready                               bool
	Rules4, Rules6                      []json.RawMessage
	Routes4, Routes6                    []json.RawMessage
}

type tunLink struct {
	Index int    `json:"ifindex"`
	Name  string `json:"ifname"`
	Alias string `json:"ifalias"`
	Info  struct {
		Kind string `json:"info_kind"`
	} `json:"linkinfo"`
}

func (t TUNLifecycle) base() string {
	if t.root != "" {
		return t.root
	}
	return "/run/lazyclash"
}

// PrepareRuntime prepares the root-owned coordination directory at install time,
// before systemd's filesystem sandbox is established.
func PrepareTUNRuntime() error { return protectedTUNDir("/run/lazyclash") }

func protectedTUNDir(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("unsafe TUN coordination directory")
	}
	return nil
}

func (t TUNLifecycle) validate() error {
	privileged := runtime.GOOS == "linux" && os.Geteuid() == 0
	if t.privileged != nil {
		privileged = t.privileged()
	}
	if !privileged {
		return fmt.Errorf("TUN lifecycle requires root on Linux")
	}
	if !filepath.IsAbs(t.Home) || !regexp.MustCompile(`^[a-f0-9]{24}$`).MatchString(t.InstanceID) {
		return fmt.Errorf("invalid TUN installation identity")
	}
	_, err := mihomo.ManagedTUN(t.Name)
	return err
}

func (t TUNLifecycle) invocationID() (string, error) {
	id := t.invocation
	if id == "" {
		id = os.Getenv("INVOCATION_ID")
	}
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(id) {
		return "", fmt.Errorf("TUN lifecycle must be invoked by systemd with an invocation ID")
	}
	return id, nil
}

func (t TUNLifecycle) path() (string, error) {
	ns := t.namespace
	if ns == "" {
		var err error
		ns, err = os.Readlink("/proc/self/ns/net")
		if err != nil {
			return "", err
		}
	}
	if !regexp.MustCompile(`^net:\[[0-9]+\]$`).MatchString(ns) {
		return "", fmt.Errorf("cannot identify network namespace")
	}
	return filepath.Join(t.base(), "netns-"+strings.TrimSuffix(strings.TrimPrefix(ns, "net:["), "]")+".json"), nil
}

func (t TUNLifecycle) command(ctx context.Context, args ...string) ([]byte, error) {
	if t.run != nil {
		return t.run(ctx, args...)
	}
	out, err := exec.CommandContext(ctx, "/usr/sbin/ip", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("inspect or clean TUN network resources: %w", err)
	}
	return out, nil
}

func (t TUNLifecycle) lock(ctx context.Context) (func(), error) {
	if err := t.validate(); err != nil {
		return nil, err
	}
	if err := protectedTUNDir(t.base()); err != nil {
		return nil, err
	}
	return store.Lock(ctx, t.base())
}

func (t TUNLifecycle) claim() (tunClaim, []byte, error) {
	path, err := t.path()
	if err != nil {
		return tunClaim{}, nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return tunClaim{}, nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return tunClaim{}, nil, fmt.Errorf("unsafe TUN ownership record")
	}
	raw, err := store.Read(path)
	if err != nil {
		return tunClaim{}, nil, err
	}
	var claim tunClaim
	if json.Unmarshal(raw, &claim) != nil || claim.Instance == "" || claim.Service == "" || claim.Invocation == "" {
		return claim, nil, fmt.Errorf("invalid TUN ownership record; administrator inspection required")
	}
	expected, resourceErr := mihomo.ManagedTUN(claim.Service)
	if resourceErr != nil || claim.Resources != expected || !filepath.IsAbs(claim.Home) || !regexp.MustCompile(`^[a-f0-9]{24}$`).MatchString(claim.Instance) || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(claim.Invocation) || claim.IfIndex < 0 || (claim.IfIndex > 0 && claim.Alias != "lazyclash:"+claim.Instance+":"+claim.Invocation) {
		return claim, nil, fmt.Errorf("invalid TUN resource ownership; administrator inspection required")
	}
	return claim, raw, nil
}

func (t TUNLifecycle) save(c tunClaim, old []byte) error {
	path, err := t.path()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return store.Replace(path, raw, old, 0600)
}

func (t TUNLifecycle) owns(c tunClaim, invocation string) bool {
	return c.Instance == t.InstanceID && c.Home == t.Home && c.Service == t.Name && c.Invocation == invocation
}

func (t TUNLifecycle) links(ctx context.Context) ([]tunLink, error) {
	raw, err := t.command(ctx, "-j", "-d", "link", "show")
	if err != nil {
		return nil, err
	}
	var out []tunLink
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("invalid network link inspection")
	}
	return out, nil
}

func tableNumber(value any) (int, bool) {
	switch value := value.(type) {
	case float64:
		return int(value), value == float64(int(value))
	case string:
		switch value {
		case "main":
			return 254, true
		case "local":
			return 255, true
		case "default":
			return 253, true
		}
		n, err := strconv.Atoi(value)
		return n, err == nil
	}
	return 0, false
}

func normalizeRule(entry map[string]any) {
	delete(entry, "iif_detached")
	delete(entry, "oif_detached")
	if table, ok := tableNumber(entry["table"]); ok {
		entry["table"] = table
	}
	for _, flag := range []string{"not", "nop"} {
		if _, present := entry[flag]; present {
			entry[flag] = true
		}
	}
}

// The policy is fixed by WithTUN. Before readiness has recorded exact rules,
// only these sing-tun-generated signatures can be attributed to this instance.
func startupRulesOwned(rules []json.RawMessage, family string, r mihomo.TUNResources) bool {
	var expected []map[string]any
	add := func(offset int, fields map[string]any) {
		fields["priority"] = r.Rule + offset
		if fields["src"] == nil {
			fields["src"] = "all"
		}
		expected = append(expected, fields)
	}
	add(10, map[string]any{"nop": true})
	if family == "-4" {
		add(0, map[string]any{"dst": "198.18.0.0", "dstlen": 30, "table": r.Table})
		add(1, map[string]any{"not": true, "dport": 53, "table": 254, "suppress_prefixlen": 0})
		add(1, map[string]any{"iif": r.Device, "goto": r.Rule + 10})
		add(2, map[string]any{"not": true, "iif": "lo", "table": r.Table})
		add(2, map[string]any{"iif": "lo", "src": "0.0.0.0", "table": r.Table})
		add(2, map[string]any{"iif": "lo", "src": "198.18.0.0", "srclen": 30, "table": r.Table})
	} else {
		add(0, map[string]any{"not": true, "dport": 53, "table": 254, "suppress_prefixlen": 0})
		add(0, map[string]any{"iif": r.Device, "goto": r.Rule + 10})
		add(0, map[string]any{"iif": "lo", "src": "::", "srclen": 1, "goto": r.Rule + 10})
		add(0, map[string]any{"iif": "lo", "src": "8000::", "srclen": 1, "goto": r.Rule + 10})
		add(1, map[string]any{"iif": "lo", "src": "fdfe:dcba:9876::", "srclen": 126, "table": r.Table})
		add(2, map[string]any{"table": r.Table})
	}
	encoded := make([]json.RawMessage, len(expected))
	for i, entry := range expected {
		encoded[i], _ = json.Marshal(entry)
	}
	return rulesSubset(rules, encoded)
}

func (t TUNLifecycle) rules(ctx context.Context, family string, r mihomo.TUNResources) ([]json.RawMessage, error) {
	raw, err := t.command(ctx, "-j", family, "rule", "show")
	if err != nil {
		return nil, err
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil, fmt.Errorf("invalid policy rule inspection")
	}
	var out []json.RawMessage
	for _, raw := range entries {
		var entry map[string]any
		if json.Unmarshal(raw, &entry) != nil {
			return nil, fmt.Errorf("invalid policy rule entry")
		}
		priority, ok := entry["priority"].(float64)
		if ok && int(priority) >= r.Rule && int(priority) <= r.Rule+10 {
			normalizeRule(entry)
			canonical, _ := json.Marshal(entry)
			out = append(out, canonical)
		}
	}
	return out, nil
}

func (t TUNLifecycle) routes(ctx context.Context, family string, r mihomo.TUNResources) ([]json.RawMessage, error) {
	raw, err := t.command(ctx, "-j", family, "route", "show", "table", "all")
	if err != nil {
		return nil, err
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil, fmt.Errorf("invalid routing table inspection")
	}
	var out []json.RawMessage
	for _, raw := range entries {
		var entry map[string]any
		if json.Unmarshal(raw, &entry) != nil {
			return nil, fmt.Errorf("invalid routing table entry")
		}
		table, ok := tableNumber(entry["table"])
		if ok && table == r.Table {
			canonical, _ := json.Marshal(entry)
			out = append(out, canonical)
		}
	}
	return out, nil
}

func (t TUNLifecycle) vacant(ctx context.Context, r mihomo.TUNResources) error {
	links, err := t.links(ctx)
	if err != nil {
		return err
	}
	for _, link := range links {
		if link.Info.Kind == "tun" || link.Name == r.Device {
			return fmt.Errorf("TUN conflict: network interface %s already exists; an administrator must stop its owner before enabling TUN", link.Name)
		}
	}
	for _, family := range []string{"-4", "-6"} {
		rules, err := t.rules(ctx, family, r)
		if err != nil {
			return err
		}
		routes, err := t.routes(ctx, family, r)
		if err != nil {
			return err
		}
		if len(rules) != 0 || len(routes) != 0 {
			return fmt.Errorf("TUN conflict: routing table %d or rule priorities %d-%d are occupied", r.Table, r.Rule, r.Rule+10)
		}
	}
	return nil
}

// CheckConflict is a non-mutating preflight for the UI. Prepare repeats it under
// the coordination lock, so a competing start cannot race the check.
func (t TUNLifecycle) CheckConflict(ctx context.Context) error {
	unlock, err := t.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	claim, _, err := t.claim()
	if err == nil {
		if claim.Instance == t.InstanceID && claim.Home == t.Home && claim.Service == t.Name {
			return nil
		}
		return fmt.Errorf("TUN conflict: root's instance %s (%s) owns TUN; stop it explicitly before transferring ownership", claim.Service, claim.Home)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	r, _ := mihomo.ManagedTUN(t.Name)
	return t.vacant(ctx, r)
}

func (t TUNLifecycle) Prepare(ctx context.Context) error {
	if !t.Enabled {
		return nil
	}
	invocation, err := t.invocationID()
	if err != nil {
		return err
	}
	unlock, err := t.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	c, _, err := t.claim()
	if err == nil {
		if t.owns(c, invocation) {
			return nil
		}
		return fmt.Errorf("TUN conflict: root's instance %s (%s) owns TUN; stop it explicitly before transferring ownership", c.Service, c.Home)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	r, _ := mihomo.ManagedTUN(t.Name)
	if err := t.vacant(ctx, r); err != nil {
		return err
	}
	return t.save(tunClaim{Home: t.Home, Service: t.Name, Instance: t.InstanceID, Invocation: invocation, Resources: r}, nil)
}

func (t TUNLifecycle) Ready(ctx context.Context) error {
	if !t.Enabled {
		return nil
	}
	invocation, err := t.invocationID()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		ready, err := t.recordReady(ctx, invocation)
		if err != nil || ready {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("TUN interface and routing did not become ready: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (t TUNLifecycle) recordReady(ctx context.Context, invocation string) (bool, error) {
	unlock, err := t.lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	c, old, err := t.claim()
	if err != nil || !t.owns(c, invocation) {
		return false, errors.Join(err, fmt.Errorf("TUN ownership changed while starting"))
	}
	links, err := t.links(ctx)
	if err != nil {
		return false, err
	}
	for _, link := range links {
		if link.Name == c.Resources.Device {
			if link.Info.Kind != "tun" {
				return false, fmt.Errorf("reserved TUN name belongs to another network device")
			}
			alias := "lazyclash:" + c.Instance + ":" + c.Invocation
			if c.IfIndex != 0 && (c.IfIndex != link.Index || link.Alias != alias) {
				return false, fmt.Errorf("TUN interface ownership changed")
			}
			if _, err := t.command(ctx, "link", "set", "dev", link.Name, "alias", alias); err != nil {
				return false, err
			}
			c.IfIndex, c.Alias = link.Index, alias
		}
	}
	c.Rules4, err = t.rules(ctx, "-4", c.Resources)
	if err != nil {
		return false, err
	}
	c.Rules6, err = t.rules(ctx, "-6", c.Resources)
	if err != nil {
		return false, err
	}
	routes4, err := t.routes(ctx, "-4", c.Resources)
	if err != nil {
		return false, err
	}
	routes6, err := t.routes(ctx, "-6", c.Resources)
	if err != nil {
		return false, err
	}
	c.Routes4, c.Routes6 = routes4, routes6
	c.Ready = c.IfIndex != 0 && len(c.Rules4) > 0 && len(c.Rules6) > 0 && len(routes4) > 0 && len(routes6) > 0
	return c.Ready, t.save(c, old)
}

func rulesSubset(current, recorded []json.RawMessage) bool {
	available := make([]string, len(recorded))
	for i := range recorded {
		available[i] = string(recorded[i])
	}
	for _, rule := range current {
		index := slices.Index(available, string(rule))
		if index < 0 {
			return false
		}
		available = slices.Delete(available, index, index+1)
	}
	return true
}

// Cleanup runs after every unit exit, including failed startup and SIGKILL.
// It only deletes resources reserved by this invocation, and refuses changed
// interface identities or rules added after successful readiness.
func (t TUNLifecycle) Cleanup(ctx context.Context) error {
	invocation, err := t.invocationID()
	if err != nil {
		return err
	}
	unlock, err := t.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	c, old, err := t.claim()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !t.owns(c, invocation) {
		return nil // A failed competing start must never release its owner's claim.
	}
	links, err := t.links(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, link := range links {
		if link.Name != c.Resources.Device {
			continue
		}
		if c.IfIndex == 0 || link.Info.Kind != "tun" || link.Index != c.IfIndex || link.Alias != c.Alias {
			return fmt.Errorf("TUN interface changed; preserving claim and network resources for administrator inspection")
		}
		found = true
	}
	currentRules := make(map[string][]json.RawMessage)
	currentRoutes := make(map[string][]json.RawMessage)
	for _, family := range []string{"-4", "-6"} {
		rules, err := t.rules(ctx, family, c.Resources)
		if err != nil {
			return err
		}
		recorded := c.Rules4
		if family == "-6" {
			recorded = c.Rules6
		}
		if (c.Ready && !rulesSubset(rules, recorded)) || (!c.Ready && !startupRulesOwned(rules, family, c.Resources)) {
			return fmt.Errorf("TUN routing rules changed or are not attributable to this instance; preserving claim for administrator inspection")
		}
		routes, err := t.routes(ctx, family, c.Resources)
		if err != nil {
			return err
		}
		recordedRoutes := c.Routes4
		if family == "-6" {
			recordedRoutes = c.Routes6
		}
		if c.Ready && !rulesSubset(routes, recordedRoutes) {
			return fmt.Errorf("TUN routing table changed; preserving claim for administrator inspection")
		}
		for _, raw := range routes {
			var route struct {
				Device string `json:"dev"`
			}
			if json.Unmarshal(raw, &route) != nil || route.Device != c.Resources.Device {
				return fmt.Errorf("TUN routing table contains foreign routes; preserving claim")
			}
		}
		currentRules[family], currentRoutes[family] = rules, routes
	}
	for _, family := range []string{"-4", "-6"} {
		// The range was empty when this invocation reserved it. Before readiness,
		// startup can leave a subset of sing-tun's rules without an interface.
		for _, raw := range currentRules[family] {
			var rule struct {
				Priority int `json:"priority"`
			}
			if err := json.Unmarshal(raw, &rule); err != nil {
				return err
			}
			if _, err := t.command(ctx, family, "rule", "delete", "priority", strconv.Itoa(rule.Priority)); err != nil {
				return err
			}
		}
		if len(currentRoutes[family]) > 0 {
			if _, err := t.command(ctx, family, "route", "flush", "table", strconv.Itoa(c.Resources.Table), "dev", c.Resources.Device); err != nil {
				return err
			}
		}
	}
	if found {
		if _, err := t.command(ctx, "link", "delete", "dev", c.Resources.Device); err != nil {
			return err
		}
	}
	path, _ := t.path()
	return store.Remove(path, old)
}
