// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/Fishwaldo/mcp-docstore/internal/ent/user"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

// IdentityError reports why verified claims could not be resolved to a store.Identity.
// Reason is a stable token for logging. Err is non-nil only for infrastructure faults
// (e.g. a DB error) that a transport should surface as 5xx; for onboarding/identity
// rejections Err is nil and the transport should treat it as an auth failure.
type IdentityError struct {
	Reason string
	Err    error
}

func (e *IdentityError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("resolve identity: %s: %v", e.Reason, e.Err)
	}
	return fmt.Sprintf("resolve identity: %s", e.Reason)
}

func (e *IdentityError) Unwrap() error { return e.Err }

// ResolveIdentity turns verified OIDC claims into a store.Identity via the tenant
// resolver and UpsertUser — the single path shared by every bearer-token transport (the
// MCP verifier and the web API's RequireBearer). Failures are returned as *IdentityError
// so each transport maps them without duplicating the resolve/UpsertUser sequence.
func ResolveIdentity(ctx context.Context, resolver *tenant.Resolver, st *store.Store, claims *Claims) (store.Identity, error) {
	key, ok := resolver.Resolve(claims.Email)
	if !ok {
		return store.Identity{}, &IdentityError{Reason: "email_not_onboarded"}
	}
	ten, err := st.TenantByKey(ctx, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Identity{}, &IdentityError{Reason: "tenant_not_provisioned"}
		}
		return store.Identity{}, &IdentityError{Reason: "db_error", Err: err}
	}
	usr, err := st.UpsertUser(ctx, ten.ID, claims.Subject, claims.Email, resolver.IsAdmin(key, claims.Email))
	if err != nil {
		if errors.Is(err, store.ErrInvalid) {
			return store.Identity{}, &IdentityError{Reason: "identity_rejected"}
		}
		return store.Identity{}, &IdentityError{Reason: "db_error", Err: err}
	}
	return store.Identity{
		TenantID: ten.ID,
		UserID:   usr.ID,
		Groups:   claims.Groups,
		IsAdmin:  usr.Role == user.RoleAdmin,
	}, nil
}
