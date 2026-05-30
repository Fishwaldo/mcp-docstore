package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Fishwaldo/mcp-docstore/internal/ent/user"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

// Middleware authenticates each request: it verifies the bearer token, resolves the
// caller's email to a tenant, upserts the user, and injects a store.Identity into the
// context. metadataURL is advertised in the WWW-Authenticate header on 401 so MCP
// clients can discover the authorization server.
func Middleware(v Verifier, resolver *tenant.Resolver, st *store.Store, metadataURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearerToken(r)
			if !ok {
				unauthorized(w, metadataURL)
				return
			}
			claims, err := v.Verify(r.Context(), raw)
			if err != nil || claims == nil {
				unauthorized(w, metadataURL)
				return
			}

			key, ok := resolver.Resolve(claims.Email)
			if !ok {
				http.Error(w, "no tenant for email domain", http.StatusForbidden)
				return
			}
			ten, err := st.TenantByKey(r.Context(), key)
			if err != nil {
				// Not-found = the resolved tenant isn't provisioned (client problem → 403);
				// anything else is a server/DB fault → 500. Either way, never call next.
				if errors.Is(err, store.ErrNotFound) {
					http.Error(w, "tenant not provisioned", http.StatusForbidden)
				} else {
					http.Error(w, "internal error", http.StatusInternalServerError)
				}
				return
			}
			usr, err := st.UpsertUser(r.Context(), ten.ID, claims.Subject, claims.Email)
			if err != nil {
				// ErrInvalid = subject already bound to a different tenant (a client
				// problem → 403). Anything else is a server/DB fault → 500. Either way
				// we never call next.
				if errors.Is(err, store.ErrInvalid) {
					http.Error(w, "identity rejected", http.StatusForbidden)
				} else {
					http.Error(w, "internal error", http.StatusInternalServerError)
				}
				return
			}

			id := store.Identity{
				TenantID: ten.ID,
				UserID:   usr.ID,
				Groups:   claims.Groups,
				IsAdmin:  usr.Role == user.RoleAdmin,
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(prefix):])
	return tok, tok != ""
}

func unauthorized(w http.ResponseWriter, metadataURL string) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata=%q`, metadataURL))
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
