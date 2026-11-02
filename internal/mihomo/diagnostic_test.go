package mihomo

import (
	"errors"
	"testing"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestConfigurationDiagnosticHasStableIdentityAndLocalizedPresentation(t *testing.T) {
	_, err := Effective([]byte(""), "127.0.0.1:9097", "test", 7897)
	var message *diagnostic.Error
	if !errors.As(err, &message) || message.Code != "diagnostic.config.yamlSize" {
		t.Fatalf("configuration diagnostic identity lost: %v", err)
	}
	if got := i18n.New(i18n.Chinese).Error(err); got != "Mihomo YAML 不能为空，且不得超过 8 MiB" {
		t.Fatal(got)
	}
	if err.Error() != "Mihomo YAML must be nonempty and at most 8 MiB" {
		t.Fatal("machine diagnostic changed")
	}
}
