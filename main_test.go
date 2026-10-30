package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHashBodyIsStableSHA256Hex(t *testing.T) {
	first := hashBody("same memory")
	if first != hashBody("same memory") {
		t.Fatal("hashBody should be deterministic")
	}
	if len(first) != 64 {
		t.Fatalf("expected a full SHA-256 hex digest (64 chars), got %d", len(first))
	}
	if first == hashBody("different memory") {
		t.Fatal("different bodies should produce different hashes")
	}
}

func TestLoopbackListenAddress(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:3200", "localhost:3200", "[::1]:3200"} {
		if !isLoopbackListenAddress(addr) {
			t.Errorf("%q should be considered loopback", addr)
		}
	}
	for _, addr := range []string{":3200", "0.0.0.0:3200", "192.0.2.10:3200"} {
		if isLoopbackListenAddress(addr) {
			t.Errorf("%q must not be considered loopback", addr)
		}
	}
}

func TestAPIAuthMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := apiAuthMiddleware("test-secret", next)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/memory/nodes", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: expected 401, got %d", unauthorized.Code)
	}

	rawToken := httptest.NewRecorder()
	rawReq := httptest.NewRequest(http.MethodGet, "/v1/memory/nodes", nil)
	rawReq.Header.Set("Authorization", "test-secret")
	handler.ServeHTTP(rawToken, rawReq)
	if rawToken.Code != http.StatusUnauthorized {
		t.Fatalf("token without Bearer scheme: expected 401, got %d", rawToken.Code)
	}

	authorized := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/memory/nodes", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	handler.ServeHTTP(authorized, req)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("valid token: expected 204, got %d", authorized.Code)
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusNoContent {
		t.Fatalf("health endpoint should not require API token, got %d", health.Code)
	}
}

func TestBulkDeleteRequiresOptInAndConfirmation(t *testing.T) {
	t.Setenv("MEM_ENABLE_BULK_DELETE", "false")
	req := httptest.NewRequest(http.MethodDelete, "/v1/memory/nodes", nil)
	req.Header.Set("X-Confirm-Delete", "all")
	if bulkDeleteAllowed(req) {
		t.Fatal("bulk delete should be disabled by default")
	}

	t.Setenv("MEM_ENABLE_BULK_DELETE", "true")
	req.Header.Del("X-Confirm-Delete")
	if bulkDeleteAllowed(req) {
		t.Fatal("bulk delete should require explicit confirmation header")
	}
	req.Header.Set("X-Confirm-Delete", "all")
	if !bulkDeleteAllowed(req) {
		t.Fatal("bulk delete should be allowed only after explicit opt-in and confirmation")
	}
}
