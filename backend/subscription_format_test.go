package main

import "testing"

func TestIsSupportedSubscriptionFormat(t *testing.T) {
	supported := []string{"clash", "shadowrocket", "surge", "surge-5.7.6"}
	for _, format := range supported {
		if !isSupportedSubscriptionFormat(format) {
			t.Fatalf("expected format %q to be supported", format)
		}
	}

	unsupported := []string{"", "surge5", "loon"}
	for _, format := range unsupported {
		if isSupportedSubscriptionFormat(format) {
			t.Fatalf("expected format %q to be rejected", format)
		}
	}
}

func TestIsBackendOnlyPath(t *testing.T) {
	backendOnlyPaths := []string{
		"/api/verify",
		"/sub",
		"/sub/path",
		"/surge.conf",
		"/surge-5.7.6.conf",
		"/shadowrocket.conf",
		"/shadowrocket/install",
	}
	for _, path := range backendOnlyPaths {
		if !isBackendOnlyPath(path) {
			t.Fatalf("expected path %q to be backend-only", path)
		}
	}

	frontendControlledPaths := []string{"/", "/nodes", "/settings"}
	for _, path := range frontendControlledPaths {
		if isBackendOnlyPath(path) {
			t.Fatalf("expected path %q to be handled by the strict frontend route handler", path)
		}
	}
}
