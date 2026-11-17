package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/nelvko/lazyclash/internal/mihomo"
)

// Match fields independently, just like rules, so anchors describe a field
// and an expression cannot accidentally span unrelated connection metadata.
func (m Model) connectionMatcher() (func(mihomo.Connection) bool, error) {
	match, err := compileSearchMatcher(m.connections.search.Value(), m.connections.caseSensitive, m.connections.wholeWord, m.connections.regex)
	if err != nil {
		return nil, err
	}
	return func(c mihomo.Connection) bool {
		md := c.Metadata
		for _, value := range []string{c.ID, md.Host, md.SourceIP, md.SourcePort, md.DestinationIP, md.DestinationPort,
			md.Network, md.Type, md.Process, md.ProcessPath, c.Rule, c.RulePayload} {
			if match(value) {
				return true
			}
		}
		for _, chain := range c.Chains {
			if match(chain) {
				return true
			}
		}
		return false
	}, nil
}

func (m Model) connectionFilterButtons() dashboardBlock {
	return m.searchMatchingButtons("conn", m.connections.caseSensitive, m.connections.wholeWord, m.connections.regex)
}

func (m *Model) toggleConnectionFilter(id string) {
	switch id {
	case "conn.case":
		m.connections.caseSensitive = !m.connections.caseSensitive
	case "conn.word":
		m.connections.wholeWord = !m.connections.wholeWord
	case "conn.regex":
		m.connections.regex = !m.connections.regex
	}
	m.scroll = 0
	m.reconcileConnections()
}

func (m *Model) connectionFilterKey(k tea.KeyPressMsg) bool {
	if m.busy || m.quitting || m.connections.confirming || m.connections.detail != nil {
		return false
	}
	id := map[string]string{"alt+c": "conn.case", "alt+w": "conn.word", "alt+r": "conn.regex"}[k.String()]
	if id == "" {
		return false
	}
	m.toggleConnectionFilter(id)
	return true
}
