package app

import (
	"context"
	"strings"
	"time"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// TestProxyDelay checks live membership and ownership, then lets the core test
// the member outside the app lock so service actions and cancellation can run.
func (a *App) TestProxyDelay(ctx context.Context, group, node string) (mihomo.ProxyDelay, error) {
	ctx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	var before RuntimeSnapshot
	var client mihomo.Client
	var identity, generation string
	err := a.locked(ctx, func() error {
		var err error
		before, client, identity, err = a.inspectRuntimeLocked(ctx)
		if err == nil && before.State == "ready" {
			_, generation, err = a.load()
		}
		return err
	})
	if err != nil {
		return mihomo.ProxyDelay{}, err
	}
	if before.State != "ready" {
		return mihomo.ProxyDelay{}, proxyRuntimeError(before)
	}
	proxies, err := client.Proxies(ctx)
	if err != nil {
		return mihomo.ProxyDelay{}, err
	}
	if err := testableProxy(proxies, group, node); err != nil {
		return mihomo.ProxyDelay{}, err
	}
	result, queryErr := client.TestProxyDelay(ctx, node)
	if ctx.Err() != nil {
		return mihomo.ProxyDelay{}, ctx.Err()
	}
	err = a.locked(ctx, func() error {
		current, _, currentID, err := a.inspectRuntimeLocked(ctx)
		if err != nil {
			return err
		}
		if current.State != "ready" {
			return proxyRuntimeError(current)
		}
		_, currentGeneration, err := a.load()
		if err != nil {
			return err
		}
		if currentID != identity || generation != currentGeneration || before.Service.PID != current.Service.PID {
			return diagnostic.New("diagnostic.runtime.delayCoreChanged", "core changed during the delay test; refresh and retry")
		}
		return queryErr
	})
	if err != nil {
		return mihomo.ProxyDelay{}, err
	}
	return result, nil
}

func proxyRuntimeError(s RuntimeSnapshot) error {
	if s.State == "stopped" {
		return diagnostic.New("diagnostic.runtime.delayNotRunning", "Mihomo is not running; start it before testing proxy delay")
	}
	return runtimeError(s)
}

func testableProxy(proxies map[string]mihomo.Proxy, group, node string) error {
	g, exists := proxies[group]
	member := false
	for _, name := range g.All {
		member = member || name == node
	}
	if !exists || !g.IsGroup() || !member {
		return diagnostic.New("diagnostic.api.nodeUnavailable", "the group or node is no longer available; refresh the nodes page")
	}
	seen := make(map[string]bool)
	for {
		p, exists := proxies[node]
		if !exists || seen[node] {
			return diagnostic.New("diagnostic.api.nodeUnavailable", "the group or node is no longer available; refresh the nodes page")
		}
		seen[node] = true
		switch strings.ToLower(p.Type) {
		case "direct", "reject", "rejectdrop", "loadbalance", "relay":
			return diagnostic.New("diagnostic.api.delayUnsupported", "this proxy does not have a single testable outbound route")
		}
		if !p.IsGroup() {
			return nil
		}
		selectedMember := false
		for _, member := range p.All {
			selectedMember = selectedMember || member == p.Now
		}
		if p.Now == "" || !selectedMember {
			return diagnostic.New("diagnostic.api.nodeUnavailable", "the group or node is no longer available; refresh the nodes page")
		}
		node = p.Now
	}
}
