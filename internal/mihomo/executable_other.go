//go:build !linux

package mihomo

// Other hosts are used for portable tests and builds, not runtime management.
// Linux file capabilities have no meaning on these hosts.
func checkFileCapabilities(string) error { return nil }
