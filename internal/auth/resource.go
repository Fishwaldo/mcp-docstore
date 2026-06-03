// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Fishwaldo/mcp-docstore/internal/ent/user"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

// identityKey is the TokenInfo.Extra key under which the resolved store.Identity is stored.
const identityKey = "docstore.identity"

// NewResourceVerifier adapts our OIDC verifier + tenant resolver + store into the SDK's
// auth.TokenVerifier. On every request it re-verifies the token and re-resolves identity,
// so token expiry and the groups claim are always current (revoked group access takes
// effect on the next request). This is a deliberate trade-off: the per-request cost is one
// signature verification plus a single indexed external_subject lookup (UpsertUser keys on
// the OIDC subject), which is cheap relative to never noticing a revoked or expired token.
// A bounded TTL identity cache to amortize the lookup is intentionally deferred until the
// per-request cost is shown to matter; adding one now would reintroduce a staleness window
// for revoked access. Any identity-resolution failure is wrapped as mcpauth.ErrInvalidToken
// so RequireBearerToken returns 401 with the resource-metadata challenge (we intentionally
// collapse the finer 401/403 distinction into 401 to use the SDK middleware; the challenge
// still guides the client). It logs auth success at DEBUG and failures at WARN/ERROR with
// the client IP and a stable reason field; the raw token is never logged.
func NewResourceVerifier(v Verifier, resolver *tenant.Resolver, st *store.Store, log *slog.Logger, ipHeader string) mcpauth.TokenVerifier {
	if log == nil {
		log = slog.Default()
	}
	return func(ctx context.Context, rawToken string, r *http.Request) (*mcpauth.TokenInfo, error) {
		ip := ClientIP(r, ipHeader)
		claims, err := v.Verify(ctx, rawToken)
		if err != nil || claims == nil {
			log.WarnContext(ctx, "auth failed", "reason", "token_invalid", "client_ip", ip)
			return nil, fmt.Errorf("%w: %v", mcpauth.ErrInvalidToken, err)
		}
		key, ok := resolver.Resolve(claims.Email)
		if !ok {
			log.WarnContext(ctx, "auth failed", "reason", "email_not_onboarded", "email", claims.Email, "client_ip", ip)
			return nil, fmt.Errorf("%w: email not onboarded", mcpauth.ErrInvalidToken)
		}
		ten, err := st.TenantByKey(ctx, key)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				log.WarnContext(ctx, "auth failed", "reason", "tenant_not_provisioned", "email", claims.Email, "client_ip", ip)
				return nil, fmt.Errorf("%w: tenant not provisioned", mcpauth.ErrInvalidToken)
			}
			log.ErrorContext(ctx, "auth error", "reason", "db_error", "email", claims.Email, "client_ip", ip, "error", err)
			return nil, err // DB fault -> 500 via the SDK middleware
		}
		usr, err := st.UpsertUser(ctx, ten.ID, claims.Subject, claims.Email, resolver.IsAdmin(key, claims.Email))
		if err != nil {
			if errors.Is(err, store.ErrInvalid) {
				log.WarnContext(ctx, "auth failed", "reason", "identity_rejected", "email", claims.Email, "client_ip", ip)
				return nil, fmt.Errorf("%w: identity rejected", mcpauth.ErrInvalidToken)
			}
			log.ErrorContext(ctx, "auth error", "reason", "db_error", "email", claims.Email, "client_ip", ip, "error", err)
			return nil, err
		}
		id := store.Identity{
			TenantID: ten.ID,
			UserID:   usr.ID,
			Groups:   claims.Groups,
			IsAdmin:  usr.Role == user.RoleAdmin,
		}
		log.DebugContext(ctx, "auth ok", "tenant", key, "user", usr.ID.String(), "admin", id.IsAdmin, "client_ip", ip)
		return NewTokenInfo(usr.ID.String(), claims.Expiry, id, ip), nil
	}
}

// NewTokenInfo builds the SDK TokenInfo carrying our resolved identity and the client IP in
// Extra. The verifier uses it on success; tests use it to construct authenticated requests.
func NewTokenInfo(userID string, exp time.Time, id store.Identity, clientIP string) *mcpauth.TokenInfo {
	return &mcpauth.TokenInfo{
		UserID:     userID, // enables the SDK's per-session hijack check
		Expiration: exp,
		Extra:      map[string]any{identityKey: id, clientIPKey: clientIP},
	}
}

// IdentityFromTokenInfo extracts the resolved identity from a TokenInfo.
func IdentityFromTokenInfo(ti *mcpauth.TokenInfo) (store.Identity, bool) {
	if ti == nil || ti.Extra == nil {
		return store.Identity{}, false
	}
	id, ok := ti.Extra[identityKey].(store.Identity)
	return id, ok
}

// IdentityFromRequest extracts the identity attached to an MCP server request by the auth
// middleware (carried in req.Extra.TokenInfo, since the handler ctx is the session-connect
// ctx, not the per-request one).
func IdentityFromRequest(req *mcp.CallToolRequest) (store.Identity, bool) {
	extra := req.GetExtra()
	if extra == nil {
		return store.Identity{}, false
	}
	return IdentityFromTokenInfo(extra.TokenInfo)
}
