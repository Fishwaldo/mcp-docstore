// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package entstore

import (
	"context"
	"testing"
	"time"

	"github.com/giantswarm/mcp-oauth/storage"
	"github.com/stretchr/testify/require"

	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrefreshtoken"
)

func TestSaveAndGetRefreshTokenFamilyRoundTrip(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	expiresAt := time.Now().Add(time.Hour).Truncate(time.Second)
	require.NoError(t, s.SaveRefreshTokenWithFamily(ctx, "rt-gen0", "user-1", "client-1", "family-1", 0, expiresAt))

	got, err := s.GetRefreshTokenFamily(ctx, "rt-gen0")
	require.NoError(t, err)
	require.Equal(t, "family-1", got.FamilyID)
	require.Equal(t, "user-1", got.UserID)
	require.Equal(t, "client-1", got.ClientID)
	require.Equal(t, 0, got.Generation)
	require.False(t, got.Revoked)
	require.True(t, got.RevokedAt.IsZero())
}

func TestGetRefreshTokenFamilyByID(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, s.SaveRefreshTokenWithFamily(ctx, "rt-gen0", "user-1", "client-1", "family-1", 0, expiresAt))

	got, err := s.GetRefreshTokenFamilyByID(ctx, "family-1")
	require.NoError(t, err)
	require.Equal(t, "family-1", got.FamilyID)
	require.Equal(t, 0, got.Generation)
}

func TestGetRefreshTokenFamilyByIDNotFound(t *testing.T) {
	s, _ := newTestEntStore(t)

	_, err := s.GetRefreshTokenFamilyByID(context.Background(), "no-such-family")
	require.ErrorIs(t, err, storage.ErrRefreshTokenFamilyNotFound)
}

func TestGetRefreshTokenFamilyNotFound(t *testing.T) {
	s, _ := newTestEntStore(t)

	_, err := s.GetRefreshTokenFamily(context.Background(), "no-such-token")
	require.ErrorIs(t, err, storage.ErrRefreshTokenFamilyNotFound)
}

// TestSaveRefreshTokenWithFamilyAdvancesGeneration verifies that rotating a refresh token
// within the same family advances the family's generation, and that both the old and new
// tokens' family lookups reflect the new (shared) generation — the family row, not a
// per-token snapshot, is the source of truth for "what generation is this family on".
func TestSaveRefreshTokenWithFamilyAdvancesGeneration(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, s.SaveRefreshTokenWithFamily(ctx, "rt-gen0", "user-1", "client-1", "family-1", 0, expiresAt))
	require.NoError(t, s.SaveRefreshTokenWithFamily(ctx, "rt-gen1", "user-1", "client-1", "family-1", 1, expiresAt))

	byID, err := s.GetRefreshTokenFamilyByID(ctx, "family-1")
	require.NoError(t, err)
	require.Equal(t, 1, byID.Generation)
}

// TestGetRefreshTokenFamilySurvivesConsumption is the key OAuth 2.1 reuse-detection property:
// once a refresh token has been atomically consumed (rotated away), GetRefreshTokenFamily must
// still resolve its family — reuse detection (and the legitimate-rotation generation
// computation that happens in the same instant) both look up the family for a token whose
// OAuthRefreshToken row is already gone.
func TestGetRefreshTokenFamilySurvivesConsumption(t *testing.T) {
	s, client := newTestEntStore(t)
	ctx := context.Background()

	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, s.SaveRefreshTokenWithFamily(ctx, "rt-consumed", "user-1", "client-1", "family-1", 0, expiresAt))

	// Consume it exactly like a legitimate refresh-token-grant exchange would.
	require.NoError(t, s.DeleteRefreshToken(ctx, "rt-consumed"))
	count, err := client.OAuthRefreshToken.Query().Where(oauthrefreshtoken.TokenHash(hashToken("rt-consumed"))).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, count)

	got, err := s.GetRefreshTokenFamily(ctx, "rt-consumed")
	require.NoError(t, err)
	require.Equal(t, "family-1", got.FamilyID)
}

func TestRevokeRefreshTokenFamily(t *testing.T) {
	s, client := newTestEntStore(t)
	ctx := context.Background()

	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, s.SaveRefreshTokenWithFamily(ctx, "rt-gen0", "user-1", "client-1", "family-1", 0, expiresAt))
	require.NoError(t, s.SaveRefreshTokenWithFamily(ctx, "rt-gen1", "user-1", "client-1", "family-1", 1, expiresAt))

	require.NoError(t, s.RevokeRefreshTokenFamily(ctx, "family-1"))

	byID, err := s.GetRefreshTokenFamilyByID(ctx, "family-1")
	require.NoError(t, err)
	require.True(t, byID.Revoked)
	require.False(t, byID.RevokedAt.IsZero())

	// Every member refresh-token row is gone: the atomic gate on either now reports
	// ErrTokenNotFound, exactly as if the tokens had never existed.
	_, _, _, err = s.AtomicGetAndDeleteRefreshToken(ctx, "rt-gen0")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)
	_, _, _, err = s.AtomicGetAndDeleteRefreshToken(ctx, "rt-gen1")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)

	count, err := client.OAuthRefreshToken.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestRevokeRefreshTokenFamilyUnknownFamilyIsNoOp(t *testing.T) {
	s, _ := newTestEntStore(t)

	require.NoError(t, s.RevokeRefreshTokenFamily(context.Background(), "no-such-family"))
}
