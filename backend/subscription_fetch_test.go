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

	directClient, err := newSubscriptionHTTPClient("", false)
	if err != nil {
		t.Fatalf("创建直连客户端失败: %v", err)
	}
	directTransport := directClient.Transport.(*http.Transport)
	if directTransport.Proxy != nil {
		t.Fatal("关闭代理后 Transport.Proxy 必须为 nil，不能继续读取系统代理环境变量")
	}
}

func TestNormalizeSubscriptionProxyURLSupportsPoolFormats(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "主机端口用户名密码",
			input: "proxy.example.com:8080:alice:secret",
			want:  "http://alice:secret@proxy.example.com:8080",
		},
		{
			name:  "显式 Socks5",
			input: "socks5://alice:secret@proxy.example.com:1080",
			want:  "socks5://alice:secret@proxy.example.com:1080",
		},
		{
			name:  "用户名密码在前",
			input: "alice:secret@proxy.example.com:8080",
			want:  "http://alice:secret@proxy.example.com:8080",
		},
		{
			name:  "主机端口在前",
			input: "proxy.example.com:8080@alice:secret",
			want:  "http://alice:secret@proxy.example.com:8080",
		},
		{
			name:  "无认证 HTTP 代理",
			input: "proxy.example.com:8080",
			want:  "http://proxy.example.com:8080",
		},
		{
			name:  "显式 HTTPS 代理",
			input: "https://alice:secret@proxy.example.com:8443",
			want:  "https://alice:secret@proxy.example.com:8443",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := normalizeSubscriptionProxyURL(testCase.input)
			if err != nil {
				t.Fatalf("normalizeSubscriptionProxyURL 返回错误: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("标准化结果 = %q，期望 %q", got, testCase.want)
			}
		})
	}

	specialURL, err := normalizeSubscriptionProxyURL("proxy.example.com:8080:alice:p@ss:word")
	if err != nil {
		t.Fatalf("特殊字符凭据标准化失败: %v", err)
	}
	parsedSpecialURL, err := url.Parse(specialURL)
	if err != nil {
		t.Fatalf("标准化后的代理 URL 无法解析: %v", err)
	}
	password, hasPassword := parsedSpecialURL.User.Password()
	if parsedSpecialURL.User.Username() != "alice" || !hasPassword || password != "p@ss:word" {
		t.Fatalf("特殊字符凭据未被安全保留: %q", specialURL)
	}
}

func TestParseSubscriptionProxyPoolSkipsBlankAndDuplicateLines(t *testing.T) {
	pool, err := parseSubscriptionProxyPool("\nproxy.example.com:8080:alice:secret\r\nhttp://alice:secret@proxy.example.com:8080\nproxy-2.example.com:8081\n")
	if err != nil {
		t.Fatalf("parseSubscriptionProxyPool 返回错误: %v", err)
	}
	if len(pool) != 2 {
		t.Fatalf("去重后代理数量 = %d，期望 2", len(pool))
	}
	if pool[0].URL != "http://alice:secret@proxy.example.com:8080" || pool[1].URL != "http://proxy-2.example.com:8081" {
		t.Fatalf("代理池标准化结果不符合预期: %#v", pool)
	}
}

func TestParseSubscriptionProxyPoolRejectsInvalidLineWithoutLeakingCredentials(t *testing.T) {
	_, err := parseSubscriptionProxyPool("proxy.example.com:8080\nftp://secret-user:secret-password@proxy.example.com:21")
	if err == nil {
		t.Fatal("不支持的代理协议应返回错误")
	}
	if !strings.Contains(err.Error(), "第 2 行") {
		t.Fatalf("错误信息缺少行号: %v", err)
	}
	for _, sensitiveValue := range []string{"secret-user", "secret-password"} {
		if strings.Contains(err.Error(), sensitiveValue) {
			t.Fatalf("错误信息泄露代理凭据 %q: %v", sensitiveValue, err)
		}
	}
}

func TestConfiguredSubscriptionProxyPoolKeepsLegacyProxyAndDeduplicates(t *testing.T) {
	var cfg Config
	cfg.SubscriptionFetch.ProxyURLs = []string{
		"proxy-1.example.com:8080",
		"http://proxy-2.example.com:8081",
	}
	cfg.SubscriptionFetch.ProxyURL = "proxy-1.example.com:8080"

	got := configuredSubscriptionProxyPool(cfg)
	want := "proxy-1.example.com:8080\nhttp://proxy-2.example.com:8081"
	if got != want {
		t.Fatalf("配置代理池 = %q，期望 %q", got, want)
	}
}

func TestSplitSubscriptionProxyEnvironmentSupportsCommonSeparators(t *testing.T) {
	got := splitSubscriptionProxyEnvironment("proxy-1:8001,proxy-2:8002;proxy-3:8003\nproxy-4:8004")
	if len(got) != 4 {
		t.Fatalf("环境变量代理数量 = %d，期望 4: %v", len(got), got)
	}
}

func TestHasConfiguredSubscriptionProxySwitchUsesTOMLStructure(t *testing.T) {
	configData := []byte("[subscription_fetch] # 合法的行尾注释\nproxy_enabled = false\nproxy_url = \"proxy.example.com:8080\"\n\n[auth]\nproxy_enabled = true\n")
	if !hasConfiguredSubscriptionProxySwitch(configData) {
		t.Fatal("应识别 subscription_fetch 中显式配置的 proxy_enabled")
	}
	if hasConfiguredSubscriptionProxySwitch([]byte("[subscription_fetch]\nproxy_url = \"proxy.example.com:8080\"\n")) {
		t.Fatal("未显式配置 proxy_enabled 时不应识别为已配置")
	}
}

func TestFetchURLContentWithProxyPoolFallsBackAfterForbidden(t *testing.T) {
	wantBody := buildTestSubscriptionYAML(20)
	var mu sync.Mutex
	forbiddenRequests := 0
	successRequests := 0

	forbiddenProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		forbiddenRequests++
		mu.Unlock()
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer forbiddenProxy.Close()

	successProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		successRequests++
		mu.Unlock()
		_, _ = w.Write([]byte(wantBody))
	}))
	defer successProxy.Close()

	pool, err := parseSubscriptionProxyPool(forbiddenProxy.URL + "\n" + successProxy.URL)
	if err != nil {
		t.Fatalf("解析测试代理池失败: %v", err)
	}
	subscriptionProxyCursor.Store(0)
	got, err := fetchURLContentWithProxyPool(
		"http://subscription.invalid/config?token=sensitive-token-value",
		pool,
		"clash.meta",
		false,
	)
	if err != nil {
		t.Fatalf("代理池故障转移返回错误: %v", err)
	}
	if got != wantBody {
		t.Fatal("代理池故障转移后的订阅内容与期望不一致")
	}

	mu.Lock()
	defer mu.Unlock()
	if forbiddenRequests == 0 || successRequests != 1 {
		t.Fatalf("代理请求次数不符合预期，403 出口=%d，成功出口=%d", forbiddenRequests, successRequests)
	}
}

func TestShouldSwitchSubscriptionProxyImmediately(t *testing.T) {
	for _, statusCode := range []int{http.StatusProxyAuthRequired, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway} {
		if !shouldSwitchSubscriptionProxyImmediately(statusCode) {
			t.Fatalf("状态码 %d 应立即切换代理", statusCode)
		}
	}
	for _, statusCode := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		if shouldSwitchSubscriptionProxyImmediately(statusCode) {
			t.Fatalf("状态码 %d 不应跳过 User-Agent 探测", statusCode)
		}
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
