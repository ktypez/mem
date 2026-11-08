package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// echoHandler answers 204 only when the caller may act as the steward.
func echoHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requireSteward(w, r) {
			w.WriteHeader(http.StatusNoContent)
		}
	})
}

func strictConfig() authConfig {
	return authConfig{APIToken: "agent-secret", StewardToken: "steward-secret", UIUser: "admin", UIPass: "ui-pass"}
}

func do(h http.Handler, method, path string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDefaultListenAddrIsLoopback(t *testing.T) {
	addr := defaultListenAddr()
	if !isLoopbackListenAddress(addr) {
		t.Fatalf("default listen address %q must be loopback", addr)
	}
	if strings.ContainsAny(addr, "[]") {
		t.Fatalf("default listen address %q looks like an unresolved placeholder", addr)
	}
}

func TestSecretEqual(t *testing.T) {
	if !secretEqual("abc", "abc") {
		t.Fatal("equal secrets must match")
	}
	if secretEqual("abc", "abd") || secretEqual("abc", "abcd") {
		t.Fatal("different secrets must not match")
	}
	if secretEqual("", "") {
		t.Fatal("empty secrets must never match")
	}
}

func TestStrictModeOnlyStewardCredentialsPromote(t *testing.T) {
	h := apiAuthMiddleware(strictConfig(), echoHandler(t))

	agent := do(h, http.MethodPost, "/v1/memory/nodes/abc/promote", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer agent-secret")
	})
	if agent.Code != http.StatusForbidden {
		t.Fatalf("agent token: expected 403, got %d", agent.Code)
	}

	// Declaring a steward identity must not help in strict mode.
	t.Setenv("MEM_STEWARD_SOURCE", "steward,hermes")
	spoof := do(h, http.MethodPost, "/v1/memory/nodes/abc/promote", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer agent-secret")
		r.Header.Set("X-Mem-Source", "steward")
	})
	if spoof.Code != http.StatusForbidden {
		t.Fatalf("spoofed steward header: expected 403, got %d", spoof.Code)
	}

	steward := do(h, http.MethodPost, "/v1/memory/nodes/abc/promote", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer steward-secret")
	})
	if steward.Code != http.StatusNoContent {
		t.Fatalf("steward token: expected 204, got %d", steward.Code)
	}

	ui := do(h, http.MethodPost, "/v1/memory/nodes/abc/promote", func(r *http.Request) {
		r.SetBasicAuth("admin", "ui-pass")
	})
	if ui.Code != http.StatusNoContent {
		t.Fatalf("web UI credentials act as steward: expected 204, got %d", ui.Code)
	}
}

func TestLegacyModeTrustsDeclaredStewardIdentity(t *testing.T) {
	t.Setenv("MEM_STEWARD_SOURCE", "steward")
	h := apiAuthMiddleware(authConfig{APIToken: "agent-secret"}, echoHandler(t))

	plain := do(h, http.MethodPost, "/v1/memory/nodes/abc/promote", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer agent-secret")
	})
	if plain.Code != http.StatusForbidden {
		t.Fatalf("anonymous writer: expected 403, got %d", plain.Code)
	}
	declared := do(h, http.MethodPost, "/v1/memory/nodes/abc/promote", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer agent-secret")
		r.Header.Set("X-Mem-Source", "steward")
	})
	if declared.Code != http.StatusNoContent {
		t.Fatalf("legacy mode honours the declared steward identity, got %d", declared.Code)
	}
}

func TestResolveWriter(t *testing.T) {
	t.Setenv("MEM_STEWARD_SOURCE", "steward,hermes")
	newReq := func(info authInfo, source string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/v1/memory/nodes", nil)
		if source != "" {
			req.Header.Set("X-Mem-Source", source)
		}
		return req.WithContext(withAuth(req.Context(), info))
	}

	// Strict mode: credentials decide, labels are only labels.
	identity, status := resolveWriter(newReq(authInfo{Role: roleAgent, Strict: true}, "hermes"), nil)
	if status != statusProposed || identity != "unverified:hermes" {
		t.Fatalf("strict agent claiming hermes: got %q/%q", identity, status)
	}
	identity, status = resolveWriter(newReq(authInfo{Role: roleAgent, Strict: true}, "worker"), nil)
	if status != statusProposed || identity != "worker" {
		t.Fatalf("strict agent: got %q/%q", identity, status)
	}
	identity, status = resolveWriter(newReq(authInfo{Role: roleSteward, Strict: true}, ""), nil)
	if status != statusActive || identity != "steward" {
		t.Fatalf("strict steward without label: got %q/%q", identity, status)
	}
	identity, status = resolveWriter(newReq(authInfo{Role: roleSteward, Strict: true}, "hermes"), nil)
	if status != statusActive || identity != "hermes" {
		t.Fatalf("strict steward with label: got %q/%q", identity, status)
	}

	// Legacy mode keeps the old behaviour.
	_, status = resolveWriter(newReq(authInfo{Role: roleAgent}, "hermes"), nil)
	if status != statusActive {
		t.Fatalf("legacy declared steward should be active, got %q", status)
	}
	_, status = resolveWriter(newReq(authInfo{Role: roleAgent}, "worker"), nil)
	if status != statusProposed {
		t.Fatalf("legacy anonymous writer should be proposed, got %q", status)
	}
}

func TestCrossOriginWritesAreRejected(t *testing.T) {
	cfg := authConfig{APIToken: "t", AllowedOrigins: []string{"https://mem.example.com/"}}
	h := apiAuthMiddleware(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	withOrigin := func(origin string) func(*http.Request) {
		return func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer t")
			r.Host = "mem.lan:3200"
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
		}
	}
	cases := []struct {
		name, method, origin string
		want                 int
	}{
		{"no origin (curl, agents)", http.MethodPost, "", http.StatusNoContent},
		{"same origin", http.MethodPost, "http://mem.lan:3200", http.StatusNoContent},
		{"other site", http.MethodPost, "https://evil.example", http.StatusForbidden},
		{"opaque origin", http.MethodPost, "null", http.StatusForbidden},
		{"allow-listed proxy origin", http.MethodPost, "https://mem.example.com", http.StatusNoContent},
		{"cross-site delete", http.MethodDelete, "https://evil.example", http.StatusForbidden},
		{"cross-site read is not a write", http.MethodGet, "https://evil.example", http.StatusNoContent},
	}
	for _, c := range cases {
		rec := do(h, c.method, "/v1/memory/nodes", withOrigin(c.origin))
		if rec.Code != c.want {
			t.Errorf("%s: expected %d, got %d", c.name, c.want, rec.Code)
		}
	}
}

func TestUIPathsOtherThanAPIRequireBasicAuth(t *testing.T) {
	h := apiAuthMiddleware(authConfig{APIToken: "t", UIUser: "admin", UIPass: "p"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, path := range []string{"/", "/graph", "/index.html", "/anything/else"} {
		if rec := do(h, http.MethodGet, path, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without credentials: expected 401, got %d", path, rec.Code)
		}
	}
	if rec := do(h, http.MethodGet, "/health", nil); rec.Code != http.StatusNoContent {
		t.Errorf("/health stays open for probes, got %d", rec.Code)
	}
}

func TestRestoreNeedsStewardAndExplicitConfirmation(t *testing.T) {
	h := apiAuthMiddleware(strictConfig(), http.HandlerFunc(handleRestore))

	agent := do(h, http.MethodPost, "/v1/memory/restore", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer agent-secret")
		r.Header.Set("X-Confirm-Restore", "replace-all")
	})
	if agent.Code != http.StatusForbidden {
		t.Fatalf("agent restore: expected 403, got %d", agent.Code)
	}

	unconfirmed := do(h, http.MethodPost, "/v1/memory/restore", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer steward-secret")
	})
	if unconfirmed.Code != http.StatusForbidden {
		t.Fatalf("restore without confirmation header: expected 403, got %d", unconfirmed.Code)
	}

	// An empty payload must be rejected before the database is touched.
	empty := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/memory/restore", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer steward-secret")
	req.Header.Set("X-Confirm-Restore", "replace-all")
	h.ServeHTTP(empty, req)
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty restore payload: expected 400, got %d (%s)", empty.Code, empty.Body.String())
	}
}

func TestHandleUISetsNonceBasedCSP(t *testing.T) {
	for _, path := range []string{"/", "/graph"} {
		rec := do(http.HandlerFunc(handleUI), http.MethodGet, path, nil)
		csp := rec.Header().Get("Content-Security-Policy")
		start := strings.Index(csp, "'nonce-")
		if start < 0 {
			t.Fatalf("%s: CSP has no nonce: %q", path, csp)
		}
		nonce := csp[start+len("'nonce-") : start+strings.Index(csp[start:], "';")]
		body := rec.Body.String()
		if strings.Contains(body, "__CSP_NONCE__") {
			t.Fatalf("%s: nonce placeholder was not replaced", path)
		}
		if strings.Count(body, `nonce="`+nonce+`"`) != strings.Count(body, "<script") {
			t.Fatalf("%s: every <script> must carry the response nonce", path)
		}
		scriptSrc := strings.SplitN(strings.SplitN(csp, "script-src ", 2)[1], ";", 2)[0]
		if strings.Contains(scriptSrc, "unsafe-inline") || strings.Contains(scriptSrc, "unsafe-eval") {
			t.Fatalf("%s: script-src must only allow the nonce: %q", path, scriptSrc)
		}
		if !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Fatalf("%s: CSP must forbid framing: %q", path, csp)
		}
	}
	a, b := newNonce(), newNonce()
	if a == b {
		t.Fatal("nonces must differ between responses")
	}
}

func TestGraphPageDoesNotRenderStoredDataAsHTML(t *testing.T) {
	// Node bodies and layer names are attacker-influenced. The graph page must
	// build its DOM with textContent/text nodes and must not use inline handlers.
	for _, banned := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "onclick=", "onerror=", "onload="} {
		if strings.Contains(graphHTML, banned) {
			t.Errorf("graph page contains %q", banned)
		}
	}
}
