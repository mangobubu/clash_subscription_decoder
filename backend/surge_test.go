package main

import (
	"strings"
	"testing"
)

func TestConvertClashYAMLToSurgeConvertsNodesGroupsAndRules(t *testing.T) {
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
  - name: VMess
    type: vmess
    server: vmess.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    tls: true
    servername: edge.example.com
    skip-cert-verify: true
    network: ws
    ws-opts:
      path: /ws
      headers:
        Host: cdn.example.com
  - name: Trojan
    type: trojan
    server: trojan.example.com
    port: 443
    password: trojan-pass
    sni: trojan.example.com
    network: ws
    ws-opts:
      path: /trojan
  - name: HY2
    type: hysteria2
    server: hy2.example.com
    port: 8443
    password: hy-pass
    sni: hy2.example.com
    obfs-password: obfs-pass
    download-bandwidth: 100
    alpn:
      - h3
  - name: VLESS
    type: vless
    server: vless.example.com
    port: 443
proxy-groups:
  - name: 手动,组
    type: select
    proxies:
      - SS,One
      - Socks
      - VLESS
      - DIRECT
  - name: Auto
    type: url-test
    tolerance: 80
    proxies:
      - SS,One
      - VMess
  - name: Fallback
    type: fallback
    url: http://test.example/generate_204
    interval: 300
    timeout: 5
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

	got, err := ConvertClashYAMLToSurge(input)
	if err != nil {
		t.Fatalf("ConvertClashYAMLToSurge returned error: %v", err)
	}

	assertContains(t, got, "[General]")
	assertContains(t, got, "[Proxy]")
	assertContains(t, got, "SS，One = ss,ss.example.com,443,encrypt-method=aes-128-gcm,password=ss-pass,udp-relay=true")
	assertContains(t, got, "Socks = socks5,socks.example.com,1080,user,pass,udp-relay=true")
	assertContains(t, got, "VMess = vmess,vmess.example.com,443,username=00000000-0000-0000-0000-000000000000,tls=true,sni=edge.example.com,skip-cert-verify=true,ws=true,ws-path=/ws,ws-headers=Host:cdn.example.com")
	assertContains(t, got, "Trojan = trojan,trojan.example.com,443,password=trojan-pass,udp-relay=true,sni=trojan.example.com,ws=true,ws-path=/trojan")
	assertContains(t, got, "HY2 = hysteria2,hy2.example.com,8443,password=hy-pass,download-bandwidth=100,salamander-password=obfs-pass,alpn=h3,sni=hy2.example.com")

	assertContains(t, got, "[Proxy Group]")
	assertContains(t, got, "手动，组 = select,SS，One,Socks,DIRECT")
	assertContains(t, got, "Auto = url-test,SS，One,VMess,url=http://www.gstatic.com/generate_204,interval=600,tolerance=80")
	assertContains(t, got, "Fallback = fallback,Auto,HY2,url=http://test.example/generate_204,interval=300,timeout=5")
	assertContains(t, got, "Balance = load-balance,SS，One,HY2")

	assertContains(t, got, "[Rule]")
	assertContains(t, got, "DOMAIN-SUFFIX,example.com,手动，组")
	assertContains(t, got, "IP-CIDR,1.1.1.1/32,Auto,no-resolve")
	assertContains(t, got, "FINAL,Fallback")

	if strings.Contains(got, "VLESS") || strings.Contains(got, "vless.example.com") {
		t.Fatalf("unsupported vless node leaked into Surge config:\n%s", got)
	}
}

func TestConvertClashYAMLToSurgeCreatesDefaultGroupWhenMissing(t *testing.T) {
	input := `
proxies:
  - name: Only
    type: ss
    server: ss.example.com
    port: 443
    cipher: aes-128-gcm
    password: ss-pass
`

	got, err := ConvertClashYAMLToSurge(input)
	if err != nil {
		t.Fatalf("ConvertClashYAMLToSurge returned error: %v", err)
	}

	assertContains(t, got, "PROXY = select,Only")
	assertContains(t, got, "FINAL,PROXY")
}

func TestConvertClashYAMLToSurgeRejectsUnsupportedOnlyConfig(t *testing.T) {
	input := `
proxies:
  - name: Unsupported
    type: vless
    server: vless.example.com
    port: 443
`

	if _, err := ConvertClashYAMLToSurge(input); err == nil {
		t.Fatal("expected unsupported-only config to fail")
	}
}
