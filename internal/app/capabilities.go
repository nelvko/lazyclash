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

// Capabilities describes the implementation's actual privilege boundary. Device
// access alone cannot make TUN available: the managed user unit deliberately has
// NoNewPrivileges and receives no network administration capabilities.
func (a *App) Capabilities() Capabilities {
	c := Capabilities{
		Scope:       "user",
		Runtime:     Capability{Supported: a.Service != nil},
		TUN:         Capability{Reason: "TUN requires a separately authorized privileged system service; this release only manages an unprivileged systemd user service"},
		SystemProxy: Capability{Reason: "automatic system proxy changes are not implemented; lazyclash env explicitly prints shell proxy variables"},
	}
	if s, ok := a.Service.(*service.Systemd); ok && s.Scope == service.System {
		c.Scope = "system"
	}
	if a.Service == nil {
		c.Runtime.Reason = "Linux with an available systemd user manager is required"
		if a.PlatformError != nil {
			c.Runtime.Reason = a.PlatformError.Error()
		}
	}
	return c
}
