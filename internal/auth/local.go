// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/giantswarm/mcp-oauth/storage"
)

// RevocationChecker reports whether an issued token has been revoked. It is satisfied by
// the OAuth storage backend; both checks run on the /mcp hot path, so implementations must
// be cheap (indexed lookups).
type RevocationChecker interface {
	IsJTIRevoked(ctx context.Context, jti string) (bool, error)
	GetRefreshTokenFamilyByID(ctx context.Context, familyID string) (*storage.RefreshTokenFamilyMetadata, error)
}

// LocalVerifier validates DocStore-issued access tokens (RFC 9068 JWTs) against a static,
// in-process public key set. Unlike OIDCVerifier it does no JWKS HTTP fetch and has no
// dependency on the server's own public URL being reachable: the keys are the AS's own
// signing key material, loaded once at boot.
type LocalVerifier struct {
	verifier *oidc.IDTokenVerifier
	audience string
	rc       RevocationChecker
}

// NewLocalVerifier verifies self-issued access tokens against a static public key set — no
// JWKS fetch, no network dependency on our own public URL. It enforces issuer, audience,
// expiry, and (when the token carries jti/family_id claims) the revocation and family state
// via rc.
func NewLocalVerifier(issuer, audience string, keys []crypto.PublicKey, rc RevocationChecker) *LocalVerifier {
	cfg := &oidc.Config{
		// The audience check is done ourselves after Verify, so we can normalize away a
		// trailing slash on either side (go-oidc's built-in check is an exact-membership
		// match). SkipClientIDCheck is required whenever ClientID is left empty.
		SkipClientIDCheck: true,
		// Asymmetric, single algorithm: the AS only ever signs with the ES256 key in
		// KeyMaterial.Signer, so nothing else should ever verify.
		SupportedSigningAlgs: []string{oidc.ES256},
	}
	keySet := &oidc.StaticKeySet{PublicKeys: keys}
	return &LocalVerifier{
		verifier: oidc.NewVerifier(issuer, keySet, cfg),
		audience: normalizeAudience(audience),
		rc:       rc,
	}
}

// localClaims is the subset of RFC 9068 access-token claims this verifier needs, beyond what
// oidc.IDToken already decodes (issuer, subject, expiry, audience).
type localClaims struct {
	Email         string   `json:"email"`
	EmailVerified *bool    `json:"email_verified"`
	Groups        []string `json:"groups"`
	JTI           string   `json:"jti"`
	FamilyID      string   `json:"family_id"`
}

// Verify validates rawToken's signature, issuer, expiry, and audience, then enforces
// revocation: a token whose jti is on the denylist, or whose family_id names a revoked
// refresh-token family, is rejected. Claims are mapped into *Claims exactly as OIDCVerifier
// does, so downstream consumers (resource.go, ResolveIdentity) don't need to know which
// verifier produced them.
func (v *LocalVerifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	idToken, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	if idToken.Subject == "" {
		return nil, fmt.Errorf("verify token: missing subject")
	}
	if !audienceMatches(idToken.Audience, v.audience) {
		return nil, fmt.Errorf("verify token: audience mismatch")
	}

	var lc localClaims
	if err := idToken.Claims(&lc); err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}

	if lc.JTI != "" {
		revoked, err := v.rc.IsJTIRevoked(ctx, lc.JTI)
		if err != nil {
			return nil, fmt.Errorf("check jti revocation: %w", err)
		}
		if revoked {
			return nil, fmt.Errorf("verify token: revoked")
		}
	}
	if lc.FamilyID != "" {
		family, err := v.rc.GetRefreshTokenFamilyByID(ctx, lc.FamilyID)
		switch {
		case err == nil:
			if family.Revoked {
				return nil, fmt.Errorf("verify token: refresh token family revoked")
			}
		case errors.Is(err, storage.ErrRefreshTokenFamilyNotFound):
			// Family aged out of retention, or never tracked (e.g. issued before
			// family tracking existed): not a revocation signal, so allow.
		default:
			// Fail closed: a storage error means we cannot confirm the family isn't
			// revoked, and the hot path must not silently treat "unknown" as "safe".
			return nil, fmt.Errorf("check family revocation: %w", err)
		}
	}

	return &Claims{
		Subject:       idToken.Subject,
		Email:         lc.Email,
		Groups:        lc.Groups,
		Expiry:        idToken.Expiry,
		EmailVerified: lc.EmailVerified,
	}, nil
}

// normalizeAudience strips a single trailing slash so callers may configure the canonical
// audience with or without one.
func normalizeAudience(aud string) string {
	return strings.TrimSuffix(aud, "/")
}

// audienceMatches reports whether any entry of aud, trailing-slash-normalized, equals the
// already-normalized want.
func audienceMatches(aud []string, want string) bool {
	for _, a := range aud {
		if normalizeAudience(a) == want {
			return true
		}
	}
	return false
}
