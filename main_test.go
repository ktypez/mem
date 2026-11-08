package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestSocketTriggeredServiceSkipsOnDemandUnits(t *testing.T) {
	sockets := map[string]bool{"mem-lan": true, "dbus": true}
	for _, unit := range []string{"mem-lan.service", "mem-lan@1.service", "dbus.service", "dbus@0.service"} {
		if !socketTriggeredService(sockets, unit) {
			t.Errorf("%q has a live socket and must not be reported as unhealthy", unit)
		}
	}
	for _, unit := range []string{"memory.service", "mem-lan.socket", "mem-lans.service", "sshd.service"} {
		if socketTriggeredService(sockets, unit) {
			t.Errorf("%q must still be reported", unit)
		}
	}
	// No sockets known (systemctl unavailable) must not silence real failures.
	if socketTriggeredService(nil, "memory.service") {
		t.Error("an empty socket set must report every unit")
	}
}

func TestAPIAuthMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := apiAuthMiddleware(authConfig{APIToken: "test-secret", UIUser: "admin", UIPass: "test-pass"}, next)

	// API without auth
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/memory/nodes", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: expected 401, got %d", unauthorized.Code)
	}

	// API with raw token (not Bearer)
	rawToken := httptest.NewRecorder()
	rawReq := httptest.NewRequest(http.MethodGet, "/v1/memory/nodes", nil)
	rawReq.Header.Set("Authorization", "test-secret")
	handler.ServeHTTP(rawToken, rawReq)
	if rawToken.Code != http.StatusUnauthorized {
		t.Fatalf("token without Bearer scheme: expected 401, got %d", rawToken.Code)
	}

	// API with valid Bearer token
	bearerAuth := httptest.NewRecorder()
	bearerReq := httptest.NewRequest(http.MethodGet, "/v1/memory/nodes", nil)
	bearerReq.Header.Set("Authorization", "Bearer test-secret")
	handler.ServeHTTP(bearerAuth, bearerReq)
	if bearerAuth.Code != http.StatusNoContent {
		t.Fatalf("valid bearer token: expected 204, got %d", bearerAuth.Code)
	}

	// API with valid Basic auth
	basicAPIAuth := httptest.NewRecorder()
	basicAPIReq := httptest.NewRequest(http.MethodGet, "/v1/memory/nodes", nil)
	basicAPIReq.SetBasicAuth("admin", "test-pass")
	handler.ServeHTTP(basicAPIAuth, basicAPIReq)
	if basicAPIAuth.Code != http.StatusNoContent {
		t.Fatalf("valid basic auth for API: expected 204, got %d", basicAPIAuth.Code)
	}

	// UI without Basic auth
	uiUnauthorized := httptest.NewRecorder()
	handler.ServeHTTP(uiUnauthorized, httptest.NewRequest(http.MethodGet, "/", nil))
	if uiUnauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("UI without auth: expected 401, got %d", uiUnauthorized.Code)
	}
	if authHeader := uiUnauthorized.Header().Get("WWW-Authenticate"); authHeader != `Basic realm="mem"` {
		t.Fatalf("expected Basic realm header, got %q", authHeader)
	}

	// UI with valid Basic auth
	uiAuthorized := httptest.NewRecorder()
	uiReq := httptest.NewRequest(http.MethodGet, "/", nil)
	uiReq.SetBasicAuth("admin", "test-pass")
	handler.ServeHTTP(uiAuthorized, uiReq)
	if uiAuthorized.Code != http.StatusNoContent {
		t.Fatalf("valid basic auth for UI: expected 204, got %d", uiAuthorized.Code)
	}

	// Health check without auth
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusNoContent {
		t.Fatalf("health endpoint should not require auth, got %d", health.Code)
	}
}

func TestEveryAPIAliasRequiresAuth(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := apiAuthMiddleware(authConfig{APIToken: "test-secret", UIUser: "admin", UIPass: "test-pass"}, next)

	// Every path that reaches an API handler must be gated, including the
	// /api/ aliases added alongside the steward endpoints.
	gated := []string{
		"/v1/memory/nodes", "/v1/memory/nodes/abc/promote", "/v1/memory/nodes/abc/reject",
		"/v1/memory/brief", "/v1/memory/inbox", "/v1/memory/stats",
		"/api/nodes", "/api/nodes/abc/promote", "/api/brief", "/api/inbox",
	}
	for _, path := range gated {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without credentials: expected 401, got %d", path, rec.Code)
		}

		authorized := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer test-secret")
		handler.ServeHTTP(authorized, req)
		if authorized.Code != http.StatusNoContent {
			t.Errorf("%s with valid token: expected 204, got %d", path, authorized.Code)
		}
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

func TestWriterIdentityPrefersHeaderThenBodyThenAgent(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/memory/nodes", nil)
	if got := writerIdentity(req, nil); got != "agent" {
		t.Fatalf("anonymous writer: expected agent, got %q", got)
	}

	body := map[string]interface{}{"source": "steward"}
	if got := writerIdentity(req, body); got != "steward" {
		t.Fatalf("body source: expected steward, got %q", got)
	}

	req.Header.Set("X-Mem-Source", "hermes")
	if got := writerIdentity(req, body); got != "hermes" {
		t.Fatalf("header should win over body: expected hermes, got %q", got)
	}
}

func TestStatusForWriterGatesByStewardIdentity(t *testing.T) {
	t.Setenv("MEM_STEWARD_SOURCE", "steward")
	if got := statusForWriter("steward"); got != statusActive {
		t.Fatalf("steward writes must be active, got %q", got)
	}
	for _, identity := range []string{"agent", "hermes", "system", ""} {
		if got := statusForWriter(identity); got != statusProposed {
			t.Fatalf("writer %q must land in the inbox as proposed, got %q", identity, got)
		}
	}
}

func TestStewardSourceAcceptsTrustedList(t *testing.T) {
	t.Setenv("MEM_STEWARD_SOURCE", " steward , hermes ")
	if got := stewardSource(); got != "steward,hermes" {
		t.Fatalf("display form should be normalized: got %q", got)
	}
	for _, identity := range []string{"steward", "hermes"} {
		if got := statusForWriter(identity); got != statusActive {
			t.Fatalf("listed writer %q must write active, got %q", identity, got)
		}
	}
	for _, identity := range []string{"hermes2", "agent", "default", "steward ", ""} {
		if got := statusForWriter(identity); got != statusProposed {
			t.Fatalf("writer %q is not listed and must be proposed, got %q", identity, got)
		}
	}

	// A value of only separators must not open the gate or panic.
	t.Setenv("MEM_STEWARD_SOURCE", " , ")
	if got := stewardSource(); got != defaultStewardSource {
		t.Fatalf("empty list should fall back to the default steward, got %q", got)
	}
	if got := statusForWriter("agent"); got != statusProposed {
		t.Fatalf("empty list must not trust agent, got %q", got)
	}
}

func TestStewardSourceDefaultAndOverride(t *testing.T) {
	t.Setenv("MEM_STEWARD_SOURCE", "")
	if got := stewardSource(); got != "steward" {
		t.Fatalf("default steward source: expected steward, got %q", got)
	}
	t.Setenv("MEM_STEWARD_SOURCE", "  mcky  ")
	if got := stewardSource(); got != "mcky" {
		t.Fatalf("override steward source should be trimmed, got %q", got)
	}
}

func TestFactRefreshInterval(t *testing.T) {
	t.Setenv("MEM_FACTS_REFRESH_MINUTES", "")
	if got := factRefreshInterval(); got != 15*time.Minute {
		t.Fatalf("default refresh interval: expected 15m, got %v", got)
	}
	t.Setenv("MEM_FACTS_REFRESH_MINUTES", "3")
	if got := factRefreshInterval(); got != 3*time.Minute {
		t.Fatalf("configured refresh interval: expected 3m, got %v", got)
	}
	for _, bad := range []string{"0", "-4", "soon"} {
		t.Setenv("MEM_FACTS_REFRESH_MINUTES", bad)
		if got := factRefreshInterval(); got != 15*time.Minute {
			t.Fatalf("invalid interval %q must fall back to 15m, got %v", bad, got)
		}
	}
}

func TestSetupFactPatternMatchesLiveSystemFactsOnly(t *testing.T) {
	mustMatch := []string{
		"mem service listens on port 3200",
		"memory.service runs as a systemd unit",
		"/etc/mem/mem.env holds the DATABASE_URL",
		"dashboard is published at 192.168.1.24",
		"cron job refreshes models every 11 minutes",
	}
	for _, body := range mustMatch {
		if !setupFactPattern.MatchString(body) {
			t.Errorf("expected setup fact to match: %q", body)
		}
	}
	mustNotMatch := []string{
		"user prefers concise responses",
		"mcky communicates in Thai",
		"the steward decides what becomes trusted knowledge",
	}
	for _, body := range mustNotMatch {
		if setupFactPattern.MatchString(body) {
			t.Errorf("preference must not be treated as a setup fact: %q", body)
		}
	}
}

func TestNodeRowsFromBackupKeepsStewardshipAndDefaultsLegacyRows(t *testing.T) {
	rows := nodeRowsFromBackup([]Node{
		{ID: "a", Body: "legacy row"},
		{ID: "b", Body: "system row", Source: "system", Status: statusActive, FactKey: "system:service:memory.service"},
		{ID: "c", Body: "proposed row", Source: "agent", Status: statusProposed},
	})
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	for i, row := range rows {
		if len(row) != len(restoredNodeColumns) {
			t.Fatalf("row %d has %d values but %d columns", i, len(row), len(restoredNodeColumns))
		}
	}

	// Legacy backups had no source/status: they must land somewhere known.
	if rows[0][12] != "agent" || rows[0][13] != statusActive {
		t.Fatalf("legacy row should default to agent/active, got %v/%v", rows[0][12], rows[0][13])
	}
	if rows[0][14] != nil {
		t.Fatalf("legacy row has no fact_key: expected nil (not empty string), got %v", rows[0][14])
	}
	if rows[1][13] != statusActive || rows[1][14] != "system:service:memory.service" {
		t.Fatalf("system fact must survive a round-trip, got %v/%v", rows[1][13], rows[1][14])
	}
	if rows[2][13] != statusProposed {
		t.Fatalf("proposed node must stay proposed, got %v", rows[2][13])
	}
}

func TestParseFreeReadsAvailableMemory(t *testing.T) {
	out := "              total        used        free      shared  buff/cache   available\nMem:        5413284     2741180      402608      121380     2269496     2424384\nSwap:             0           0           0\n"
	total, avail, ok := parseFree(out)
	if !ok {
		t.Fatal("expected Mem: line to parse")
	}
	if total != 5413284 || avail != 2424384 {
		t.Fatalf("got total=%d avail=%d", total, avail)
	}
	if _, _, ok := parseFree("no memory line here"); ok {
		t.Fatal("garbage input must not parse")
	}
	if got := humanBytes(2424384); got != "2.3 MiB" {
		t.Fatalf("humanBytes: got %q", got)
	}
}

func TestFirstWords(t *testing.T) {
	if got := firstWords("a b c d e f g", 3); got != "a b c" {
		t.Fatalf("got %q", got)
	}
	if got := firstWords("short", 6); got != "short" {
		t.Fatalf("got %q", got)
	}
}
