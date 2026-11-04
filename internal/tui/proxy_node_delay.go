package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// Individual retries have their own bounded queue so they neither replace a
// group's progress nor cancel work for another group.
type proxyNodeTests struct {
	generation uint64
	queue      []proxyTestTarget
	running    map[string]proxyNodeRequest
}

type proxyNodeRequest struct {
	generation uint64
	target     proxyTestTarget
	cancel     context.CancelFunc
}

type proxyNodeDelayMsg struct {
	generation uint64
	target     proxyTestTarget
	result     mihomo.ProxyDelay
	err        error
}

func (m Model) proxyNodeTestState(group, node string) string {
	key, path := proxyResultKey(group, node), m.proxyPathKey(node)
	if request, ok := m.proxies.nodeTests.running[key]; ok && request.target.path == path {
		return "testing"
	}
	for _, target := range m.proxies.nodeTests.queue {
		if target.group == group && target.node == node && target.path == path {
			return "queued"
		}
	}
	return ""
}

func (m Model) proxyNodeTargetValid(target proxyTestTarget) bool {
	_, group, exists := m.proxyGroup(target.group)
	return exists && target.node != "" && indexOf(group.All, target.node) >= 0 && m.proxyTestable(target.node) && target.path == m.proxyPathKey(target.node)
}

func (m *Model) testProxyNode(group, node string) tea.Cmd {
	if _, supported := m.backend.(proxyDelayBackend); !supported || m.page != "proxies" || !m.proxiesReady() || m.busy || m.quitting {
		return nil
	}
	target := proxyTestTarget{group, node, m.proxyPathKey(node)}
	if !m.proxyNodeTargetValid(target) {
		return nil
	}
	key := proxyResultKey(group, node)
	if m.proxies.testing && m.proxies.testGroup == group {
		if m.proxies.inflight[key] && m.proxies.inflightPaths[key] == target.path {
			return nil
		}
		for i, queued := range m.proxies.queue {
			if queued.group == group && queued.node == node {
				// Reuse the group run, giving this explicit retry the next slot.
				copy(m.proxies.queue[1:i+1], m.proxies.queue[:i])
				m.proxies.queue[0] = target
				return nil
			}
		}
	}
	if m.proxyNodeTestState(group, node) != "" {
		return nil
	}
	// A changed reference path replaces its obsolete in-flight retry.
	if previous, exists := m.proxies.nodeTests.running[key]; exists {
		previous.cancel()
		delete(m.proxies.nodeTests.running, key)
	}
	m.pruneProxyNodeTests()
	m.proxies.nodeTests.queue = append(m.proxies.nodeTests.queue, target)
	return m.nextProxyNodeTests()
}

func (m *Model) nextProxyNodeTests() tea.Cmd {
	backend, supported := m.backend.(proxyDelayBackend)
	if !supported || m.page != "proxies" || !m.proxiesReady() || m.busy || m.quitting {
		return nil
	}
	n := &m.proxies.nodeTests
	var commands []tea.Cmd
	for len(n.queue) > 0 && len(n.running) < 4 {
		target := n.queue[0]
		n.queue = n.queue[1:]
		if !m.proxyNodeTargetValid(target) {
			continue
		}
		if n.running == nil {
			n.running = make(map[string]proxyNodeRequest)
		}
		ctx, cancel := context.WithCancel(m.ctx)
		n.generation++
		generation := n.generation
		n.running[proxyResultKey(target.group, target.node)] = proxyNodeRequest{generation, target, cancel}
		commands = append(commands, func() tea.Msg {
			result, err := backend.TestProxyDelay(ctx, target.group, target.node)
			return proxyNodeDelayMsg{generation, target, result, err}
		})
	}
	return tea.Batch(commands...)
}

func (m *Model) acceptProxyNodeDelay(msg proxyNodeDelayMsg) tea.Cmd {
	key := proxyResultKey(msg.target.group, msg.target.node)
	request, exists := m.proxies.nodeTests.running[key]
	if !exists || request.generation != msg.generation {
		return nil
	}
	request.cancel()
	delete(m.proxies.nodeTests.running, key)
	if m.page != "proxies" || !m.proxiesReady() || m.busy || m.quitting {
		m.stopProxyNodeTests("")
		return nil
	}
	if !errors.Is(msg.err, context.Canceled) && m.proxyNodeTargetValid(msg.target) {
		if msg.err != nil {
			msg.result = mihomo.ProxyDelay{State: "failed", Time: time.Now(), URL: mihomo.DefaultProxyDelayURL}
		}
		m.proxies.results[key] = proxyMeasurement{msg.result, msg.target.path, msg.err}
	}
	return m.nextProxyNodeTests()
}

func (m *Model) stopProxyNodeTests(group string) {
	n := &m.proxies.nodeTests
	for key, request := range n.running {
		if group == "" || request.target.group == group {
			request.cancel()
			delete(n.running, key)
		}
	}
	queue := n.queue[:0]
	for _, target := range n.queue {
		if group != "" && target.group != group {
			queue = append(queue, target)
		}
	}
	n.queue = queue
}

func (m *Model) pruneProxyNodeTests() {
	n := &m.proxies.nodeTests
	for key, request := range n.running {
		if !m.proxyNodeTargetValid(request.target) {
			request.cancel()
			delete(n.running, key)
		}
	}
	queue := n.queue[:0]
	for _, target := range n.queue {
		if m.proxyNodeTargetValid(target) {
			queue = append(queue, target)
		}
	}
	n.queue = queue
}
