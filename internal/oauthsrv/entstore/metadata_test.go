// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package entstore

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/giantswarm/mcp-oauth/storage"
	"github.com/stretchr/testify/require"
)

func testTokenMetadata() storage.TokenMetadata {
	return storage.TokenMetadata{
		UserID:      "user-1",
		ClientID:    "client-1",
		IssuedAt:    time.Now().Add(-time.Minute).Truncate(time.Second),
		ExpiresAt:   time.Now().Add(time.Hour).Truncate(time.Second),
		TokenType:   "access",
		Audience:    "https://resource.example.com",
		Scopes:      []string{"docs:read", "docs:write"},
		FamilyID:    "family-1",
		JKT:         "jkt-thumbprint",
		ExtraClaims: map[string]any{"act": "agentA"},
	}
}

func TestSaveAndGetTokenMetadataRoundTrip(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	want := testTokenMetadata()
	require.NoError(t, s.SaveTokenMetadata(ctx, "access-token-value", want))

	got, err := s.GetTokenMetadata("access-token-value")
	require.NoError(t, err)
	require.Equal(t, want.UserID, got.UserID)
	require.Equal(t, want.ClientID, got.ClientID)
	require.True(t, want.IssuedAt.Equal(got.IssuedAt))
	require.True(t, want.ExpiresAt.Equal(got.ExpiresAt))
	require.Equal(t, want.TokenType, got.TokenType)
	require.Equal(t, want.Audience, got.Audience)
	require.Equal(t, want.Scopes, got.Scopes)
	require.Equal(t, want.FamilyID, got.FamilyID)
	require.Equal(t, want.JKT, got.JKT)
	require.Equal(t, want.ExtraClaims, got.ExtraClaims)
}

func TestGetTokenMetadataNotFound(t *testing.T) {
	s, _ := newTestEntStore(t)

	_, err := s.GetTokenMetadata("no-such-token")
	require.Error(t, err)
}

func TestSaveTokenMetadataUpsert(t *testing.T) {
	s, client := newTestEntStore(t)
	ctx := context.Background()

	md := testTokenMetadata()
	require.NoError(t, s.SaveTokenMetadata(ctx, "access-token-value", md))

	md.Scopes = []string{"docs:read"}
	require.NoError(t, s.SaveTokenMetadata(ctx, "access-token-value", md))

	got, err := s.GetTokenMetadata("access-token-value")
	require.NoError(t, err)
	require.Equal(t, []string{"docs:read"}, got.Scopes)

	count, err := client.OAuthTokenMetadata.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestTokenMetadataNeverStoresRawTokenID(t *testing.T) {
	s, client := newTestEntStore(t)
	ctx := context.Background()

	const raw = "super-secret-bearer-value"
	require.NoError(t, s.SaveTokenMetadata(ctx, raw, testTokenMetadata()))

	row, err := client.OAuthTokenMetadata.Query().Only(ctx)
	require.NoError(t, err)
	require.NotEqual(t, raw, row.TokenID)
	require.Equal(t, hashToken(raw), row.TokenID)
}

func TestRevokeJTIAndIsJTIRevokedRoundTrip(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	revoked, err := s.IsJTIRevoked(ctx, "jti-1")
	require.NoError(t, err)
	require.False(t, revoked)

	require.NoError(t, s.RevokeJTI(ctx, "jti-1", time.Now().Add(time.Hour)))

	revoked, err = s.IsJTIRevoked(ctx, "jti-1")
	require.NoError(t, err)
	require.True(t, revoked)
}

func TestIsJTIRevokedExpiredEntryIsNotRevoked(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.RevokeJTI(ctx, "jti-expired", time.Now().Add(-time.Minute)))

	revoked, err := s.IsJTIRevoked(ctx, "jti-expired")
	require.NoError(t, err)
	require.False(t, revoked, "an expired denylist entry must report as not-revoked")
}

func TestRevokeJTIUpsertRefreshesExpiry(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.RevokeJTI(ctx, "jti-1", time.Now().Add(-time.Minute)))
	revoked, err := s.IsJTIRevoked(ctx, "jti-1")
	require.NoError(t, err)
	require.False(t, revoked)

	// Re-revoking the same jti with a future expiry must take effect (upsert, not
	// create-only).
	require.NoError(t, s.RevokeJTI(ctx, "jti-1", time.Now().Add(time.Hour)))
	revoked, err = s.IsJTIRevoked(ctx, "jti-1")
	require.NoError(t, err)
	require.True(t, revoked)
}

func TestRevokeAllTokensForUserClient(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	expiresAt := time.Now().Add(time.Hour)

	// Two refresh-token generations in the same family for (user-1, client-1).
	require.NoError(t, s.SaveRefreshTokenWithFamily(ctx, "rt-gen0", "user-1", "client-1", "family-1", 0, expiresAt))
	require.NoError(t, s.SaveRefreshTokenWithFamily(ctx, "rt-gen1", "user-1", "client-1", "family-1", 1, expiresAt))

	// One non-expired and one already-expired access-token metadata row for the same pair.
	live := testTokenMetadata()
	live.ExpiresAt = time.Now().Add(time.Hour)
	require.NoError(t, s.SaveTokenMetadata(ctx, "access-live", live))

	expired := testTokenMetadata()
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	require.NoError(t, s.SaveTokenMetadata(ctx, "access-expired", expired))

	// A refresh token belonging to a different client must survive untouched.
	require.NoError(t, s.SaveRefreshTokenWithFamily(ctx, "rt-other-client", "user-1", "client-2", "family-2", 0, expiresAt))

	n, err := s.RevokeAllTokensForUserClient(ctx, "user-1", "client-1")
	require.NoError(t, err)
	// 2 refresh-token rows deleted + 1 non-expired access token denylisted = 3.
	require.Equal(t, 3, n)

	// The pair has no usable refresh tokens left.
	_, _, _, err = s.AtomicGetAndDeleteRefreshToken(ctx, "rt-gen0")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)
	_, _, _, err = s.AtomicGetAndDeleteRefreshToken(ctx, "rt-gen1")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)

	// The family is marked revoked.
	fam, err := s.GetRefreshTokenFamilyByID(ctx, "family-1")
	require.NoError(t, err)
	require.True(t, fam.Revoked)

	// The non-expired access token's JTI (its stored, hashed token_id) is now revoked; the
	// already-expired one was skipped (revoking it has no security value).
	liveRevoked, err := s.IsJTIRevoked(ctx, hashToken("access-live"))
	require.NoError(t, err)
	require.True(t, liveRevoked)

	expiredRevoked, err := s.IsJTIRevoked(ctx, hashToken("access-expired"))
	require.NoError(t, err)
	require.False(t, expiredRevoked)

	// The other client's family is untouched.
	other, err := s.GetRefreshTokenFamilyByID(ctx, "family-2")
	require.NoError(t, err)
	require.False(t, other.Revoked)
	_, err = s.GetRefreshTokenInfo(ctx, "rt-other-client")
	require.NoError(t, err)
}

func TestRevokeAllTokensForUserClientNoTokens(t *testing.T) {
	s, _ := newTestEntStore(t)

	n, err := s.RevokeAllTokensForUserClient(context.Background(), "user-none", "client-none")
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

func TestGetTokensByUserClient(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveTokenMetadata(ctx, "access-1", testTokenMetadata()))
	require.NoError(t, s.SaveTokenMetadata(ctx, "access-2", testTokenMetadata()))

	other := testTokenMetadata()
	other.ClientID = "client-2"
	require.NoError(t, s.SaveTokenMetadata(ctx, "access-3", other))

	ids, err := s.GetTokensByUserClient(ctx, "user-1", "client-1")
	require.NoError(t, err)
	sort.Strings(ids)

	want := []string{hashToken("access-1"), hashToken("access-2")}
	sort.Strings(want)
	require.Equal(t, want, ids)
}
