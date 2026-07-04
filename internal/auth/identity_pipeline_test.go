// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	josejose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
)

func TestVerifyRequestIdentityHappyPath(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	rc := newFakeRevocationChecker()
	v := NewLocalVerifier(testIssuer, []string{testAudience}, []crypto.PublicKey{key.Public()}, rc)

	claims := defaultClaims()
	claims.Issuer = testIssuer
	claims.Email = "alice@acme.com"
	tok := mintToken(t, key, josejose.ES256, claims)

	id, err := VerifyRequestIdentity(context.Background(), v, acmeResolver(t), newAuthStore(t), tok)
	require.NoError(t, err)
	require.NotNil(t, id)
	require.NotEqual(t, uuid.Nil, id.TenantID)
	require.True(t, id.IsAdmin)
}

func TestVerifyRequestIdentityUnknownDomainIsErrIdentityRejected(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	rc := newFakeRevocationChecker()
	v := NewLocalVerifier(testIssuer, []string{testAudience}, []crypto.PublicKey{key.Public()}, rc)

	claims := defaultClaims()
	claims.Issuer = testIssuer
	claims.Email = "nobody@nope.com"
	tok := mintToken(t, key, josejose.ES256, claims)

	id, err := VerifyRequestIdentity(context.Background(), v, acmeResolver(t), newAuthStore(t), tok)
	require.Nil(t, id)
	require.True(t, errors.Is(err, ErrIdentityRejected), "want ErrIdentityRejected, got %v", err)

	var ie *IdentityError
	require.ErrorAs(t, err, &ie)
	require.Equal(t, "email_not_onboarded", ie.Reason)
}

func TestVerifyRequestIdentityInvalidTokenIsNotErrIdentityRejected(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	rc := newFakeRevocationChecker()
	v := NewLocalVerifier(testIssuer, []string{testAudience}, []crypto.PublicKey{key.Public()}, rc)

	id, err := VerifyRequestIdentity(context.Background(), v, acmeResolver(t), newAuthStore(t), "garbage-not-a-jwt")
	require.Nil(t, id)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrIdentityRejected), "garbage token must not be classified as ErrIdentityRejected")
}

func TestVerifyRequestIdentityExpiredTokenIsNotErrIdentityRejected(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	rc := newFakeRevocationChecker()
	v := NewLocalVerifier(testIssuer, []string{testAudience}, []crypto.PublicKey{key.Public()}, rc)

	claims := defaultClaims()
	claims.Issuer = testIssuer
	claims.Email = "alice@acme.com"
	claims.Expiry = time.Now().Add(-time.Hour).Unix()
	tok := mintToken(t, key, josejose.ES256, claims)

	id, err := VerifyRequestIdentity(context.Background(), v, acmeResolver(t), newAuthStore(t), tok)
	require.Nil(t, id)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrIdentityRejected))
}
