package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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
// so token expiry and the groups claim are always current (spec §3, §6). Any identity-
// resolution failure is wrapped as mcpauth.ErrInvalidToken so RequireBearerToken returns
// 401 with the resource-metadata challenge (we intentionally collapse the finer 401/403
// distinction into 401 to use the SDK middleware; the challenge still guides the client).
func NewResourceVerifier(v Verifier, resolver *tenant.Resolver, st *store.Store) mcpauth.TokenVerifier {
	return func(ctx context.Context, rawToken string, _ *http.Request) (*mcpauth.TokenInfo, error) {
		claims, err := v.Verify(ctx, rawToken)
		if err != nil || claims == nil {
			return nil, fmt.Errorf("%w: %v", mcpauth.ErrInvalidToken, err)
		}
		key, ok := resolver.Resolve(claims.Email)
		if !ok {
			return nil, fmt.Errorf("%w: email not onboarded", mcpauth.ErrInvalidToken)
		}
		ten, err := st.TenantByKey(ctx, key)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("%w: tenant not provisioned", mcpauth.ErrInvalidToken)
			}
			return nil, err // DB fault -> 500 via the SDK middleware
		}
		usr, err := st.UpsertUser(ctx, ten.ID, claims.Subject, claims.Email, resolver.IsAdmin(key, claims.Email))
		if err != nil {
			if errors.Is(err, store.ErrInvalid) {
				return nil, fmt.Errorf("%w: identity rejected", mcpauth.ErrInvalidToken)
			}
			return nil, err
		}
		id := store.Identity{
			TenantID: ten.ID,
			UserID:   usr.ID,
			Groups:   claims.Groups,
			IsAdmin:  usr.Role == user.RoleAdmin,
		}
		return &mcpauth.TokenInfo{
			UserID:     usr.ID.String(), // enables the SDK's per-session hijack check
			Expiration: claims.Expiry,
			Extra:      map[string]any{identityKey: id},
		}, nil
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
