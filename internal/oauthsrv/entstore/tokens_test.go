// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package entstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/giantswarm/mcp-oauth/storage"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrefreshtoken"
)

func testProviderToken() *oauth2.Token {
	tok := &oauth2.Token{
		AccessToken:  "access-token-value",
		TokenType:    "Bearer",
		RefreshToken: "upstream-refresh-value",
		Expiry:       time.Now().Add(time.Hour).Truncate(time.Second),
	}
	return tok.WithExtra(map[string]any{"id_token": "id-token-jwt-value"})
}

func TestSaveAndGetTokenRoundTrip(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	want := testProviderToken()
	require.NoError(t, s.SaveToken(ctx, "user-1", want))

	got, err := s.GetToken(ctx, "user-1")
	require.NoError(t, err)
	require.Equal(t, want.AccessToken, got.AccessToken)
	require.Equal(t, want.RefreshToken, got.RefreshToken)
	require.Equal(t, want.TokenType, got.TokenType)
	require.True(t, want.Expiry.Equal(got.Expiry))
	require.Equal(t, "id-token-jwt-value", got.Extra("id_token"))
}

func TestSaveTokenRefreshesTTLOnResave(t *testing.T) {
	s, client := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveToken(ctx, "user-ttl", testProviderToken()))
	row1, err := client.OAuthProviderToken.Query().Only(ctx)
	require.NoError(t, err)
	firstExpiry := row1.ExpiresAt

	time.Sleep(10 * time.Millisecond)

	require.NoError(t, s.SaveToken(ctx, "user-ttl", testProviderToken()))
	row2, err := client.OAuthProviderToken.Query().Only(ctx)
	require.NoError(t, err)

	require.True(t, row2.ExpiresAt.After(firstExpiry), "expiry must be refreshed on re-save")

	// Still exactly one row: SaveToken upserts by user_id rather than inserting a duplicate.
	count, err := client.OAuthProviderToken.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestGetTokenExpiredIsNotFound(t *testing.T) {
	s, client := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveToken(ctx, "user-expired", testProviderToken()))

	// Force the row's expiry into the past.
	n, err := client.OAuthProviderToken.Update().SetExpiresAt(time.Now().Add(-time.Minute)).Save(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	_, err = s.GetToken(ctx, "user-expired")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)
}

func TestGetTokenNotFound(t *testing.T) {
	s, _ := newTestEntStore(t)

	_, err := s.GetToken(context.Background(), "no-such-user")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)
}

func TestDeleteToken(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveToken(ctx, "user-del", testProviderToken()))
	require.NoError(t, s.DeleteToken(ctx, "user-del"))

	_, err := s.GetToken(ctx, "user-del")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)

	// Deleting an already-gone token is a harmless no-op.
	require.NoError(t, s.DeleteToken(ctx, "user-del"))
}

func testUserInfo() *storage.UserInfo {
	return &storage.UserInfo{
		ID:            "user-1",
		Email:         "user@example.com",
		EmailVerified: true,
		Name:          "Example User",
		GivenName:     "Example",
		FamilyName:    "User",
		Picture:       "https://example.com/avatar.png",
		Locale:        "en-US",
		Groups:        []string{"engineering", "on-call"},
	}
}

func TestSaveAndGetUserInfoRoundTrip(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	want := testUserInfo()
	require.NoError(t, s.SaveUserInfo(ctx, "user-1", want))

	got, err := s.GetUserInfo(ctx, "user-1")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestGetUserInfoNotFound(t *testing.T) {
	s, _ := newTestEntStore(t)

	_, err := s.GetUserInfo(context.Background(), "no-such-user")
	require.ErrorIs(t, err, storage.ErrUserInfoNotFound)
}

func TestSaveUserInfoUpsert(t *testing.T) {
	s, client := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveUserInfo(ctx, "user-1", testUserInfo()))
	updated := testUserInfo()
	updated.Name = "Renamed User"
	require.NoError(t, s.SaveUserInfo(ctx, "user-1", updated))

	got, err := s.GetUserInfo(ctx, "user-1")
	require.NoError(t, err)
	require.Equal(t, "Renamed User", got.Name)

	count, err := client.OAuthUserInfo.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestSaveGetDeleteRefreshToken(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, s.SaveRefreshToken(ctx, "raw-refresh-token", "user-1", expiresAt))

	userID, err := s.GetRefreshTokenInfo(ctx, "raw-refresh-token")
	require.NoError(t, err)
	require.Equal(t, "user-1", userID)

	require.NoError(t, s.DeleteRefreshToken(ctx, "raw-refresh-token"))

	_, err = s.GetRefreshTokenInfo(ctx, "raw-refresh-token")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)

	// Deleting an already-gone refresh token is a harmless no-op.
	require.NoError(t, s.DeleteRefreshToken(ctx, "raw-refresh-token"))
}

func TestGetRefreshTokenInfoNotFound(t *testing.T) {
	s, _ := newTestEntStore(t)

	_, err := s.GetRefreshTokenInfo(context.Background(), "no-such-token")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)
}

func TestGetRefreshTokenInfoExpired(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveRefreshToken(ctx, "expiring-token", "user-1", time.Now().Add(-time.Minute)))

	_, err := s.GetRefreshTokenInfo(ctx, "expiring-token")
	require.ErrorIs(t, err, storage.ErrTokenExpired)
}

func TestRefreshTokenNeverStoresRawValue(t *testing.T) {
	s, client := newTestEntStore(t)
	ctx := context.Background()

	const raw = "super-secret-raw-refresh-token"
	require.NoError(t, s.SaveRefreshToken(ctx, raw, "user-1", time.Now().Add(time.Hour)))

	row, err := client.OAuthRefreshToken.Query().Only(ctx)
	require.NoError(t, err)
	require.NotEqual(t, raw, row.TokenHash)
	require.Equal(t, hashToken(raw), row.TokenHash)
}

func TestAtomicGetAndDeleteRefreshToken(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	providerToken := testProviderToken()
	require.NoError(t, s.SaveToken(ctx, "user-1", providerToken))
	require.NoError(t, s.SaveRefreshToken(ctx, "atomic-refresh-1", "user-1", time.Now().Add(time.Hour)))

	// (a) success returns userID, clientID, and the cached provider token.
	userID, clientID, tok, err := s.AtomicGetAndDeleteRefreshToken(ctx, "atomic-refresh-1")
	require.NoError(t, err)
	require.Equal(t, "user-1", userID)
	require.Equal(t, "", clientID) // SaveRefreshToken records no client binding.
	require.NotNil(t, tok)
	require.Equal(t, providerToken.AccessToken, tok.AccessToken)
	require.Equal(t, "id-token-jwt-value", tok.Extra("id_token"))

	// (b) second call on the same (now-deleted) token is not found.
	_, _, _, err = s.AtomicGetAndDeleteRefreshToken(ctx, "atomic-refresh-1")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)
}

func TestAtomicGetAndDeleteRefreshTokenExpired(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveRefreshToken(ctx, "atomic-expired", "user-1", time.Now().Add(-time.Minute)))

	_, _, _, err := s.AtomicGetAndDeleteRefreshToken(ctx, "atomic-expired")
	require.ErrorIs(t, err, storage.ErrTokenExpired)
}

func TestAtomicGetAndDeleteRefreshTokenNotFound(t *testing.T) {
	s, _ := newTestEntStore(t)

	_, _, _, err := s.AtomicGetAndDeleteRefreshToken(context.Background(), "no-such-token")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)
}

func TestAtomicGetAndDeleteRefreshTokenAbsentProviderToken(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	// A valid, unexpired refresh token but NO cached provider token for the user. The absent
	// provider token must surface as ErrTokenNotFound rather than a (…, nil, nil) success: the
	// mcp-oauth refresh handler dereferences the returned provider token unconditionally, so a
	// nil here would be a nil-pointer DoS.
	require.NoError(t, s.SaveRefreshToken(ctx, "refresh-no-provider", "user-1", time.Now().Add(time.Hour)))

	_, _, tok, err := s.AtomicGetAndDeleteRefreshToken(ctx, "refresh-no-provider")
	require.ErrorIs(t, err, storage.ErrTokenNotFound)
	require.Nil(t, tok)
}

func TestAtomicGetAndDeleteRefreshTokenConcurrentSingleWinner(t *testing.T) {
	s, client := newTestEntStore(t)
	ctx := context.Background()

	// The winner also loads the cached provider token, so give the user one; without it every
	// caller would legitimately get ErrTokenNotFound and there would be no winner to single out.
	require.NoError(t, s.SaveToken(ctx, "user-1", testProviderToken()))
	require.NoError(t, s.SaveRefreshToken(ctx, "race-refresh", "user-1", time.Now().Add(time.Hour)))

	const goroutines = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var winners int
	var notFoundErrs int

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, err := s.AtomicGetAndDeleteRefreshToken(ctx, "race-refresh")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				winners++
			} else if errors.Is(err, storage.ErrTokenNotFound) {
				notFoundErrs++
			}
		}()
	}
	wg.Wait()

	require.Equal(t, 1, winners)
	require.Equal(t, goroutines-1, notFoundErrs)

	// The row is gone after the single winner deletes it.
	count, err := client.OAuthRefreshToken.Query().Where(oauthrefreshtoken.TokenHash(hashToken("race-refresh"))).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}
