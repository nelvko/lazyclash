package app

import "github.com/nelvko/lazyclash/internal/service"

type Capability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

type Capabilities struct {
	Scope       string     `json:"scope"`
	Runtime     Capability `json:"runtime"`
	TUN         Capability `json:"tun"`
	SystemProxy Capability `json:"system_proxy"`
}

// Capabilities reports the selected service scope. Only the system unit can
// receive the bounded capabilities needed for explicitly enabled host TUN.
func (a *App) Capabilities() Capabilities {
	c := Capabilities{
		Scope:       "user",
		Runtime:     Capability{Supported: a.Service != nil},
		TUN:         Capability{Reason: "TUN controls host traffic and requires a root-managed system service; the user service supports ordinary HTTP/SOCKS proxying"},
		SystemProxy: Capability{Reason: "automatic system proxy changes are not implemented; lazyclash env explicitly prints shell proxy variables"},
	}
	if s, ok := a.Service.(interface{ ServiceScope() service.Scope }); ok && s.ServiceScope() == service.System {
		c.Scope = "system"
		c.TUN = Capability{Supported: true}
	}
	if a.Service == nil {
		c.Runtime.Reason = "Linux with an available systemd service manager is required"
		if a.PlatformError != nil {
			c.Runtime.Reason = a.PlatformError.Error()
		}
	}
	return c
}
