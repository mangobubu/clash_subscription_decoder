package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoadProfileRawContentUsesLocalContentWithoutFetcher(t *testing.T) {
	profile := SubscriptionProfile{
		SourceType:   profileSourceLocal,
		LocalContent: "proxies:\n  - name: local\n",
	}

	got, fetchedRemote, err := loadProfileRawContent(profile, func(string) (string, error) {
		t.Fatal("local profile must not call remote fetcher")
		return "", nil
	})

	if err != nil {
		t.Fatalf("loadProfileRawContent returned error: %v", err)
	}
	if fetchedRemote {
		t.Fatal("local profile unexpectedly marked as remote fetch")
	}
	if got != profile.LocalContent {
		t.Fatalf("loadProfileRawContent() = %q, want %q", got, profile.LocalContent)
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

func TestLooksLikePlainSubscriptionConfigAcceptsProviderOnlyYaml(t *testing.T) {
	content := "proxy-providers:\n  provider-a:\n    type: http\nproxy-groups:\n  - name: PROXY\nrules:\n  - MATCH,PROXY\n"
	if !looksLikePlainSubscriptionConfig(content) {
		t.Fatal("provider-only Clash YAML was not recognized as plain config")
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
