package downloadnet

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"

	"github.com/nelvko/lazyclash/internal/diagnostic"
	"github.com/nelvko/lazyclash/internal/i18n"
)

func TestNetworkDiagnosticTranslatesPublicFieldsWithoutTransportCredentials(t *testing.T) {
	transport := &url.Error{Op: "Get", URL: "https://user:private-token@example.invalid/private-path?token=private-token", Err: &net.DNSError{Err: "lookup private-token", Name: "private-token.example.invalid"}}
	err := Failure(t.Context(), "official version query", transport)
	var safe *Error
	if !errors.As(err, &safe) || safe.Kind != "DNS lookup" {
		t.Fatalf("classification lost: %v", err)
	}
	var message *diagnostic.Error
	if !errors.As(safe.Diagnostic(), &message) || message.Code != "diagnostic.network.failure" {
		t.Fatal("message identity missing")
	}
	localized := i18n.New(i18n.Chinese).Error(err)
	if localized != "官方版本查询失败：DNS 查询" {
		t.Fatal(localized)
	}
	if strings.Contains(localized+safe.Diagnostic().Error(), "private-token") {
		t.Fatal("credential leaked through localized diagnostic")
	}
	if safe.Unwrap() != nil {
		t.Fatal("sensitive transport cause retained")
	}
	if i18n.New(i18n.English).Error(err) != "official version query failed: DNS lookup" {
		t.Fatal("English diagnostic changed")
	}
}

func TestNetworkDiagnosticPreservesHTTPAndCancellationSemantics(t *testing.T) {
	err := HTTPError("release-file download", 407)
	if got := i18n.New(i18n.Chinese).Error(err); got != "发布文件下载失败：HTTP 407（代理认证）" {
		t.Fatal(got)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = Failure(ctx, "official version query", errors.New("private-token"))
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation sentinel lost")
	}
	if got := i18n.New(i18n.Chinese).Error(err); got != "官方版本查询失败：操作已取消" {
		t.Fatal(got)
	}
	var safe *Error
	if !errors.As(err, &safe) || strings.Contains(safe.Diagnostic().Error(), "private-token") {
		t.Fatal("cancellation diagnostic leaked raw cause")
	}
}
