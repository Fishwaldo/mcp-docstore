// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package web

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"regexp"
	"testing"
	"time"

	josejose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/mcp-oauth/storage"

	"github.com/Fishwaldo/mcp-docstore/internal/auth"
	"github.com/Fishwaldo/mcp-docstore/internal/config"
	"github.com/Fishwaldo/mcp-docstore/internal/store"
	"github.com/Fishwaldo/mcp-docstore/internal/tenant"
)

// testIssuer/testAudience mirror the values internal/auth/local_test.go uses: the LocalVerifier
// built in newTestServer checks tokens against exactly these.
const (
	testIssuer   = "https://docstore.example.com"
	testAudience = "https://docstore.example.com/mcp"
)

var sanitizeRe = regexp.MustCompile(`[/# ]`)

// sanitizeName makes t.Name() safe for use in a SQLite DSN (no "/ # space").
func sanitizeName(name string) string {
	return sanitizeRe.ReplaceAllString(name, "_")
}

// fakeRevocationChecker satisfies auth.RevocationChecker with nothing ever revoked — these
// tests exercise RequireBearer's routing of verify/resolve outcomes, not revocation.
type fakeRevocationChecker struct{}

func (fakeRevocationChecker) IsJTIRevoked(context.Context, string) (bool, error) {
	return false, nil
}

func (fakeRevocationChecker) GetRefreshTokenFamilyByID(context.Context, string) (*storage.RefreshTokenFamilyMetadata, error) {
	return nil, storage.ErrRefreshTokenFamilyNotFound
}

// testTokenClaims mirrors the claim shape a real access token carries.
type testTokenClaims struct {
	Issuer        string   `json:"iss"`
	Subject       string   `json:"sub"`
	Audience      []string `json:"aud"`
	Expiry        int64    `json:"exp"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Groups        []string `json:"groups"`
}

// defaultTestClaims builds a valid, non-expired claim set for subject/email/groups against
// testIssuer/testAudience.
func defaultTestClaims(subject, email string, groups []string) testTokenClaims {
	return testTokenClaims{
		Issuer:        testIssuer,
		Subject:       subject,
		Audience:      []string{testAudience},
		Expiry:        time.Now().Add(time.Hour).Unix(),
		Email:         email,
		EmailVerified: true,
		Groups:        groups,
	}
}

// mintToken signs claims as an ES256 JWT with key, the same way internal/auth/local_test.go
// mints tokens for LocalVerifier.
func mintToken(t *testing.T, key crypto.Signer, claims testTokenClaims) string {
	t.Helper()
	signer, err := josejose.NewSigner(josejose.SigningKey{Algorithm: josejose.ES256, Key: key}, nil)
	require.NoError(t, err)
	tok, err := jwt.Signed(signer).Claims(claims).Serialize()
	require.NoError(t, err)
	return tok
}

// newTestServer builds a Server wired with a real in-memory store, a tenant resolver mapping
// "acme.com" to the seeded "acme" tenant, and a real auth.LocalVerifier over a freshly
// generated ES256 key. The key is returned so tests can mint bearer tokens with mintToken that
// RequireBearer will accept.
func newTestServer(t *testing.T) (*Server, *store.Store, *ecdsa.PrivateKey) {
	t.Helper()
	dsn := "file:web-" + sanitizeName(t.Name()) + "?mode=memory&cache=shared&_pragma=foreign_keys(1)"
	st, err := store.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	require.NoError(t, st.Migrate(context.Background()))
	_, err = st.EnsureTenant(context.Background(), "acme", "Acme")
	require.NoError(t, err)

	resolver, err := tenant.NewResolver([]config.TenantSpec{
		{Key: "acme", Match: config.TenantMatch{Domains: []string{"acme.com"}}, Admins: []string{"alice@acme.com"}},
	})
	require.NoError(t, err)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	verifier := auth.NewLocalVerifier(testIssuer, []string{testAudience}, []crypto.PublicKey{key.Public()}, fakeRevocationChecker{})

	srv := New(Config{}, st, nil, resolver, verifier, nil)
	return srv, st, key
}
