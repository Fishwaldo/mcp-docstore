// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

// ErrIdentityRejected is the sentinel a caller checks with errors.Is to tell "the token
// itself was valid, but it could not be resolved to a store.Identity" apart from "the token
// was invalid" (bad signature, expired, wrong audience, revoked, ...). VerifyRequestIdentity
// wraps it around the underlying *IdentityError, so a caller that also needs the finer
// Reason/Err distinction (an onboarding rejection vs. an infrastructure fault) can still
// errors.As for it.
//
// The distinction matters because the two failure modes warrant different HTTP treatment:
// an invalid token is a 401 (the client should re-authenticate), while a valid-but-unresolved
// identity is a 403 for transports that can express it (the client is who it says it is, but
// isn't provisioned) — the /mcp path collapses both to 401 to fit the SDK's bearer-token
// middleware, but other transports need not.
var ErrIdentityRejected = errors.New("auth: identity rejected")

// verifyRequestIdentity is the actual verify->ResolveIdentity->identity pipeline. It also
// returns the verified Claims, which VerifyRequestIdentity (the exported form, for callers
// outside this package that only ever need the resolved identity) discards. Package-internal
// callers that need claim data beyond the identity — NewResourceVerifier needs claims.Expiry
// to populate mcpauth.TokenInfo.Expiration — call this directly instead, so the token is
// verified exactly once per request rather than once for the identity and again for the
// claims.
func verifyRequestIdentity(ctx context.Context, v Verifier, resolver *tenant.Resolver, st *store.Store, rawToken string) (*Claims, *store.Identity, error) {
	claims, err := v.Verify(ctx, rawToken)
	if err != nil {
		return nil, nil, err
	}
	if claims == nil {
		return nil, nil, fmt.Errorf("verify token: verifier returned no claims")
	}
	id, err := ResolveIdentity(ctx, resolver, st, claims)
	if err != nil {
		return claims, nil, fmt.Errorf("%w: %w", ErrIdentityRejected, err)
	}
	return claims, &id, nil
}

// VerifyRequestIdentity runs the pipeline shared by every transport that authenticates a
// bearer token: verify rawToken with v, then resolve the resulting claims to a
// store.Identity via resolver and st. It is the single place this sequence is implemented;
// NewResourceVerifier (the /mcp path) and any transport built on it call this instead of
// duplicating verify+ResolveIdentity plumbing.
//
// A token-invalid error is returned unchanged, so callers can distinguish it with a plain
// errors.Is/As on the verifier's own error type. A token that verifies but whose identity
// cannot be resolved (unknown tenant, not onboarded, or a DB error encountered while
// resolving) instead returns an error wrapping both ErrIdentityRejected and the underlying
// *IdentityError, so callers can use errors.Is(err, ErrIdentityRejected) to route it
// differently from a token-invalid error without needing to know about *IdentityError at all.
func VerifyRequestIdentity(ctx context.Context, v Verifier, resolver *tenant.Resolver, st *store.Store, rawToken string) (*store.Identity, error) {
	_, id, err := verifyRequestIdentity(ctx, v, resolver, st, rawToken)
	return id, err
}
