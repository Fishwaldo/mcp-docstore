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
// returns 401 {"error":"invalid_token"}; a token that verifies but cannot be resolved to a
// tenant/user (auth.ErrIdentityRejected) returns 403 {"error":"no_access"} — the caller proved
// who they are but isn't provisioned for any tenant. See publicAPIPaths for the two exempt
// GET paths.
func (s *Server) RequireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && publicAPIPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		const prefix = "Bearer "
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, prefix) {
			writeAuthError(w, http.StatusUnauthorized, "invalid_token", "")
			return
		}
		rawToken := strings.TrimSpace(strings.TrimPrefix(authz, prefix))
		if rawToken == "" {
			writeAuthError(w, http.StatusUnauthorized, "invalid_token", "")
			return
		}

		id, err := auth.VerifyRequestIdentity(r.Context(), s.verifier, s.resolver, s.store, rawToken)
		if err != nil {
			if errors.Is(err, auth.ErrIdentityRejected) {
				writeAuthError(w, http.StatusForbidden, "no_access", "authenticated but not authorized for any tenant")
				return
			}
			writeAuthError(w, http.StatusUnauthorized, "invalid_token", "")
			return
		}

		ctx := context.WithValue(r.Context(), identityCtxKey, *id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
