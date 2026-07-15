package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestBuildSubscriptionFetchStrategiesPrioritizesConfiguredUserAgent(t *testing.T) {
	strategies := buildSubscriptionFetchStrategies("clash.meta")
	if len(strategies) == 0 {
		t.Fatal("抓取策略不能为空")
	}
	if strategies[0].UserAgent != "clash.meta" || strategies[0].Name != "配置客户端" {
		t.Fatalf("首个策略 = %#v，期望优先使用配置客户端", strategies[0])
	}

	clashMetaCount := 0
	for _, strategy := range strategies {
		if strings.EqualFold(strategy.UserAgent, "clash.meta") {
			clashMetaCount++
		}
	}
	if clashMetaCount != 1 {
		t.Fatalf("clash.meta 策略数量 = %d，期望去重后为 1", clashMetaCount)
	}
}

func TestFetchURLContentWithClientUsesConfiguredUserAgent(t *testing.T) {
	wantBody := buildTestSubscriptionYAML(20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != "my-local-clash/1.0" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(wantBody))
	}))
	defer server.Close()

	got, err := fetchURLContentWithClient(server.URL+"/config?token=secret", server.Client(), "my-local-clash/1.0")
	if err != nil {
		t.Fatalf("fetchURLContentWithClient 返回错误: %v", err)
	}
	if got != wantBody {
		t.Fatalf("抓取内容与期望不一致")
	}
}

func TestFetchURLContentWithClientFallsBackToClashMeta(t *testing.T) {
	wantBody := buildTestSubscriptionYAML(20)
	var mu sync.Mutex
	requestedUserAgents := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestedUserAgents = append(requestedUserAgents, r.UserAgent())
		mu.Unlock()

		if r.UserAgent() != "clash.meta" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(wantBody))
	}))
	defer server.Close()

	got, err := fetchURLContentWithClient(server.URL, server.Client(), "unknown-client")
	if err != nil {
		t.Fatalf("fetchURLContentWithClient 返回错误: %v", err)
	}
	if got != wantBody {
		t.Fatalf("降级抓取内容与期望不一致")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requestedUserAgents) != 2 {
		t.Fatalf("请求次数 = %d，期望配置客户端失败后仅降级一次", len(requestedUserAgents))
	}
	if requestedUserAgents[0] != "unknown-client" || requestedUserAgents[1] != "clash.meta" {
		t.Fatalf("User-Agent 降级顺序 = %v", requestedUserAgents)
	}
}

func TestFetchURLContentWithClientReturnsActionableForbiddenError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "sensitive-token-value", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := fetchURLContentWithClient(server.URL+"/config?token=sensitive-token-value", server.Client(), "blocked-client")
	if err == nil {
		t.Fatal("全部策略返回 403 时应返回错误")
	}
	errorMessage := err.Error()
	for _, expected := range []string{"HTTP 403", "SUBSCRIPTION_USER_AGENT", "SUBSCRIPTION_PROXY_URL"} {
		if !strings.Contains(errorMessage, expected) {
			t.Fatalf("错误信息 %q 缺少 %q", errorMessage, expected)
		}
	}
	if strings.Contains(errorMessage, "sensitive-token-value") {
		t.Fatalf("错误信息泄露了订阅 Token: %q", errorMessage)
	}
}

func TestNewSubscriptionFetchErrorExplainsBrowserOnlyNotFound(t *testing.T) {
	err := newSubscriptionFetchError([]subscriptionFetchFailure{{StatusCode: http.StatusNotFound}})
	if !strings.Contains(err.Error(), "浏览器访问为 404 但 Clash 可用") {
		t.Fatalf("404 错误未提供客户端标识提示: %v", err)
	}
}

func TestNewSubscriptionHTTPClientValidatesProxyAndTLSOptions(t *testing.T) {
	if _, err := newSubscriptionHTTPClient("ftp://127.0.0.1:7890", false); err == nil {
		t.Fatal("不支持的代理协议应返回错误")
	}

	client, err := newSubscriptionHTTPClient("socks5://127.0.0.1:7890", true)
	if err != nil {
		t.Fatalf("创建订阅客户端失败: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport 类型 = %T，期望 *http.Transport", client.Transport)
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("显式开启时应跳过上游 TLS 证书校验")
	}

	request, err := http.NewRequest(http.MethodGet, "https://example.com/sub", nil)
	if err != nil {
		t.Fatalf("创建测试请求失败: %v", err)
	}
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatalf("解析代理地址失败: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "socks5://127.0.0.1:7890" {
		t.Fatalf("代理地址 = %v，期望 socks5://127.0.0.1:7890", proxyURL)
	}
}

func TestRedactSubscriptionURLRemovesCredentials(t *testing.T) {
	redacted := redactSubscriptionURL("https://user:password@example.com/config?token=secret&name=test")
	for _, sensitiveValue := range []string{"user", "password", "secret", "test"} {
		if strings.Contains(redacted, sensitiveValue) {
			t.Fatalf("脱敏地址 %q 仍包含敏感值 %q", redacted, sensitiveValue)
		}
	}
	if !strings.Contains(redacted, "token=REDACTED") || !strings.Contains(redacted, "name=REDACTED") {
		t.Fatalf("脱敏地址未保留必要的查询参数键: %q", redacted)
	}
}

func TestRedactSubscriptionRequestErrorRemovesToken(t *testing.T) {
	err := redactSubscriptionRequestError(&url.Error{
		Op:  http.MethodGet,
		URL: "https://example.com/config?token=sensitive-token-value",
		Err: errors.New("dial failed"),
	})
	if strings.Contains(err.Error(), "sensitive-token-value") {
		t.Fatalf("网络错误泄露了订阅 Token: %v", err)
	}
	if !strings.Contains(err.Error(), "token=REDACTED") || !strings.Contains(err.Error(), "dial failed") {
		t.Fatalf("网络错误脱敏后缺少必要诊断信息: %v", err)
	}
}

func buildTestSubscriptionYAML(nodeCount int) string {
	var builder strings.Builder
	builder.WriteString("proxies:\n")
	for index := 0; index < nodeCount; index++ {
		fmt.Fprintf(&builder, "  - name: node-%d\n    type: ss\n    server: 127.0.0.1\n    port: %d\n    cipher: aes-128-gcm\n    password: test\n", index, 8000+index)
	}
	return builder.String()
}
