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

	got := buildAbsoluteRequestURL(ctx, "/shadowrocket/config/test-token.conf")
	want := "https://proxy.example.com/shadowrocket/config/test-token.conf"
	if got != want {
		t.Fatalf("buildAbsoluteRequestURL() = %q, want %q", got, want)
	}
}

func TestParseShadowrocketConfigToken(t *testing.T) {
	got, err := parseShadowrocketConfigToken("test-token%3D.conf")
	if err != nil {
		t.Fatalf("parseShadowrocketConfigToken returned error: %v", err)
	}
	if got != "test-token=" {
		t.Fatalf("parseShadowrocketConfigToken() = %q, want %q", got, "test-token=")
	}
}

func TestShadowrocketInstallPageContainsConfigScheme(t *testing.T) {
	configURL := "https://proxy.example.com/shadowrocket/config/test-token.conf"
	installURL := "shadowrocket://config/add/" + configURL
	page := buildShadowrocketInstallHTML(installURL)

	if !strings.Contains(page, "安装到 Shadowrocket") {
		t.Fatalf("install page should contain fallback install link text, got:\n%s", page)
	}
	if !strings.Contains(page, installURL) {
		t.Fatalf("install page should contain scheme URL %q, got:\n%s", installURL, page)
	}
	if !strings.Contains(page, configURL) {
		t.Fatalf("install page should keep the raw config URL visible to Shadowrocket, got:\n%s", page)
	}
}
