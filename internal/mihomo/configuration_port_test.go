package mihomo

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigurationDistinguishesUnreportedDisabledAndActiveMixedPort(t *testing.T) {
	for _, tc := range []struct {
		field   string
		present bool
		port    int
		invalid bool
	}{
		{"", false, 0, false}, {`,"mixed-port":0`, true, 0, false}, {`,"mixed-port":17890`, true, 17890, false}, {`,"mixed-port":65536`, true, 0, true}, {`,"mixed-port":-1`, true, 0, true}, {`,"mixed-port":"7897"`, true, 0, true},
	} {
		t.Run(tc.field, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"mode":"rule","tun":{"enable":false}%s}`, tc.field)
			}))
			defer s.Close()
			cfg, err := (Client{BaseURL: s.URL}).Configuration(t.Context())
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid port accepted")
				}
				return
			}
			if err != nil || (cfg.MixedPort != nil) != tc.present {
				t.Fatal(cfg, err)
			}
			if tc.present && *cfg.MixedPort != tc.port {
				t.Fatal(*cfg.MixedPort)
			}
		})
	}
}
