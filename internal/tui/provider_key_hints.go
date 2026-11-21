package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m *Model) providersContextHelp() string {
	if m.providers.editing {
		return m.tr("navigation.providers.searching")
	}
	var hints []string
	label := ""
	switch m.focus {
	case "providers.search":
		label = m.tr("navigation.providers.searchAction")
	case "providers.clear":
		label = m.tr("navigation.providers.clearAction")
	case "providers.back":
		label = m.tr("providers.close")
	case "providers.reload":
		if m.providersSupported(m.providers.kind) && m.providers.active && m.providersVisible() {
			label = m.tr("providers.reload")
		}
	case "providers.all":
		if m.providersCanUpdateAll() {
			label = m.tr("providers.update_all")
		}
	default:
		if strings.HasPrefix(m.focus, "provider.update.") {
			if provider, ok := m.providerByName(strings.TrimPrefix(m.focus, "provider.update.")); ok {
				if value, enabled := m.providerUpdateLabel(provider); enabled {
					label = value
				}
			}
		}
	}
	if label != "" {
		hints = append(hints, m.tr("navigation.providers.activate", label))
	}
	if provider, ok := m.providerByName(m.providers.selected); ok {
		if label, enabled := m.providerUpdateLabel(provider); enabled {
			hints = append(hints, m.tr("navigation.providers.updateKey", label))
		}
	}
	hints = append(hints, m.tr("navigation.providers.browse"))
	if m.providersSupported(m.providers.kind) && m.providers.active && m.providersVisible() {
		hints = append(hints, m.tr("navigation.providers.reloadKey"))
	}
	if len(m.filteredProviders()) > 0 {
		hints = append(hints, m.tr("navigation.providers.rows"))
	}
	return strings.Join(hints, " · ")
}

func (m *Model) providersFooterHelp(width int) string {
	var hints []string
	for _, hint := range strings.Split(m.providersContextHelp(), " · ") {
		candidate := strings.Join(append(hints, hint), " · ")
		if ansi.StringWidth(candidate) <= width {
			hints = append(hints, hint)
		}
	}
	return strings.Join(hints, " · ")
}
