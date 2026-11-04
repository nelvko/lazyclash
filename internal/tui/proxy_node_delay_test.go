package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/nelvko/lazyclash/internal/i18n"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

type proxyNodeDelayFixture struct {
	*proxiesPTYBackend
	calls []proxyTestTarget
}

func (b *proxyNodeDelayFixture) TestProxyDelay(ctx context.Context, group, node string) (mihomo.ProxyDelay, error) {
	b.calls = append(b.calls, proxyTestTarget{group: group, node: node})
	return mihomo.ProxyDelay{State: "success", Delay: 57, Time: time.Now(), URL: mihomo.DefaultProxyDelayURL}, ctx.Err()
}

func proxyNodeTestMessage(t *testing.T, m Model, group, node string, delay int) proxyNodeDelayMsg {
	t.Helper()
	request, exists := m.proxies.nodeTests.running[proxyResultKey(group, node)]
	if !exists {
		t.Fatalf("node test is not running: %s/%s", group, node)
	}
	return proxyNodeDelayMsg{generation: request.generation, target: request.target, result: mihomo.ProxyDelay{State: "success", Delay: delay, Time: time.Now(), URL: mihomo.DefaultProxyDelayURL}}
}

func runProxyNodeDelayCommand(t *testing.T, cmd tea.Cmd) proxyNodeDelayMsg {
	t.Helper()
	var run func(tea.Cmd) (proxyNodeDelayMsg, bool)
	run = func(command tea.Cmd) (proxyNodeDelayMsg, bool) {
		if command == nil {
			return proxyNodeDelayMsg{}, false
		}
		switch message := command().(type) {
		case proxyNodeDelayMsg:
			return message, true
		case tea.BatchMsg:
			for _, child := range message {
				if result, exists := run(child); exists {
					return result, true
				}
			}
		}
		return proxyNodeDelayMsg{}, false
	}
	if message, exists := run(cmd); exists {
		return message
	}
	t.Fatal("node measurement did not return its result message")
	return proxyNodeDelayMsg{}
}

func TestProxiesNodeDelayClickTestsOnlyItsNodeWithoutSelecting(t *testing.T) {
	for _, group := range []string{"PROXY", "AUTO"} {
		t.Run(group, func(t *testing.T) {
			m, original := proxiesModel(t, 160, 50)
			backend := &proxyNodeDelayFixture{proxiesPTYBackend: original}
			m.backend = backend
			setProxyTestGroup(&m, group)
			gi, selected, _ := m.proxyGroup(group)
			ni := indexOf(selected.All, "HK 01")
			m.focus = fmt.Sprintf("group.%d", gi)
			m.sortProxies("delay")
			beforeOrder := m.proxyMembers(selected)
			m.layout()
			beforeExpansion := cloneProxyExpansion(m.proxies.expanded)
			id := fmt.Sprintf("proxies.delay.%d.%d", gi, ni)
			m, cmd := click(m, findTarget(t, m, id))
			if cmd == nil || m.busy || m.proxies.detail != "" || m.proxies.testing || m.proxies.pendingNode != "" || backend.selections != 0 || m.focus != id || !reflect.DeepEqual(m.proxies.expanded, beforeExpansion) {
				t.Fatalf("delay click selected a node or changed the group: focus=%s pending=%s detail=%s expanded=%+v", m.focus, m.proxies.pendingNode, m.proxies.detail, m.proxies.expanded)
			}
			defer m.stopProxyTests()
			if len(m.proxies.nodeTests.running) != 1 || m.proxyNodeTestState(group, "HK 01") != "testing" {
				t.Fatal("delay click did not start exactly one node measurement")
			}
			m, _ = update(m, runProxyNodeDelayCommand(t, cmd))
			if len(backend.calls) != 1 || backend.calls[0].group != group || backend.calls[0].node != "HK 01" || backend.selections != 0 {
				t.Fatalf("delay click called another backend operation: calls=%+v selections=%d", backend.calls, backend.selections)
			}
			if result := m.proxyDelay(group, "HK 01").result; result.State != "success" || result.Delay != 57 || m.proxyNodeTestState(group, "HK 01") != "" {
				t.Fatalf("single-node completion did not update its result: %+v", result)
			}
			_, observed, _ := m.proxyGroup(group)
			if observed.Now != selected.Now || m.focus != id || !reflect.DeepEqual(m.proxyMembers(observed), beforeOrder) || !reflect.DeepEqual(m.proxies.expanded, beforeExpansion) {
				t.Fatalf("single-node completion moved the selection, order or location: group=%+v focus=%s", observed, m.focus)
			}
		})
	}
}

func TestProxiesNodeDelaySlotHasFixedWidthAndCenteredValues(t *testing.T) {
	for _, language := range []string{i18n.English, i18n.Chinese} {
		for _, state := range []string{"untested", "success", "timeout", "failed", "testing", "queued"} {
			t.Run(language+"/"+state, func(t *testing.T) {
				m, _ := proxiesModel(t, 160, 40)
				m.locale = i18n.New(language)
				proxy := m.snapshot.Proxies["HK 01"]
				proxy.History = nil
				m.snapshot.Proxies["HK 01"] = proxy
				expected := m.tr("proxies.not_tested")
				switch state {
				case "success", "timeout", "failed":
					m.proxies.results[proxyResultKey("PROXY", "HK 01")] = proxyMeasurement{result: mihomo.ProxyDelay{State: state, Delay: 57}, path: m.proxyPathKey("HK 01")}
					if state == "success" {
						expected = "57 ms"
					} else if state == "timeout" {
						expected = m.tr("proxies.timeout")
					} else {
						expected = m.tr("proxies.test_failed")
					}
				case "testing":
					m.testProxyNode("PROXY", "HK 01")
					expected = m.tr("proxies.testing")
				case "queued":
					for _, node := range []string{"AUTO", "HK 02", "JP 01", "US 01", "HK 01"} {
						m.testProxyNode("PROXY", node)
					}
					expected = m.tr("proxies.queued")
				}
				defer m.stopProxyTests()
				for _, width := range []int{36, 50, 72} {
					card := m.proxyNode(0, 1, width)
					var slot hitRegion
					for _, hit := range card.targets {
						if hit.ID == "proxies.delay.0.1" {
							slot = hit
						}
					}
					if slot.Y != 1 || slot.H != 1 || slot.W != 12 || slot.X+slot.W != width-2 {
						t.Fatalf("delay slot moved with its value: width=%d slot=%+v", width, slot)
					}
					line := ansi.Strip(card.lines[1])
					value := ansi.Cut(line, slot.X, slot.X+slot.W)
					left := ansi.StringWidth(value) - ansi.StringWidth(strings.TrimLeft(value, " "))
					right := ansi.StringWidth(value) - ansi.StringWidth(strings.TrimRight(value, " "))
					if strings.TrimSpace(value) != expected || max(left, right)-min(left, right) > 1 || !strings.HasSuffix(line, "  ") {
						t.Fatalf("delay value is not centered within its slot: value=%q expected=%q padding=%d/%d", value, expected, left, right)
					}
				}
			})
		}
	}
}

func TestProxiesNodeDelayDisabledSlotsAreInertAndKeepOuterPadding(t *testing.T) {
	for _, state := range []string{"direct", "reject", "unsupported", "busy", "quitting", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			m, backend := proxiesModel(t, 160, 40)
			ni := 1
			switch state {
			case "direct":
				ni = indexOf(m.snapshot.Groups[0].All, "DIRECT")
			case "reject":
				ni = indexOf(m.snapshot.Groups[0].All, "REJECT")
			case "unsupported":
				m.backend = backend.fakeBackend
			case "busy":
				m.busy = true
			case "quitting":
				m.quitting = true
			case "unavailable":
				m.snapshot.Service.Active = false
			}
			const width = 50
			card := m.proxyNode(0, ni, width)
			for x := width - 14; x < width; x++ {
				hitID := ""
				for _, hit := range card.targets {
					if hit.Y <= 1 && hit.Y+hit.H > 1 && hit.X <= x && hit.X+hit.W > x {
						hitID = hit.ID
					}
				}
				if x < width-2 && hitID != "" || x >= width-2 && hitID != fmt.Sprintf("node.0.%d", ni) {
					t.Fatalf("disabled slot captures selection or swallows outer padding: state=%s x=%d id=%s", state, x, hitID)
				}
			}
			if cmd := m.testProxyNode("PROXY", m.snapshot.Groups[0].All[ni]); cmd != nil || len(m.proxies.nodeTests.running) != 0 {
				t.Fatal("disabled delay slot started a measurement")
			}
		})
	}
}

func TestProxiesNodeDelayRepeatedClicksReusePendingRequest(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.testProxyNode("PROXY", "HK 01")
	defer m.stopProxyTests()
	first := proxyNodeTestMessage(t, m, "PROXY", "HK 01", 57)
	m.testProxyNode("PROXY", "HK 01")
	if len(m.proxies.nodeTests.running) != 1 || len(m.proxies.nodeTests.queue) != 0 || proxyNodeTestMessage(t, m, "PROXY", "HK 01", 57).generation != first.generation {
		t.Fatal("repeated clicks duplicated or replaced the pending request")
	}
	m, _ = update(m, first)
	m.testProxyNode("PROXY", "HK 01")
	second := proxyNodeTestMessage(t, m, "PROXY", "HK 01", 89)
	if second.generation <= first.generation {
		t.Fatal("a completed node could not start a new measurement")
	}
	m, _ = update(m, first)
	if m.proxyNodeTestState("PROXY", "HK 01") != "testing" || m.proxyDelay("PROXY", "HK 01").result.Delay != 57 {
		t.Fatal("the previous request completed or overwrote the newer one")
	}
	m, _ = update(m, second)
	if m.proxyNodeTestState("PROXY", "HK 01") != "" || m.proxyDelay("PROXY", "HK 01").result.Delay != 89 {
		t.Fatal("the newest request did not publish its measurement")
	}
}

func TestProxiesNodeDelayQueueIsBoundedAndDuplicateFree(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	for _, node := range []string{"AUTO", "HK 01", "HK 02", "JP 01", "SG 01", "US 01", "US 01"} {
		m.testProxyNode("PROXY", node)
	}
	defer m.stopProxyTests()
	if len(m.proxies.nodeTests.running) != 4 || len(m.proxies.nodeTests.queue) != 2 || m.proxyNodeTestState("PROXY", "US 01") != "queued" {
		t.Fatalf("node tests exceeded their concurrency or duplicated a queue entry: %+v", m.proxies.nodeTests)
	}
	completed := 0
	for len(m.proxies.nodeTests.running) > 0 {
		for _, request := range m.proxies.nodeTests.running {
			m, _ = update(m, proxyNodeTestMessage(t, m, request.target.group, request.target.node, 57))
			completed++
			break
		}
		if len(m.proxies.nodeTests.running) > 4 {
			t.Fatal("draining the node queue exceeded four concurrent requests")
		}
	}
	if completed != 6 || len(m.proxies.nodeTests.queue) != 0 || len(m.proxies.results) != 6 {
		t.Fatalf("node queue lost or duplicated work: completed=%d results=%d", completed, len(m.proxies.results))
	}
}

func TestProxiesNodeDelayReusesGroupWorkAndPrioritizesQueuedNode(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.testProxyGroup()
	defer m.stopProxyTests()
	groupGeneration := m.proxies.generation
	m.testProxyNode("PROXY", "HK 01")
	m.testProxyNode("PROXY", "US 01")
	if m.proxies.generation != groupGeneration || len(m.proxies.nodeTests.running) != 0 || len(m.proxies.nodeTests.queue) != 0 || len(m.proxies.queue) != 2 || m.proxies.queue[0].node != "US 01" {
		t.Fatalf("node click duplicated existing group work or ignored queue priority: groupQueue=%+v nodeQueue=%+v", m.proxies.queue, m.proxies.nodeTests.queue)
	}
}

func TestProxiesNodeDelayInvalidationImmediatelyReleasesAQueueSlot(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	for _, node := range []string{"AUTO", "HK 01", "HK 02", "JP 01", "SG 01", "US 01"} {
		m.testProxyNode("PROXY", node)
	}
	defer m.stopProxyTests()
	late := proxyNodeTestMessage(t, m, "PROXY", "AUTO", 777)
	m.snapshot.Groups[1].Now = "HK 01"
	if cmd := m.syncProxies(); cmd == nil || len(m.proxies.nodeTests.running) != 4 || len(m.proxies.nodeTests.queue) != 1 || m.proxyNodeTestState("PROXY", "SG 01") != "testing" || m.proxyNodeTestState("PROXY", "US 01") != "queued" {
		t.Fatal("invalidating a running reference left its queue slot idle")
	}
	m, _ = update(m, late)
	if _, exists := m.proxies.results[proxyResultKey("PROXY", "AUTO")]; exists {
		t.Fatal("an invalidated reference published its old path's result")
	}
}

func TestProxiesNodeDelayGroupStartAndCancelPreserveOtherGroups(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.testProxyNode("PROXY", "HK 01")
	m.testProxyNode("AUTO", "HK 02")
	defer m.stopProxyTests()
	obsolete := proxyNodeTestMessage(t, m, "PROXY", "HK 01", 777)
	other := proxyNodeTestMessage(t, m, "AUTO", "HK 02", 57)
	m.focus = "group.0"
	m.testProxyGroup()
	if m.proxyNodeTestState("PROXY", "HK 01") != "" || m.proxyNodeTestState("AUTO", "HK 02") != "testing" || !m.proxies.testing {
		t.Fatal("starting a group test retained duplicate work or canceled another group's node")
	}
	m.testProxyGroup()
	if m.proxies.testing || m.proxyNodeTestState("AUTO", "HK 02") != "testing" {
		t.Fatal("canceling the group also canceled an independent node test")
	}
	m, _ = update(m, obsolete)
	if _, exists := m.proxies.results[proxyResultKey("PROXY", "HK 01")]; exists {
		t.Fatal("the superseded single-node request overwrote group work")
	}
	m, _ = update(m, other)
	if m.proxyDelay("AUTO", "HK 02").result.Delay != 57 {
		t.Fatal("the other group's independent request could no longer finish")
	}
}

func TestProxiesNodeDelayChangedGroupPathStartsFreshSingleRequest(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.testProxyGroup()
	defer m.stopProxyTests()
	obsolete := proxyTestMessage(m, "PROXY", "AUTO", 777)
	m.snapshot.Groups[1].Now = "HK 01"
	if cmd := m.testProxyNode("PROXY", "AUTO"); cmd == nil || m.proxyNodeTestState("PROXY", "AUTO") != "testing" {
		t.Fatal("an old group request blocked testing the reference's new path")
	}
	current := proxyNodeTestMessage(t, m, "PROXY", "AUTO", 57)
	m, _ = update(m, current)
	if label, _ := m.proxyDelayLabel("PROXY", "AUTO"); label != "57 ms" || !m.proxies.inflight[proxyResultKey("PROXY", "AUTO")] {
		t.Fatalf("old group activity concealed the new path's measurement: %q", label)
	}
	m, _ = update(m, obsolete)
	if label, _ := m.proxyDelayLabel("PROXY", "AUTO"); label != "57 ms" || m.proxyDelay("PROXY", "AUTO").result.Delay != 57 {
		t.Fatalf("an old group result replaced the new path's measurement: %q", label)
	}
}

func TestProxiesNodeDelayOldQueuedPathDoesNotHideNewMeasurement(t *testing.T) {
	m, _ := proxiesModel(t, 160, 40)
	m.snapshot.Groups[0].All = []string{"HK 01", "HK 02", "JP 01", "SG 01", "AUTO", "US 01", "DIRECT", "REJECT"}
	m.testProxyGroup()
	defer m.stopProxyTests()
	m.snapshot.Groups[1].Now = "HK 01"
	m.proxies.results[proxyResultKey("PROXY", "AUTO")] = proxyMeasurement{result: mihomo.ProxyDelay{State: "success", Delay: 57}, path: m.proxyPathKey("AUTO")}
	if label, _ := m.proxyDelayLabel("PROXY", "AUTO"); label != "57 ms" || len(m.proxies.queue) != 2 {
		t.Fatalf("an old queued path concealed the current path's measurement: label=%q queue=%+v", label, m.proxies.queue)
	}
}

func TestProxiesNodeDelayLateResultsCannotSurviveInvalidation(t *testing.T) {
	for _, change := range []string{"source", "mode", "path", "member", "page", "busy", "quit"} {
		t.Run(change, func(t *testing.T) {
			m, _ := proxiesModel(t, 160, 40)
			m.testProxyNode("PROXY", "AUTO")
			late := proxyNodeTestMessage(t, m, "PROXY", "AUTO", 777)
			switch change {
			case "source":
				m.snapshot.Generation += "-new"
			case "mode":
				m.snapshot.Mode.Actual = "global"
			case "path":
				m.snapshot.Groups[1].Now = "HK 01"
			case "member":
				m.snapshot.Groups[0].All = m.snapshot.Groups[0].All[1:]
			case "page":
				m.page = "overview"
			case "busy":
				m.busy = true
			case "quit":
				m.quitting = true
			}
			m.syncProxies()
			if len(m.proxies.nodeTests.running) != 0 || len(m.proxies.nodeTests.queue) != 0 {
				t.Fatalf("%s change retained stale node work", change)
			}
			m, _ = update(m, late)
			if _, exists := m.proxies.results[proxyResultKey("PROXY", "AUTO")]; exists {
				t.Fatalf("%s change accepted an obsolete node measurement", change)
			}
		})
	}
}

func TestProxiesNodeDelayFocusFollowsNodeIdentityAndOwnsItsCard(t *testing.T) {
	m, _ := proxiesModel(t, 160, 50)
	m.focus = "proxies.delay.0.1"
	m.layout()
	card := m.proxyNode(0, 1, 50)
	if !strings.HasPrefix(ansi.Strip(card.lines[1]), "  › HK 01") || m.snapshot.Groups[0].Now != "AUTO" {
		t.Fatal("focusing the delay lost its card focus or changed selection")
	}
	m.snapshot.Groups[0], m.snapshot.Groups[2] = m.snapshot.Groups[2], m.snapshot.Groups[0]
	m.snapshot.Groups[2].All[1], m.snapshot.Groups[2].All[2] = m.snapshot.Groups[2].All[2], m.snapshot.Groups[2].All[1]
	m.layout()
	if m.focus != "proxies.delay.2.2" || !hasAction(m.targets, m.focus) || m.pageViews["proxies"].group != "PROXY" || m.pageViews["proxies"].node != "HK 01" {
		t.Fatalf("delay focus followed a stale group or member index: focus=%s state=%+v", m.focus, m.pageViews["proxies"])
	}
	m.busy = true
	m.layout()
	if m.focus != "node.2.2" || !hasAction(m.targets, m.focus) {
		t.Fatalf("a disabled delay action lost its card fallback: focus=%s", m.focus)
	}
}

func TestProxiesNodeDelaySharesItsCardsTabStopAndEnterAction(t *testing.T) {
	m, backend := proxiesModel(t, 160, 40)
	m.focus = "node.0.1"
	m.layout()
	before := len(m.focusTargets())
	m.focus = "proxies.delay.0.1"
	m.layout()
	stops, delayStops := m.focusTargets(), 0
	for _, stop := range stops {
		if strings.HasPrefix(stop.ID, "proxies.delay.") {
			delayStops++
			if stop.ID != m.focus {
				t.Fatalf("a different card's delay gained a Tab stop: %+v", stop)
			}
		}
	}
	if len(stops) != before || delayStops != 1 {
		t.Fatalf("delay added per-node Tab stops: before=%d after=%d delayStops=%d", before, len(stops), delayStops)
	}
	m, cmd := update(m, special(tea.KeyEnter))
	defer m.stopProxyTests()
	if cmd == nil || m.proxyNodeTestState("PROXY", "HK 01") != "testing" || m.busy || backend.selections != 0 || m.snapshot.Groups[0].Now != "AUTO" {
		t.Fatal("Enter on a focused delay selected its node or failed to test it")
	}
}
