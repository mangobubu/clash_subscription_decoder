package main

import (
	"strings"
	"testing"
)

func TestConvertClashYAMLToShadowrocketConvertsNodesGroupsAndRules(t *testing.T) {
	input := `
proxies:
  - name: SS,One
    type: ss
    server: ss.example.com
    port: 443
    cipher: aes-128-gcm
    password: ss-pass
  - name: Socks
    type: socks5
    server: socks.example.com
    port: 1080
    username: user
    password: pass
  - name: VLESS
    type: vless
    server: vless.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    tls: true
    servername: edge.example.com
    network: ws
    ws-opts:
      path: /ws
      headers:
        Host: cdn.example.com
  - name: HY2
    type: hysteria2
    server: hy2.example.com
    port: 8443
    password: hy-pass
    sni: hy2.example.com
    obfs-password: obfs-pass
    alpn:
      - h3
  - name: VMess
    type: vmess
    server: vmess.example.com
    port: 443
proxy-groups:
  - name: 手动,组
    type: select
    proxies:
      - SS,One
      - Socks
      - VMess
      - DIRECT
  - name: Auto
    type: url-test
    proxies:
      - SS,One
      - VLESS
  - name: Fallback
    type: fallback
    url: http://test.example/generate_204
    interval: 300
    timeout: 5
    tolerance: 50
    proxies:
      - Auto
      - HY2
  - name: Balance
    type: load-balance
    proxies:
      - SS,One
      - HY2
rules:
  - DOMAIN-SUFFIX,example.com,手动,组
  - IP-CIDR,1.1.1.1/32,Auto,no-resolve
  - MATCH,Fallback
`

	got, err := ConvertClashYAMLToShadowrocket(input)
	if err != nil {
		t.Fatalf("ConvertClashYAMLToShadowrocket returned error: %v", err)
	}

	assertContains(t, got, "[Proxy]")
	assertContains(t, got, "SS，One = ss,ss.example.com,443,password=ss-pass,method=aes-128-gcm,udp-relay=true")
	assertContains(t, got, "Socks = socks5,socks.example.com,1080,user,pass")
	assertContains(t, got, "VLESS = vless,vless.example.com,443,password=00000000-0000-0000-0000-000000000000,tls=true,peer=edge.example.com,obfs=websocket,obfs-uri=/ws,obfs-host=cdn.example.com")
	assertContains(t, got, "HY2 = hysteria2,hy2.example.com,8443,auth=hy-pass,udp=1,peer=hy2.example.com,obfsParam=obfs-pass,alpn=h3")

	assertContains(t, got, "[Proxy Group]")
	assertContains(t, got, "手动，组 = select,SS，One,Socks,DIRECT")
	assertContains(t, got, "Auto = url-test,SS，One,VLESS,url=http://www.gstatic.com/generate_204,interval=600")
	assertContains(t, got, "Fallback = fallback,Auto,HY2,url=http://test.example/generate_204,interval=300,timeout=5,tolerance=50")
	assertContains(t, got, "Balance = load-balance,SS，One,HY2,url=http://www.gstatic.com/generate_204,interval=600")

	assertContains(t, got, "[Rule]")
	assertContains(t, got, "DOMAIN-SUFFIX,example.com,手动，组")
	assertContains(t, got, "IP-CIDR,1.1.1.1/32,Auto,no-resolve")
	assertContains(t, got, "FINAL,Fallback")

	if strings.Contains(got, "VMess") || strings.Contains(got, "vmess.example.com") {
		t.Fatalf("unsupported vmess node leaked into Shadowrocket config:\n%s", got)
	}
}

func TestConvertClashYAMLToShadowrocketCreatesDefaultGroupWhenMissing(t *testing.T) {
	input := `
proxies:
  - name: Only
    type: ss
    server: ss.example.com
    port: 443
    cipher: aes-128-gcm
    password: ss-pass
`

	got, err := ConvertClashYAMLToShadowrocket(input)
	if err != nil {
		t.Fatalf("ConvertClashYAMLToShadowrocket returned error: %v", err)
	}

	assertContains(t, got, "PROXY = select,Only")
	assertContains(t, got, "FINAL,PROXY")
}

func TestConvertClashYAMLToShadowrocketMapsConfiguredGroupsToBuiltinProxy(t *testing.T) {
	input := `
proxies:
  - name: Primary
    type: ss
    server: primary.example.com
    port: 443
    cipher: aes-128-gcm
    password: primary-pass
  - name: Backup
    type: ss
    server: backup.example.com
    port: 443
    cipher: aes-128-gcm
    password: backup-pass
proxy-groups:
  - name: 节点选择
    type: select
    proxies:
      - Primary
  - name: 微软服务
    type: select
    proxies:
      - 节点选择
      - Backup
      - DIRECT
  - name: 保持原样
    type: select
    proxies:
      - Backup
rules:
  - DOMAIN-SUFFIX,storage.msn.com,节点选择
  - DOMAIN-SUFFIX,office.com,微软服务
  - DOMAIN-SUFFIX,plain.example,保持原样
  - MATCH,节点选择
`

	got, err := ConvertClashYAMLToShadowrocketWithOptions(input, shadowrocketConversionOptions{
		BuiltinProxyGroups: map[string]bool{"节点选择": true},
	})
	if err != nil {
		t.Fatalf("ConvertClashYAMLToShadowrocketWithOptions returned error: %v", err)
	}

	assertContains(t, got, "节点选择 = select,Primary")
	assertContains(t, got, "微软服务 = select,PROXY,Backup,DIRECT")
	assertContains(t, got, "保持原样 = select,Backup")
	assertContains(t, got, "DOMAIN-SUFFIX,storage.msn.com,PROXY")
	assertContains(t, got, "DOMAIN-SUFFIX,office.com,微软服务")
	assertContains(t, got, "DOMAIN-SUFFIX,plain.example,保持原样")
	assertContains(t, got, "FINAL,PROXY")
	assertNotContains(t, got, "DOMAIN-SUFFIX,storage.msn.com,节点选择")
}

func TestConvertClashYAMLToShadowrocketKeepsAnyTLS(t *testing.T) {
	input := `
proxies:
  - name: AnyTLS One
    type: anytls
    server: any.example.com
    port: 443
    password: any-pass
    sni: apple.com
    skip-cert-verify: true
    alpn:
      - h2
    udp: true
proxy-groups:
  - name: Auto
    type: url-test
    proxies:
      - AnyTLS One
      - DIRECT
rules:
  - MATCH,Auto
`

	got, err := ConvertClashYAMLToShadowrocket(input)
	if err != nil {
		t.Fatalf("ConvertClashYAMLToShadowrocket returned error: %v", err)
	}

	assertContains(t, got, "AnyTLS One = anytls,any.example.com,443,password=any-pass,peer=apple.com,allowInsecure=1,alpn=h2,udp=1")
	assertContains(t, got, "Auto = url-test,AnyTLS One,DIRECT,url=http://www.gstatic.com/generate_204,interval=600")
	assertContains(t, got, "FINAL,Auto")
}

func TestConvertClashYAMLToShadowrocketRejectsUnsupportedOnlyConfig(t *testing.T) {
	input := `
proxies:
  - name: Unsupported
    type: vmess
    server: vmess.example.com
    port: 443
`

	if _, err := ConvertClashYAMLToShadowrocket(input); err == nil {
		t.Fatal("expected unsupported-only config to fail")
	}
}

func assertContains(t *testing.T, content, expected string) {
	t.Helper()
	if !strings.Contains(content, expected) {
		t.Fatalf("expected output to contain %q, got:\n%s", expected, content)
	}
}

func assertNotContains(t *testing.T, content, unexpected string) {
	t.Helper()
	if strings.Contains(content, unexpected) {
		t.Fatalf("expected output not to contain %q, got:\n%s", unexpected, content)
	}
}
