package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBuildAbsoluteRequestURLUsesForwardedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/shadowrocket/install?token=test-token", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "proxy.example.com")

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req

	got := buildAbsoluteRequestURL(ctx, "/shadowrocket.conf?token=test-token")
	want := "https://proxy.example.com/shadowrocket.conf?token=test-token"
	if got != want {
		t.Fatalf("buildAbsoluteRequestURL() = %q, want %q", got, want)
	}
}

func TestShadowrocketInstallPageContainsConfigScheme(t *testing.T) {
	configURL := "https://proxy.example.com/shadowrocket.conf?token=test-token"
	installURL := "shadowrocket://config/add/https%3A%2F%2Fproxy.example.com%2Fshadowrocket.conf%3Ftoken%3Dtest-token"
	page := buildShadowrocketInstallHTML(installURL)

	if !strings.Contains(page, "安装到 Shadowrocket") {
		t.Fatalf("install page should contain fallback install link text, got:\n%s", page)
	}
	if !strings.Contains(page, installURL) {
		t.Fatalf("install page should contain scheme URL %q, got:\n%s", installURL, page)
	}
	if strings.Contains(page, configURL) {
		t.Fatalf("raw config URL should stay encoded inside scheme URL, got:\n%s", page)
	}
}
