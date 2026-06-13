package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadProfileRawContentRejectsLocalContentWithoutFetcher(t *testing.T) {
	profile := SubscriptionProfile{SourceType: profileSourceLocal}

	_, fetchedRemote, err := loadProfileRawContent(profile, func(string) (string, error) {
		t.Fatal("local profile must not call remote fetcher")
		return "", nil
	})

	if err == nil {
		t.Fatal("local profile should not be loaded from local_content")
	}
	if fetchedRemote {
		t.Fatal("local profile unexpectedly marked as remote fetch")
	}
}

func TestLoadProfileRawContentFetchesRemoteContent(t *testing.T) {
	profile := SubscriptionProfile{
		SourceType: profileSourceRemote,
		URL:        "https://example.com/sub",
	}

	got, fetchedRemote, err := loadProfileRawContent(profile, func(targetURL string) (string, error) {
		if targetURL != profile.URL {
			t.Fatalf("fetch URL = %q, want %q", targetURL, profile.URL)
		}
		return "proxies: []", nil
	})

	if err != nil {
		t.Fatalf("loadProfileRawContent returned error: %v", err)
	}
	if !fetchedRemote {
		t.Fatal("remote profile did not mark as remote fetch")
	}
	if got != "proxies: []" {
		t.Fatalf("loadProfileRawContent() = %q, want remote payload", got)
	}
}

func TestMergeProfileRulesPreservesTargetAndLetsSourceOverride(t *testing.T) {
	targetRules := []CustomRule{
		{ID: 1, ProfileID: 10, Type: "DOMAIN-SUFFIX", Payload: "example.com", Target: "DIRECT"},
		{ID: 2, ProfileID: 10, Type: "DOMAIN", Payload: "keep.example", Target: "REJECT"},
	}
	sourceRules := []CustomRule{
		{ID: 3, ProfileID: 20, Type: "DOMAIN-SUFFIX", Payload: "example.com", Target: "PROXY"},
		{ID: 4, ProfileID: 20, Type: "MATCH", Payload: "-", Target: "DIRECT"},
	}

	got := mergeProfileRules(targetRules, sourceRules, 10)
	if len(got) != 3 {
		t.Fatalf("mergeProfileRules length = %d, want 3", len(got))
	}

	byKey := map[string]CustomRule{}
	for _, rule := range got {
		if rule.ID != 0 {
			t.Fatalf("merged rule ID = %d, want reset to 0", rule.ID)
		}
		if rule.ProfileID != 10 {
			t.Fatalf("merged rule ProfileID = %d, want 10", rule.ProfileID)
		}
		byKey[rule.Type+"\x00"+rule.Payload] = rule
	}

	if byKey["DOMAIN-SUFFIX\x00example.com"].Target != "PROXY" {
		t.Fatal("source rule did not override same type/payload target rule")
	}
	if byKey["DOMAIN\x00keep.example"].Target != "REJECT" {
		t.Fatal("target-only rule was not preserved")
	}
	if byKey["MATCH\x00-"].Target != "DIRECT" {
		t.Fatal("source-only rule was not added")
	}
}

func TestExtractCopyableProxyGroupsFromYAMLFoldsSourceNodesAndKeepsExtra(t *testing.T) {
	content := `proxies:
  - name: 香港 01
    type: ss
  - name: 美国 02
    type: trojan
proxy-groups:
  - name: 自动选择
    type: url-test
    proxies:
      - 香港 01
      - 美国 02
      - DIRECT
    url: http://test.example/generate_204
    interval: 300
    tolerance: 80
    use:
      - remote-provider
    filter: 港
  - name: 兜底策略
    type: fallback
    proxies:
      - 自动选择
      - REJECT
`

	groups, orderNames, err := extractCopyableProxyGroupsFromYAML(content, 10)
	if err != nil {
		t.Fatalf("extractCopyableProxyGroupsFromYAML returned error: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups length = %d, want 2", len(groups))
	}
	assertStringSliceEqual(t, orderNames, []string{"自动选择", "兜底策略"})

	if groups[0].ProfileID != 10 {
		t.Fatalf("copied group ProfileID = %d, want 10", groups[0].ProfileID)
	}
	assertStringSliceEqual(t, groups[0].GetProxiesList(), []string{"[ALL_NODES]", "DIRECT"})
	assertStringSliceEqual(t, groups[1].GetProxiesList(), []string{"自动选择", "REJECT"})

	var extra map[string]interface{}
	if err := json.Unmarshal([]byte(groups[0].Extra), &extra); err != nil {
		t.Fatalf("extra JSON invalid: %v", err)
	}
	if extra["url"] != "http://test.example/generate_204" {
		t.Fatalf("extra url = %v", extra["url"])
	}
	if _, ok := extra["use"]; ok {
		t.Fatal("provider scoped use field should be removed")
	}
	if _, ok := extra["filter"]; ok {
		t.Fatal("provider scoped filter field should be removed")
	}
}

func TestBuildManualProfileYAMLFromResourcesKeepsCopiedGroupExtraAndOrder(t *testing.T) {
	extraBytes, err := json.Marshal(map[string]interface{}{
		"url":       "http://test.example/generate_204",
		"interval":  300,
		"tolerance": 80,
		"use":       []string{"remote-provider"},
	})
	if err != nil {
		t.Fatalf("json.Marshal extra returned error: %v", err)
	}

	nodes := []CustomNode{
		{
			Name:   "香港 01",
			Type:   "ss",
			Server: "127.0.0.1",
			Port:   8388,
			Config: `{"name":"香港 01","type":"ss","server":"127.0.0.1","port":8388}`,
		},
		{
			Name:   "美国 02",
			Type:   "trojan",
			Server: "127.0.0.2",
			Port:   443,
			Config: `{"name":"美国 02","type":"trojan","server":"127.0.0.2","port":443}`,
		},
	}
	groups := []CustomProxyGroup{
		{Name: "自动选择", Type: "url-test", Proxies: `["[ALL_NODES]","DIRECT"]`, Extra: string(extraBytes)},
		{Name: "兜底策略", Type: "fallback", Proxies: `["自动选择","REJECT"]`},
	}

	rules := []CustomRule{{Type: "MATCH", Payload: "-", Target: "自动选择"}}
	got, err := buildManualProfileYAMLFromResources(nodes, applyCustomGroupOrder(groups, []string{"兜底策略", "自动选择"}), rules)
	if err != nil {
		t.Fatalf("buildManualProfileYAMLFromResources returned error: %v", err)
	}

	assertStringSliceEqual(t, yamlSequenceNames(t, got, "proxy-groups"), []string{"兜底策略", "自动选择"})
	assertYAMLGroupProxiesEqual(t, got, "自动选择", []string{"香港 01", "美国 02", "DIRECT"})
	assertYAMLGroupScalarEqual(t, got, "自动选择", "url", "http://test.example/generate_204")
	assertYAMLGroupScalarEqual(t, got, "自动选择", "interval", "300")
	assertYAMLGroupScalarEqual(t, got, "自动选择", "tolerance", "80")
	assertYAMLGroupFieldAbsent(t, got, "自动选择", "use")
}

func TestLooksLikePlainSubscriptionConfigAcceptsProviderOnlyYaml(t *testing.T) {
	content := "proxy-providers:\n  provider-a:\n    type: http\nproxy-groups:\n  - name: PROXY\nrules:\n  - MATCH,PROXY\n"
	if !looksLikePlainSubscriptionConfig(content) {
		t.Fatal("provider-only Clash YAML was not recognized as plain config")
	}
}

func TestBuildManualProfileYAMLFromResourcesRejectsEmptyNodes(t *testing.T) {
	_, err := buildManualProfileYAMLFromResources(nil, nil, nil)
	if err == nil {
		t.Fatal("expected local manual profile without nodes to fail")
	}
}

func TestBuildManualProfileYAMLFromResourcesCreatesDefaults(t *testing.T) {
	nodes := []CustomNode{
		{
			Name:   "香港 01",
			Type:   "ss",
			Server: "127.0.0.1",
			Port:   8388,
			Config: `{"name":"香港 01","type":"ss","server":"127.0.0.1","port":8388,"cipher":"aes-256-gcm","password":"pass"}`,
		},
	}

	got, err := buildManualProfileYAMLFromResources(nodes, nil, nil)
	if err != nil {
		t.Fatalf("buildManualProfileYAMLFromResources returned error: %v", err)
	}

	for _, expected := range []string{
		"proxies:",
		"name: 香港 01",
		"proxy-groups:",
		"name: 代理",
		"- DIRECT",
		defaultGeositeDirectRule,
		defaultGeoIPDirectRule,
		defaultProxyMatchRule,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("generated YAML missing %q:\n%s", expected, got)
		}
	}
}

func TestBuildManualProfileYAMLFromResourcesKeepsCustomRules(t *testing.T) {
	nodes := []CustomNode{
		{
			Name:   "新加坡 01",
			Type:   "socks5",
			Server: "127.0.0.1",
			Port:   1080,
			Config: `{"name":"新加坡 01","type":"socks5","server":"127.0.0.1","port":1080}`,
		},
	}
	rules := []CustomRule{
		{Type: "DOMAIN-SUFFIX", Payload: "example.com", Target: "DIRECT"},
	}

	got, err := buildManualProfileYAMLFromResources(nodes, nil, rules)
	if err != nil {
		t.Fatalf("buildManualProfileYAMLFromResources returned error: %v", err)
	}
	if !strings.Contains(got, "DOMAIN-SUFFIX,example.com,DIRECT") {
		t.Fatalf("generated YAML missing custom rule:\n%s", got)
	}
	if strings.Contains(got, defaultGeositeDirectRule) {
		t.Fatalf("generated YAML should not include default rules when custom rules exist:\n%s", got)
	}
}

func TestBuildManualProfileYAMLFromResourcesSkipsDeletedRules(t *testing.T) {
	nodes := []CustomNode{
		{
			Name:   "新加坡 01",
			Type:   "socks5",
			Server: "127.0.0.1",
			Port:   1080,
			Config: `{"name":"新加坡 01","type":"socks5","server":"127.0.0.1","port":1080}`,
		},
	}
	rules := []CustomRule{
		{Type: "DOMAIN-SUFFIX", Payload: "deleted.example", Target: deletedCustomRuleTarget},
		{Type: "MATCH", Payload: "-", Target: "DIRECT"},
	}

	got, err := buildManualProfileYAMLFromResources(nodes, nil, rules)
	if err != nil {
		t.Fatalf("buildManualProfileYAMLFromResources returned error: %v", err)
	}
	if strings.Contains(got, deletedCustomRuleTarget) || strings.Contains(got, "deleted.example") {
		t.Fatalf("generated YAML should skip deleted rules:\n%s", got)
	}
	if !strings.Contains(got, "MATCH,DIRECT") {
		t.Fatalf("generated YAML missing retained rule:\n%s", got)
	}
}

func TestBuildManualRuleLinesKeepsTerminalRulesAtEnd(t *testing.T) {
	got := buildManualRuleLines([]CustomRule{
		{Type: "DOMAIN-SUFFIX", Payload: "before.example", Target: "DIRECT"},
		{Type: "MATCH", Payload: "-", Target: "Leak"},
		{Type: "DOMAIN-SUFFIX", Payload: "after.example", Target: "DIRECT"},
	})

	assertStringSliceEqual(t, got, []string{
		"DOMAIN-SUFFIX,before.example,DIRECT",
		"DOMAIN-SUFFIX,after.example,DIRECT",
		"MATCH,Leak",
	})
}

func TestApplyResourceOrderToYAMLContentOrdersNodes(t *testing.T) {
	content := `proxies:
  - name: 节点 A
    type: ss
  - name: 节点 B
    type: ss
  - name: 节点 C
    type: ss
proxy-groups: []
`

	got := applyResourceOrderToYAMLContent(content, resourceOrderTypeNodes, []string{"节点 C", "不存在", "节点 A", "节点 C", ""})
	names := yamlSequenceNames(t, got, "proxies")
	assertStringSliceEqual(t, names, []string{"节点 C", "节点 A", "节点 B"})
}

func TestApplyResourceOrderToYAMLContentOrdersGroups(t *testing.T) {
	content := `proxies: []
proxy-groups:
  - name: 自动选择
    type: url-test
    proxies: []
  - name: 手动选择
    type: select
    proxies: []
  - name: 兜底策略
    type: fallback
    proxies: []
`

	got := applyResourceOrderToYAMLContent(content, resourceOrderTypeGroups, []string{"兜底策略", "自动选择"})
	names := yamlSequenceNames(t, got, "proxy-groups")
	assertStringSliceEqual(t, names, []string{"兜底策略", "自动选择", "手动选择"})
}

func TestFilterYAMLSequenceByNameRemovesHiddenAndOverriddenItems(t *testing.T) {
	content := `proxies:
  - name: 订阅节点 A
    type: ss
  - name: 接管节点
    type: vmess
  - name: 保留节点
    type: trojan
`
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		t.Fatalf("yaml.Unmarshal returned error: %v", err)
	}
	seq := findTopLevelSequenceNode(root.Content[0], "proxies")
	filterYAMLSequenceByName(seq, map[string]bool{"订阅节点 A": true}, map[string]bool{"接管节点": true})

	out, err := yaml.Marshal(&root)
	if err != nil {
		t.Fatalf("yaml.Marshal returned error: %v", err)
	}
	assertStringSliceEqual(t, yamlSequenceNames(t, string(out), "proxies"), []string{"保留节点"})
}

func TestPruneProxyGroupMembersRemovesUnavailableReferences(t *testing.T) {
	content := `proxy-groups:
  - name: 自动选择
    type: select
    proxies:
      - 可用节点
      - 已删节点
      - 兜底策略
      - 已删策略组
      - DIRECT
  - name: 兜底策略
    type: fallback
    proxies:
      - 可用节点
`
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		t.Fatalf("yaml.Unmarshal returned error: %v", err)
	}
	seq := findTopLevelSequenceNode(root.Content[0], "proxy-groups")
	pruneProxyGroupMembers(seq, map[string]bool{"可用节点": true, "自动选择": true, "兜底策略": true})

	out, err := yaml.Marshal(&root)
	if err != nil {
		t.Fatalf("yaml.Marshal returned error: %v", err)
	}
	assertYAMLGroupProxiesEqual(t, string(out), "自动选择", []string{"可用节点", "兜底策略", "DIRECT"})
}

func TestStringSliceFromAnyNormalizesProxyMembers(t *testing.T) {
	got := stringSliceFromAny([]interface{}{" 节点 A ", "", 123, "DIRECT"})
	assertStringSliceEqual(t, got, []string{"节点 A", "123", "DIRECT"})
}

func TestBuildManualProfileYAMLFromResourcesKeepsSavedResourceOrder(t *testing.T) {
	nodes := []CustomNode{
		{
			Name:   "香港 01",
			Type:   "ss",
			Server: "127.0.0.1",
			Port:   8388,
			Config: `{"name":"香港 01","type":"ss","server":"127.0.0.1","port":8388}`,
		},
		{
			Name:   "新加坡 02",
			Type:   "socks5",
			Server: "127.0.0.2",
			Port:   1080,
			Config: `{"name":"新加坡 02","type":"socks5","server":"127.0.0.2","port":1080}`,
		},
		{
			Name:   "美国 03",
			Type:   "trojan",
			Server: "127.0.0.3",
			Port:   443,
			Config: `{"name":"美国 03","type":"trojan","server":"127.0.0.3","port":443}`,
		},
	}
	groups := []CustomProxyGroup{
		{Name: "手动选择", Type: "select", Proxies: `["[ALL_NODES]"]`},
		{Name: "自动选择", Type: "url-test", Proxies: `["[ALL_NODES]"]`},
		{Name: "兜底策略", Type: "fallback", Proxies: `["[ALL_NODES]"]`},
	}
	rules := []CustomRule{{Type: "MATCH", Payload: "-", Target: "手动选择"}}

	orderedNodes := applyCustomNodeOrder(nodes, []string{"新加坡 02", "香港 01"})
	orderedGroups := applyCustomGroupOrder(groups, []string{"自动选择", "手动选择"})
	got, err := buildManualProfileYAMLFromResources(orderedNodes, orderedGroups, rules)
	if err != nil {
		t.Fatalf("buildManualProfileYAMLFromResources returned error: %v", err)
	}

	assertStringSliceEqual(t, yamlSequenceNames(t, got, "proxies"), []string{"新加坡 02", "香港 01", "美国 03"})
	assertStringSliceEqual(t, yamlSequenceNames(t, got, "proxy-groups"), []string{"自动选择", "手动选择", "兜底策略"})
	if !strings.Contains(got, "MATCH,手动选择") {
		t.Fatalf("generated YAML missing custom MATCH rule:\n%s", got)
	}
}

func TestExtractProfileRulesFromSubscriptionContentParsesClashRules(t *testing.T) {
	content := `proxies: []
proxy-groups: []
rules:
  - DOMAIN-SUFFIX,example.com,DIRECT
  - GEOSITE,cn,DIRECT
  - GEOIP,CN,DIRECT,no-resolve
  - AND,((DOMAIN,example.org),(NETWORK,UDP)),REJECT
  - MATCH,代理
`

	got, err := extractProfileRulesFromSubscriptionContent(content, 7)
	if err != nil {
		t.Fatalf("extractProfileRulesFromSubscriptionContent returned error: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("rules length = %d, want 5", len(got))
	}

	byKey := map[string]CustomRule{}
	for _, rule := range got {
		if rule.ProfileID != 7 {
			t.Fatalf("rule ProfileID = %d, want 7", rule.ProfileID)
		}
		byKey[rule.Type+"\x00"+rule.Payload] = rule
	}

	if byKey["DOMAIN-SUFFIX\x00example.com"].Target != "DIRECT" {
		t.Fatal("DOMAIN-SUFFIX rule target was not parsed")
	}
	if byKey["GEOSITE\x00cn"].Target != "DIRECT" {
		t.Fatal("GEOSITE rule target was not parsed")
	}
	if byKey["GEOIP\x00CN"].Target != "DIRECT,no-resolve" {
		t.Fatalf("GEOIP target = %q, want DIRECT,no-resolve", byKey["GEOIP\x00CN"].Target)
	}
	if byKey["AND\x00((DOMAIN,example.org),(NETWORK,UDP))"].Target != "REJECT" {
		t.Fatal("AND rule with comma payload was not parsed")
	}
	if byKey["MATCH\x00-"].Target != "代理" {
		t.Fatal("MATCH rule target was not parsed")
	}
}

func TestInjectCustomRulesKeepsCustomTerminalRuleAtEnd(t *testing.T) {
	content := `proxies: []
proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - DIRECT
rules:
  - DOMAIN-SUFFIX,qq.com,Proxy
  - MATCH,Proxy
  - DOMAIN-SUFFIX,example.com,Proxy
`

	got := injectCustomRulesWithRules(content, []CustomRule{
		{Type: "MATCH", Payload: "-", Target: "Leak"},
		{Type: "DOMAIN-SUFFIX", Payload: "qq.com", Target: "DIRECT"},
	})

	assertStringSliceEqual(t, yamlSequenceScalars(t, got, "rules"), []string{
		"DOMAIN-SUFFIX,qq.com,DIRECT",
		"DOMAIN-SUFFIX,example.com,Proxy",
		"MATCH,Leak",
	})
}

func TestInjectCustomRulesKeepsNewestCustomRulesFirst(t *testing.T) {
	content := `rules:
  - DOMAIN-SUFFIX,origin.example,DIRECT
  - MATCH,DIRECT
`

	got := injectCustomRulesWithRules(content, []CustomRule{
		{ID: 3, Type: "DOMAIN-SUFFIX", Payload: "new.example", Target: "PROXY"},
		{ID: 2, Type: "DOMAIN-SUFFIX", Payload: "old.example", Target: "DIRECT"},
	})

	assertStringSliceEqual(t, yamlSequenceScalars(t, got, "rules"), []string{
		"DOMAIN-SUFFIX,new.example,PROXY",
		"DOMAIN-SUFFIX,old.example,DIRECT",
		"DOMAIN-SUFFIX,origin.example,DIRECT",
		"MATCH,DIRECT",
	})
}

func TestInjectCustomRulesDropsOriginalRulesWithDeletedTargets(t *testing.T) {
	content := `proxies:
  - name: 可用节点
    type: ss
proxy-groups:
  - name: 可用策略
    type: select
    proxies:
      - 可用节点
rules:
  - DOMAIN-SUFFIX,keep.example,可用策略
  - DOMAIN-SUFFIX,drop.example,已删策略
  - MATCH,已删策略
`

	got := injectCustomRulesWithRules(content, nil)
	assertStringSliceEqual(t, yamlSequenceScalars(t, got, "rules"), []string{
		"DOMAIN-SUFFIX,keep.example,可用策略",
	})
}

func TestInjectCustomRulesHandlesCommaPayloadFingerprint(t *testing.T) {
	content := `rules:
  - AND,((DOMAIN,example.org),(NETWORK,UDP)),REJECT
  - GEOIP,CN,DIRECT,no-resolve
  - MATCH,DIRECT
`

	got := injectCustomRulesWithRules(content, []CustomRule{
		{Type: "AND", Payload: "((DOMAIN,example.org),(NETWORK,UDP))", Target: deletedCustomRuleTarget},
		{Type: "GEOIP", Payload: "CN", Target: "PROXY,no-resolve"},
	})

	assertStringSliceEqual(t, yamlSequenceScalars(t, got, "rules"), []string{
		"GEOIP,CN,PROXY,no-resolve",
		"MATCH,DIRECT",
	})
}

func TestFilterNewLocalizedRulesSkipsExistingManualRules(t *testing.T) {
	existing := []CustomRule{
		{ID: 1, ProfileID: 3, Type: "GEOSITE", Payload: "cn", Target: "REJECT"},
	}
	candidates := []CustomRule{
		{ProfileID: 3, Type: "GEOSITE", Payload: "cn", Target: "DIRECT"},
		{ProfileID: 3, Type: "MATCH", Payload: "-", Target: "代理"},
	}

	got, skipped := filterNewLocalizedRules(existing, candidates, 3)
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if len(got) != 1 {
		t.Fatalf("new rules length = %d, want 1", len(got))
	}
	if got[0].Type != "MATCH" || got[0].Payload != "-" || got[0].Target != "代理" {
		t.Fatalf("unexpected imported rule: %+v", got[0])
	}
}

func TestMergeSubscriptionPlainContentsKeepsPrimaryGroupsAndRules(t *testing.T) {
	primary := `proxies:
  - name: Proxy A
    type: ss
    server: primary.example.com
    port: 8388
    cipher: aes-128-gcm
    password: primary-pass
proxy-groups:
  - name: Main
    type: select
    proxies:
      - Proxy A
      - DIRECT
rules:
  - DOMAIN-SUFFIX,primary.example,Main
  - MATCH,Main
`
	secondary := `proxies:
  - name: Proxy A
    type: ss
    server: secondary-a.example.com
    port: 8388
    cipher: aes-128-gcm
    password: secondary-pass-a
  - name: Proxy B
    type: ss
    server: secondary-b.example.com
    port: 8388
    cipher: aes-128-gcm
    password: secondary-pass-b
proxy-groups:
  - name: Secondary
    type: select
    proxies:
      - Proxy B
rules:
  - DOMAIN-SUFFIX,secondary.example,Secondary
  - MATCH,Secondary
`

	got, err := mergeSubscriptionPlainContents(primary, []string{secondary})
	if err != nil {
		t.Fatalf("mergeSubscriptionPlainContents returned error: %v", err)
	}

	assertStringSliceEqual(t, yamlSequenceNames(t, got, "proxies"), []string{"Proxy A", "Proxy A 2", "Proxy B"})
	assertStringSliceEqual(t, yamlSequenceNames(t, got, "proxy-groups"), []string{"Main"})
	assertYAMLGroupProxiesEqual(t, got, "Main", []string{"Proxy A", "DIRECT", "Proxy A 2", "Proxy B"})
	assertStringSliceEqual(t, yamlSequenceScalars(t, got, "rules"), []string{
		"DOMAIN-SUFFIX,primary.example,Main",
		"MATCH,Main",
	})
}

func TestDecodeSubscriptionPlainContentPreservesMergedYAMLStructure(t *testing.T) {
	primary := `proxies:
  - name: Proxy A
    type: ss
    server: primary.example.com
    port: 8388
    cipher: aes-128-gcm
    password: primary-pass
proxy-groups:
  - name: Main
    type: select
    proxies:
      - Proxy A
      - DIRECT
rules:
  - MATCH,Main
`
	secondary := `proxies:
  - name: Proxy B
    type: ss
    server: secondary.example.com
    port: 8388
    cipher: aes-128-gcm
    password: secondary-pass
`

	merged, err := mergeSubscriptionPlainContents(primary, []string{secondary})
	if err != nil {
		t.Fatalf("mergeSubscriptionPlainContents returned error: %v", err)
	}

	got, err := decodeSubscriptionPlainContent(merged)
	if err != nil {
		t.Fatalf("decodeSubscriptionPlainContent returned error: %v", err)
	}

	assertStringSliceEqual(t, yamlSequenceNames(t, got, "proxies"), []string{"Proxy A", "Proxy B"})
	assertStringSliceEqual(t, yamlSequenceNames(t, got, "proxy-groups"), []string{"Main"})
	assertYAMLGroupProxiesEqual(t, got, "Main", []string{"Proxy A", "DIRECT", "Proxy B"})
	assertStringSliceEqual(t, yamlSequenceScalars(t, got, "rules"), []string{"MATCH,Main"})
}

func TestNormalizeProfileSourceRequestsRequiresSinglePrimary(t *testing.T) {
	req := profileWriteRequest{
		Name:       "multi",
		SourceType: profileSourceRemote,
		Sources: []profileSourceWriteRequest{
			{URL: "https://primary.example.com/sub", IsPrimary: true},
			{URL: "backup.example.com/sub"},
		},
	}

	normalized, err := validateProfileWriteRequest(req)
	if err != nil {
		t.Fatalf("validateProfileWriteRequest returned error: %v", err)
	}
	if normalized.URL != "https://primary.example.com/sub" {
		t.Fatalf("primary URL = %q, want primary source URL", normalized.URL)
	}
	if len(normalized.Sources) != 2 {
		t.Fatalf("sources length = %d, want 2", len(normalized.Sources))
	}
	if normalized.Sources[1].URL != "https://backup.example.com/sub" {
		t.Fatalf("second source URL = %q, want normalized https URL", normalized.Sources[1].URL)
	}

	req.Sources[1].IsPrimary = true
	if _, err := validateProfileWriteRequest(req); err == nil {
		t.Fatal("expected multiple primary sources to fail validation")
	}
}

func TestDecodeSubscriptionPlainContentAcceptsBase64Yaml(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("rules:\n  - MATCH,代理\n"))
	got, err := decodeSubscriptionPlainContent(raw)
	if err != nil {
		t.Fatalf("decodeSubscriptionPlainContent returned error: %v", err)
	}
	if !strings.Contains(got, "MATCH,代理") {
		t.Fatalf("decoded content = %q, want MATCH rule", got)
	}
}

func TestDecodeSubscriptionPlainContentConvertsBase64ProxyURIList(t *testing.T) {
	ssUserInfo := base64.StdEncoding.EncodeToString([]byte("aes-128-gcm:ss-pass"))
	uriList := strings.Join([]string{
		"ss://" + ssUserInfo + "@ss.example.com:8388?udp=1#SS%20One",
		"anytls://any-pass@any.example.com:443?sni=apple.com&insecure=1&udp=1#AnyTLS%20One",
		"vless://00000000-0000-0000-0000-000000000000@vless.example.com:443?security=reality&sni=apple.com&flow=xtls-rprx-vision&encryption=none&pbk=pub-key&sid=short-id&fp=ios&type=tcp&allowInsecure=1#VLESS%20One",
	}, "\n")
	raw := base64.StdEncoding.EncodeToString([]byte(uriList))

	got, err := decodeSubscriptionPlainContent(raw)
	if err != nil {
		t.Fatalf("decodeSubscriptionPlainContent returned error: %v", err)
	}

	names := yamlSequenceNames(t, got, "proxies")
	assertStringSliceEqual(t, names, []string{"SS One", "AnyTLS One", "VLESS One"})

	var cfg manualProfileConfig
	if err := yaml.Unmarshal([]byte(got), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal returned error: %v\n%s", err, got)
	}
	if len(cfg.ProxyGroups) != 1 {
		t.Fatalf("proxy groups length = %d, want 1", len(cfg.ProxyGroups))
	}
	assertStringSliceEqual(t, cfg.ProxyGroups[0].Proxies, []string{"SS One", "AnyTLS One", "VLESS One", manualDirectPolicyName})

	var rawCfg struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(got), &rawCfg); err != nil {
		t.Fatalf("yaml.Unmarshal returned error: %v\n%s", err, got)
	}
	if rawCfg.Proxies[1]["type"] != "anytls" {
		t.Fatalf("second proxy type = %v, want anytls", rawCfg.Proxies[1]["type"])
	}
	if rawCfg.Proxies[1]["skip-cert-verify"] != true {
		t.Fatalf("anytls skip-cert-verify = %v, want true", rawCfg.Proxies[1]["skip-cert-verify"])
	}
}

func TestSubscriptionFetchCandidatePrefersMoreNormalizedNodes(t *testing.T) {
	degradedYAML := `proxies:
  - name: Only SS
    type: ss
    server: ss.example.com
    port: 8388
    cipher: aes-128-gcm
    password: ss-pass
`
	uriList := strings.Join([]string{
		"anytls://pass-a@a.example.com:443#AnyTLS%20A",
		"anytls://pass-b@b.example.com:443#AnyTLS%20B",
		"vless://00000000-0000-0000-0000-000000000000@vless.example.com:443?security=tls#VLESS%20A",
	}, "\n")
	fullRaw := base64.StdEncoding.EncodeToString([]byte(uriList))

	degradedCandidate := newSubscriptionFetchCandidate("clash", degradedYAML)
	fullCandidate := newSubscriptionFetchCandidate("curl", fullRaw)

	if degradedCandidate.NodeCount != 1 {
		t.Fatalf("degraded node count = %d, want 1", degradedCandidate.NodeCount)
	}
	if fullCandidate.NodeCount != 3 {
		t.Fatalf("full node count = %d, want 3", fullCandidate.NodeCount)
	}
	if !isBetterSubscriptionFetchCandidate(fullCandidate, degradedCandidate) {
		t.Fatalf("expected URI-list candidate with more nodes to win")
	}
}

func TestGenerateProfileSubTokenCarriesProfileID(t *testing.T) {
	token := generateProfileSubToken(SubscriptionProfile{ID: 42})
	decoded, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("generated token is not URL-safe base64: %v", err)
	}
	if !strings.HasPrefix(string(decoded), "profile:42|") {
		t.Fatalf("decoded token = %q, want profile id prefix", decoded)
	}
}

func yamlSequenceNames(t *testing.T, content string, key string) []string {
	t.Helper()

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		t.Fatalf("yaml.Unmarshal returned error: %v\n%s", err, content)
	}
	if len(root.Content) == 0 {
		t.Fatalf("YAML content is empty:\n%s", content)
	}

	seq := findTopLevelSequenceNode(root.Content[0], key)
	if seq == nil {
		t.Fatalf("YAML missing sequence %q:\n%s", key, content)
	}

	names := make([]string, 0, len(seq.Content))
	for _, item := range seq.Content {
		names = append(names, yamlMappingName(item))
	}
	return names
}

func yamlSequenceScalars(t *testing.T, content string, key string) []string {
	t.Helper()

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		t.Fatalf("yaml.Unmarshal returned error: %v\n%s", err, content)
	}
	if len(root.Content) == 0 {
		t.Fatalf("YAML content is empty:\n%s", content)
	}

	seq := findTopLevelSequenceNode(root.Content[0], key)
	if seq == nil {
		t.Fatalf("YAML missing sequence %q:\n%s", key, content)
	}

	values := make([]string, 0, len(seq.Content))
	for _, item := range seq.Content {
		values = append(values, item.Value)
	}
	return values
}

func yamlProxyGroupNode(t *testing.T, content string, groupName string) *yaml.Node {
	t.Helper()

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		t.Fatalf("yaml.Unmarshal returned error: %v\n%s", err, content)
	}
	if len(root.Content) == 0 {
		t.Fatalf("YAML content is empty:\n%s", content)
	}

	seq := findTopLevelSequenceNode(root.Content[0], "proxy-groups")
	if seq == nil {
		t.Fatalf("YAML missing proxy-groups:\n%s", content)
	}
	for _, item := range seq.Content {
		if yamlMappingName(item) == groupName {
			return item
		}
	}
	t.Fatalf("YAML missing proxy group %q:\n%s", groupName, content)
	return nil
}

func yamlMappingValueNode(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func assertYAMLGroupProxiesEqual(t *testing.T, content string, groupName string, want []string) {
	t.Helper()
	groupNode := yamlProxyGroupNode(t, content, groupName)
	proxiesNode := yamlMappingValueNode(groupNode, "proxies")
	if proxiesNode == nil || proxiesNode.Kind != yaml.SequenceNode {
		t.Fatalf("group %q missing proxies sequence:\n%s", groupName, content)
	}
	got := make([]string, 0, len(proxiesNode.Content))
	for _, item := range proxiesNode.Content {
		got = append(got, item.Value)
	}
	assertStringSliceEqual(t, got, want)
}

func assertYAMLGroupScalarEqual(t *testing.T, content string, groupName string, key string, want string) {
	t.Helper()
	groupNode := yamlProxyGroupNode(t, content, groupName)
	valueNode := yamlMappingValueNode(groupNode, key)
	if valueNode == nil {
		t.Fatalf("group %q missing field %q:\n%s", groupName, key, content)
	}
	if valueNode.Value != want {
		t.Fatalf("group %q field %q = %q, want %q", groupName, key, valueNode.Value, want)
	}
}

func assertYAMLGroupFieldAbsent(t *testing.T, content string, groupName string, key string) {
	t.Helper()
	groupNode := yamlProxyGroupNode(t, content, groupName)
	if valueNode := yamlMappingValueNode(groupNode, key); valueNode != nil {
		t.Fatalf("group %q field %q should be absent, got %q", groupName, key, valueNode.Value)
	}
}

func assertStringSliceEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice length = %d, want %d; got=%v want=%v", len(got), len(want), got, want)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("slice[%d] = %q, want %q; got=%v want=%v", idx, got[idx], want[idx], got, want)
		}
	}
}
