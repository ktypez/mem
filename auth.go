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

// Authentication and authorization.
//
// There are two roles:
//
//   - agent   : holds MEM_API_TOKEN. May read and write, but writes always land in
//     the review inbox as "proposed".
//   - steward : holds MEM_STEWARD_TOKEN, or signs in to the web UI with the Basic
//     credentials. May write active nodes, review the inbox, and restore backups.
//
// When MEM_STEWARD_TOKEN is not configured the service runs in legacy mode: the
// X-Mem-Source header is trusted as-is, so any API client can claim a steward
// identity. That is kept only so existing deployments keep working; it is not a
// security boundary and the server logs a warning at startup.

type role int

const (
	roleNone    role = iota // no authentication configured (local development only)
	roleAgent               // MEM_API_TOKEN
	roleSteward             // MEM_STEWARD_TOKEN or web UI Basic credentials
)

type authConfig struct {
	APIToken       string
	StewardToken   string
	UIUser         string
	UIPass         string
	AllowedOrigins []string // extra origins allowed to send browser writes (reverse proxies that rewrite Host)
}

// enabled reports whether any credential is configured.
func (c authConfig) enabled() bool {
	return c.APIToken != "" || c.StewardToken != "" || c.UIPass != ""
}

// strictStewardMode reports whether trust comes from credentials rather than from
// the self-declared X-Mem-Source header.
func (c authConfig) strictStewardMode() bool {
	return c.StewardToken != ""
}

type authInfo struct {
	Role   role
	Strict bool
}

type authKey struct{}

func withAuth(ctx context.Context, info authInfo) context.Context {
	return context.WithValue(ctx, authKey{}, info)
}

func authFrom(r *http.Request) authInfo {
	info, _ := r.Context().Value(authKey{}).(authInfo)
	return info
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

// authenticate resolves the caller's role from the request credentials.
func authenticate(r *http.Request, cfg authConfig) (role, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		provided := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if secretEqual(provided, cfg.StewardToken) {
			return roleSteward, true
		}
		if secretEqual(provided, cfg.APIToken) {
			return roleAgent, true
		}
	}
	if basicAuthOK(r, cfg) {
		return roleSteward, true
	}
	return roleNone, false
}

// canWriteTrusted reports whether the request may create active nodes and review
// the inbox. identity is the writer's declared source label.
func canWriteTrusted(r *http.Request, identity string) bool {
	info := authFrom(r)
	if info.Role == roleSteward {
		return true
	}
	if info.Strict {
		return false
	}
	// Legacy mode: the declared identity is the only signal there is.
	return isStewardIdentity(identity)
}

// requireSteward answers 403 unless the caller may act as the steward.
func requireSteward(w http.ResponseWriter, r *http.Request) bool {
	if canWriteTrusted(r, writerIdentity(r, nil)) {
		return true
	}
	jsonResp(w, http.StatusForbidden, map[string]string{"error": "steward credentials required"})
	return false
}

// resolveWriter decides the stored source label and the status of a write.
func resolveWriter(r *http.Request, data map[string]interface{}) (identity, status string) {
	identity = writerIdentity(r, data)
	if canWriteTrusted(r, identity) {
		if identity == "agent" && authFrom(r).Role == roleSteward {
			// Authenticated steward with no explicit label (for example the web UI).
			identity = stewardSources()[0]
		}
		return identity, statusActive
	}
	if authFrom(r).Strict && isStewardIdentity(identity) {
		// The caller claimed a steward label without steward credentials. Keep the
		// write but mark the label so nobody mistakes it for the real steward.
		identity = "unverified:" + identity
	}
	return identity, statusProposed
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

func apiAuthMiddleware(cfg authConfig, next http.Handler) http.Handler {
	strict := cfg.strictStewardMode()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)

		isAPI := strings.HasPrefix(r.URL.Path, "/v1/memory/") || strings.HasPrefix(r.URL.Path, "/api/")
		// Everything that is neither the API nor the health probe is the web UI,
		// including paths the catch-all handler would serve the page for.
		isUI := !isAPI && r.URL.Path != "/health"

		if isUI && cfg.UIPass != "" && !basicAuthOK(r, cfg) {
			w.Header().Set("WWW-Authenticate", `Basic realm="mem"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		info := authInfo{Strict: strict}
		if isAPI {
			w.Header().Set("Cache-Control", "no-store")
			if cfg.enabled() {
				got, ok := authenticate(r, cfg)
				if !ok {
					if strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
						w.Header().Set("WWW-Authenticate", `Basic realm="mem"`)
					} else {
						w.Header().Set("WWW-Authenticate", "Bearer")
					}
					jsonResp(w, http.StatusUnauthorized, map[string]string{"error": "valid bearer token or basic auth required"})
					return
				}
				info.Role = got
			}
			if !originAllowed(r, cfg.AllowedOrigins) {
				jsonResp(w, http.StatusForbidden, map[string]string{"error": "cross-origin write rejected"})
				return
			}
			if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
				limit := int64(1 << 20)
				if r.URL.Path == "/v1/memory/restore" {
					limit = 100 << 20
				}
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
		}
		next.ServeHTTP(w, r.WithContext(withAuth(r.Context(), info)))
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
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src https://fonts.gstatic.com; " +
		"img-src 'self' data:; " +
		"connect-src 'self'; " +
		"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
}
