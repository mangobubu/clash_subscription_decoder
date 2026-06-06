package main

import "testing"

func TestNormalizeBatchCustomRulesDeduplicatesByRuleIdentity(t *testing.T) {
	rules, err := normalizeBatchCustomRules(42, []customRuleWritePayload{
		{Type: " DOMAIN-SUFFIX ", Payload: " example.com ", Target: " DIRECT "},
		{Type: "MATCH", Payload: "", Target: "PROXY"},
		{Type: "DOMAIN-SUFFIX", Payload: "example.com", Target: "REJECT"},
	})
	if err != nil {
		t.Fatalf("normalizeBatchCustomRules returned error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("normalized rules length = %d, want 2", len(rules))
	}

	first := rules[0]
	if first.ProfileID != 42 || first.Type != "DOMAIN-SUFFIX" || first.Payload != "example.com" || first.Target != "REJECT" {
		t.Fatalf("deduplicated rule = %+v, want last target for DOMAIN-SUFFIX/example.com", first)
	}

	second := rules[1]
	if second.ProfileID != 42 || second.Type != "MATCH" || second.Payload != "-" || second.Target != "PROXY" {
		t.Fatalf("empty payload rule = %+v, want MATCH payload defaulted to '-'", second)
	}
}

func TestNormalizeBatchCustomRulesRejectsInvalidRule(t *testing.T) {
	_, err := normalizeBatchCustomRules(42, []customRuleWritePayload{
		{Type: "DOMAIN", Payload: "example.com", Target: ""},
	})
	if err == nil {
		t.Fatal("expected empty target to be rejected")
	}
}
