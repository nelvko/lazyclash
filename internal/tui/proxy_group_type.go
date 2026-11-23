package tui

// Mihomo's control API and YAML configuration use different names for these types.
func proxyGroupConfigType(apiType string) string {
	switch apiType {
	case "Selector":
		return "select"
	case "URLTest":
		return "url-test"
	case "Fallback":
		return "fallback"
	case "LoadBalance":
		return "load-balance"
	case "Relay":
		return "relay"
	}
	return ""
}

func (m *Model) proxyGroupTypeLabel(apiType string) string {
	if kind := proxyGroupConfigType(apiType); kind != "" {
		return m.tr("proxies.type." + kind)
	}
	if name := clean(apiType); name != "" {
		return name
	}
	return m.tr("proxies.unknown_type")
}
