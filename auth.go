package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
)

// Authentication.
//
// There is one API credential (MEM_API_TOKEN) and, for compatibility, an
// optional second accepted token (MEM_STEWARD_TOKEN). Both roles can read and
// write: the wiki has no review gate, writes are active immediately.
// The web UI uses Basic auth (MEM_UI_USER / MEM_UI_PASSWORD).

type authConfig struct {
	APIToken       string
	StewardToken   string // accepted as an equivalent API token
	UIUser         string
	UIPass         string
	AllowedOrigins []string // extra origins allowed to send browser writes (reverse proxies that rewrite Host)
}

// enabled reports whether any credential is configured.
func (c authConfig) enabled() bool {
	return c.APIToken != "" || c.StewardToken != "" || c.UIPass != ""
}

// secretEqual compares two secrets in constant time without leaking their length.
// An empty secret never matches.
func secretEqual(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

func basicAuthOK(r *http.Request, cfg authConfig) bool {
	u, p, ok := r.BasicAuth()
	if !ok || cfg.UIPass == "" {
		return false
	}
	// Evaluate both comparisons so timing does not reveal which one failed.
	userOK := secretEqual(u, cfg.UIUser)
	passOK := secretEqual(p, cfg.UIPass)
	return userOK && passOK
}

// authenticate resolves whether the caller holds a valid API credential.
func authenticate(r *http.Request, cfg authConfig) bool {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		provided := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if secretEqual(provided, cfg.APIToken) || secretEqual(provided, cfg.StewardToken) {
			return true
		}
	}
	return basicAuthOK(r, cfg)
}

// originAllowed blocks cross-site browser writes (CSRF). Browsers attach an Origin
// header to cross-site POSTs; command-line clients and agents send none, so they
// are unaffected.
func originAllowed(r *http.Request, extra []string) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false // includes the opaque "null" origin
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, allowed := range extra {
		if strings.EqualFold(strings.TrimRight(origin, "/"), strings.TrimRight(allowed, "/")) {
			return true
		}
	}
	return false
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
}

type authInfo struct {
	Identity string
}

type authKey struct{}

func withAuth(ctx context.Context, info authInfo) context.Context {
	return context.WithValue(ctx, authKey{}, info)
}

func authFrom(r *http.Request) authInfo {
	info, _ := r.Context().Value(authKey{}).(authInfo)
	return info
}

func apiAuthMiddleware(cfg authConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)

		isAPI := strings.HasPrefix(r.URL.Path, "/v1/memory/") ||
			strings.HasPrefix(r.URL.Path, "/v1/wiki/") ||
			strings.HasPrefix(r.URL.Path, "/api/")
		// Everything that is neither the API nor the health probe is the web UI,
		// including paths the catch-all handler would serve the page for.
		isUI := !isAPI && r.URL.Path != "/health"

		if isUI && cfg.UIPass != "" && !basicAuthOK(r, cfg) {
			w.Header().Set("WWW-Authenticate", `Basic realm="mem"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		identity := writerIdentity(r, nil)
		if isAPI {
			w.Header().Set("Cache-Control", "no-store")
			if cfg.enabled() {
				if !authenticate(r, cfg) {
					if strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
						w.Header().Set("WWW-Authenticate", `Basic realm="mem"`)
					} else {
						w.Header().Set("WWW-Authenticate", "Bearer")
					}
					jsonResp(w, http.StatusUnauthorized, map[string]string{"error": "valid bearer token or basic auth required"})
					return
				}
			}
			if !originAllowed(r, cfg.AllowedOrigins) {
				jsonResp(w, http.StatusForbidden, map[string]string{"error": "cross-origin write rejected"})
				return
			}
			if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
				limit := int64(1 << 20)
				if r.URL.Path == "/v1/memory/restore" {
					limit = 512 << 20
				}
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
		}
		next.ServeHTTP(w, r.WithContext(withAuth(r.Context(), authInfo{Identity: identity})))
	})
}

// newNonce returns a fresh random value for a Content-Security-Policy script nonce.
func newNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return base64.RawStdEncoding.EncodeToString(b)
}

// contentSecurityPolicy only runs scripts that carry the per-response nonce, so an
// injected inline script or handler cannot execute even if escaping is missed.
func contentSecurityPolicy(nonce string) string {
	return "default-src 'none'; " +
		"script-src 'nonce-" + nonce + "'; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; " +
		"connect-src 'self'; " +
		"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
}
