package app

import (
	"testing"

	"github.com/nelvko/lazyclash/internal/service"
)

func TestCapabilitiesFollowManagedServiceScope(t *testing.T) {
	for _, scope := range []service.Scope{service.User, service.System} {
		t.Run(string(scope), func(t *testing.T) {
			a := &App{Service: &service.Systemd{Scope: scope}}
			c := a.Capabilities()
			if c.Scope != string(scope) || !c.Runtime.Supported || c.TUN.Supported != (scope == service.System) || c.SystemProxy.Supported {
				t.Fatalf("unexpected scope capabilities: %+v", c)
			}
			if scope == service.User && c.TUN.Reason == "" {
				t.Fatal("user scope omitted the host TUN boundary")
			}
		})
	}
}
