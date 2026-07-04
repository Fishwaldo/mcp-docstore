// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
)

type ctxKey int

const identityCtxKey ctxKey = iota

// IdentityFromContext returns the identity RequireBearer resolved for this request.
func IdentityFromContext(ctx context.Context) (store.Identity, bool) {
	id, ok := ctx.Value(identityCtxKey).(store.Identity)
	return id, ok
}

// publicAPIPaths are the exact GET paths under /api that RequireBearer lets through without a
// bearer token: Huma's own OpenAPI document and its docs UI. The docs page is a static HTML
// shell (Stoplight Elements, loaded from a CDN) that itself fetches the spec client-side with
// no credentials, so both must be reachable unauthenticated for the page to render anything.
// Every other /api path — including a non-GET request to one of these two — still requires a
// bearer token.
var publicAPIPaths = map[string]bool{
	"/api/openapi.json": true,
	"/api/docs":         true,
}

// writeAuthError writes a JSON error body of the shape {"error": code} (plus
// error_description when non-empty), matching the failure format the SPA/API clients expect.
func writeAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]string{"error": code}
	if description != "" {
		body["error_description"] = description
	}
	_ = json.NewEncoder(w).Encode(body)
}

// RequireBearer authenticates a request from its Authorization: Bearer header by running it
// through auth.VerifyRequestIdentity — the exact verify-then-resolve pipeline the /mcp
// transport uses — so /api and /mcp trust identically-issued tokens. On success the resolved
// identity is attached to the request context under the same key the API handlers already read
// via IdentityFromContext. A missing/malformed header or a token that fails verification
// returns 401 {"error":"invalid_token"}. A token that verifies but cannot be resolved to a
// tenant/user is split by *auth.IdentityError.Err: a nil Err means a genuine rejection (no
// tenant mapping, not onboarded) and returns 403 {"error":"no_access"} — the caller proved who
// they are but isn't provisioned for any tenant; a non-nil Err means the rejection was actually
// an infrastructure fault (e.g. a DB error during the tenant/user lookup) and returns 500, since
// the caller may well be provisioned and the SPA should retry rather than render a terminal
// "no access" screen. Every failure path is logged (WARN for auth failures, ERROR for infra
// faults) with the client IP and a stable reason, mirroring auth.NewResourceVerifier's handling
// of the same pipeline for /mcp. See publicAPIPaths for the two exempt GET paths.
func (s *Server) RequireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && publicAPIPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		ip := auth.ClientIP(r, "")

		const prefix = "Bearer "
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, prefix) {
			s.log.WarnContext(r.Context(), "auth failed", "reason", "missing_bearer_header", "client_ip", ip)
			writeAuthError(w, http.StatusUnauthorized, "invalid_token", "")
			return
		}
		rawToken := strings.TrimSpace(strings.TrimPrefix(authz, prefix))
		if rawToken == "" {
			s.log.WarnContext(r.Context(), "auth failed", "reason", "empty_bearer_token", "client_ip", ip)
			writeAuthError(w, http.StatusUnauthorized, "invalid_token", "")
			return
		}

		id, err := auth.VerifyRequestIdentity(r.Context(), s.verifier, s.resolver, s.store, rawToken)
		if err != nil {
			var ie *auth.IdentityError
			if errors.Is(err, auth.ErrIdentityRejected) && errors.As(err, &ie) {
				if ie.Err != nil {
					s.log.ErrorContext(r.Context(), "auth error", "reason", ie.Reason, "client_ip", ip, "error", ie.Err)
					writeAuthError(w, http.StatusInternalServerError, "server_error", "identity resolution failed")
					return
				}
				s.log.WarnContext(r.Context(), "auth failed", "reason", ie.Reason, "client_ip", ip)
				writeAuthError(w, http.StatusForbidden, "no_access", "authenticated but not authorized for any tenant")
				return
			}
			s.log.WarnContext(r.Context(), "auth failed", "reason", "token_invalid", "client_ip", ip)
			writeAuthError(w, http.StatusUnauthorized, "invalid_token", "")
			return
		}

		ctx := context.WithValue(r.Context(), identityCtxKey, *id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
