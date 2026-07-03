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
)

func testAuthState(stateID, providerState string) *storage.AuthorizationState {
	return &storage.AuthorizationState{
		StateID:              stateID,
		OriginalClientState:  "client-state-xyz",
		ClientID:             "client-abc",
		RedirectURI:          "https://app.example/cb",
		Scope:                "openid profile",
		Resource:             "https://api.example/",
		CodeChallenge:        "challenge",
		CodeChallengeMethod:  "S256",
		ProviderState:        providerState,
		ProviderCodeVerifier: "super-secret-verifier",
		Nonce:                "nonce-123",
		ExpiresAt:            time.Now().Add(5 * time.Minute),
	}
}

func TestSaveAndGetAuthorizationStateByStateID(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	want := testAuthState("state-1", "provider-state-1")
	require.NoError(t, s.SaveAuthorizationState(ctx, want))

	got, err := s.GetAuthorizationState(ctx, "state-1")
	require.NoError(t, err)
	require.Equal(t, want.StateID, got.StateID)
	require.Equal(t, want.OriginalClientState, got.OriginalClientState)
	require.Equal(t, want.ClientID, got.ClientID)
	require.Equal(t, want.RedirectURI, got.RedirectURI)
	require.Equal(t, want.Scope, got.Scope)
	require.Equal(t, want.Resource, got.Resource)
	require.Equal(t, want.CodeChallenge, got.CodeChallenge)
	require.Equal(t, want.CodeChallengeMethod, got.CodeChallengeMethod)
	require.Equal(t, want.ProviderState, got.ProviderState)
	require.Equal(t, want.ProviderCodeVerifier, got.ProviderCodeVerifier)
	require.Equal(t, want.Nonce, got.Nonce)
	require.False(t, got.CreatedAt.IsZero())
}

func TestGetAuthorizationStateByProviderState(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	want := testAuthState("state-2", "provider-state-2")
	require.NoError(t, s.SaveAuthorizationState(ctx, want))

	got, err := s.GetAuthorizationStateByProviderState(ctx, "provider-state-2")
	require.NoError(t, err)
	require.Equal(t, want.StateID, got.StateID)
	require.Equal(t, want.ProviderCodeVerifier, got.ProviderCodeVerifier)
}

func TestGetAuthorizationStateNotFound(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	_, err := s.GetAuthorizationState(ctx, "no-such-state")
	require.ErrorIs(t, err, storage.ErrAuthorizationStateNotFound)

	_, err = s.GetAuthorizationStateByProviderState(ctx, "no-such-provider-state")
	require.ErrorIs(t, err, storage.ErrAuthorizationStateNotFound)
}

func TestDeleteAuthorizationState(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveAuthorizationState(ctx, testAuthState("state-3", "provider-state-3")))

	require.NoError(t, s.DeleteAuthorizationState(ctx, "state-3"))

	_, err := s.GetAuthorizationState(ctx, "state-3")
	require.ErrorIs(t, err, storage.ErrAuthorizationStateNotFound)
	_, err = s.GetAuthorizationStateByProviderState(ctx, "provider-state-3")
	require.ErrorIs(t, err, storage.ErrAuthorizationStateNotFound)

	// Deleting an already-gone state is a harmless no-op.
	require.NoError(t, s.DeleteAuthorizationState(ctx, "state-3"))
}

func TestDeleteAuthorizationStateByProviderState(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveAuthorizationState(ctx, testAuthState("state-4", "provider-state-4")))

	// Deletion also works when passed the provider_state value (as happens
	// after a provider callback validates via GetAuthorizationStateByProviderState).
	require.NoError(t, s.DeleteAuthorizationState(ctx, "provider-state-4"))

	_, err := s.GetAuthorizationState(ctx, "state-4")
	require.ErrorIs(t, err, storage.ErrAuthorizationStateNotFound)
}

func testAuthCode(t *testing.T, code string, expiresAt time.Time) *storage.AuthorizationCode {
	t.Helper()
	token := &oauth2.Token{
		AccessToken:  "access-token-value",
		TokenType:    "Bearer",
		RefreshToken: "refresh-token-value",
		Expiry:       time.Now().Add(time.Hour).Truncate(time.Second),
	}
	token = token.WithExtra(map[string]any{"id_token": "id-token-jwt-value"})

	return &storage.AuthorizationCode{
		Code:                code,
		ClientID:            "client-abc",
		RedirectURI:         "https://app.example/cb",
		Scope:               "openid profile",
		Resource:            "https://api.example/",
		Audience:            "https://api.example/",
		CodeChallenge:       "challenge",
		CodeChallengeMethod: "S256",
		UserID:              "user-1",
		ProviderToken:       token,
		ExpiresAt:           expiresAt,
	}
}

func TestSaveAndGetAuthorizationCode(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	want := testAuthCode(t, "code-1", time.Now().Add(5*time.Minute))
	require.NoError(t, s.SaveAuthorizationCode(ctx, want))

	got, err := s.GetAuthorizationCode(ctx, "code-1")
	require.NoError(t, err)
	require.Equal(t, want.Code, got.Code)
	require.Equal(t, want.ClientID, got.ClientID)
	require.Equal(t, want.UserID, got.UserID)
	require.False(t, got.Used)
	require.NotNil(t, got.ProviderToken)
	require.Equal(t, want.ProviderToken.AccessToken, got.ProviderToken.AccessToken)
	require.Equal(t, want.ProviderToken.RefreshToken, got.ProviderToken.RefreshToken)
	require.Equal(t, want.ProviderToken.TokenType, got.ProviderToken.TokenType)
	require.True(t, want.ProviderToken.Expiry.Equal(got.ProviderToken.Expiry))
	require.Equal(t, "id-token-jwt-value", got.ProviderToken.Extra("id_token"))
}

func TestSaveAuthorizationCodeNilProviderToken(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	code := &storage.AuthorizationCode{
		Code:        "code-no-token",
		ClientID:    "client-abc",
		RedirectURI: "https://app.example/cb",
		UserID:      "user-1",
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}
	require.NoError(t, s.SaveAuthorizationCode(ctx, code))

	got, err := s.GetAuthorizationCode(ctx, "code-no-token")
	require.NoError(t, err)
	require.Nil(t, got.ProviderToken)
}

func TestGetAuthorizationCodeNotFound(t *testing.T) {
	s, _ := newTestEntStore(t)

	_, err := s.GetAuthorizationCode(context.Background(), "missing-code")
	require.ErrorIs(t, err, storage.ErrAuthorizationCodeNotFound)
}

func TestAtomicCheckAndMarkAuthCodeUsed(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	want := testAuthCode(t, "atomic-code-1", time.Now().Add(5*time.Minute))
	require.NoError(t, s.SaveAuthorizationCode(ctx, want))

	// (a) first call succeeds and returns the code with decrypted ProviderToken.
	got, err := s.AtomicCheckAndMarkAuthCodeUsed(ctx, "atomic-code-1")
	require.NoError(t, err)
	require.Equal(t, want.ProviderToken.AccessToken, got.ProviderToken.AccessToken)
	require.Equal(t, "id-token-jwt-value", got.ProviderToken.Extra("id_token"))
	require.True(t, got.Used)

	// (b) second call on the same code fails as reuse AND still returns the
	// decoded auth-code — the library's reuse handler dereferences its UserID
	// to revoke every token for that user+client, so a nil here would panic
	// the token endpoint on every replay.
	reused, err := s.AtomicCheckAndMarkAuthCodeUsed(ctx, "atomic-code-1")
	require.ErrorIs(t, err, storage.ErrAuthorizationCodeUsed)
	require.NotNil(t, reused)
	require.Equal(t, want.UserID, reused.UserID)
}

func TestAtomicCheckAndMarkAuthCodeUsedNotFound(t *testing.T) {
	s, _ := newTestEntStore(t)

	// (c) unknown code.
	_, err := s.AtomicCheckAndMarkAuthCodeUsed(context.Background(), "no-such-code")
	require.ErrorIs(t, err, storage.ErrAuthorizationCodeNotFound)
}

func TestAtomicCheckAndMarkAuthCodeUsedExpired(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	// (d) expired code.
	expired := testAuthCode(t, "expired-code", time.Now().Add(-time.Hour))
	require.NoError(t, s.SaveAuthorizationCode(ctx, expired))

	_, err := s.AtomicCheckAndMarkAuthCodeUsed(ctx, "expired-code")
	require.ErrorIs(t, err, storage.ErrTokenExpired)
}

func TestAtomicCheckAndMarkAuthCodeUsedConcurrentSingleWinner(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveAuthorizationCode(ctx, testAuthCode(t, "race-code", time.Now().Add(5*time.Minute))))

	const goroutines = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var winners int
	var reuseErrs int

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.AtomicCheckAndMarkAuthCodeUsed(ctx, "race-code")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				winners++
			} else if errors.Is(err, storage.ErrAuthorizationCodeUsed) {
				reuseErrs++
			}
		}()
	}
	wg.Wait()

	require.Equal(t, 1, winners)
	require.Equal(t, goroutines-1, reuseErrs)
}

func TestDeleteAuthorizationCode(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveAuthorizationCode(ctx, testAuthCode(t, "code-to-delete", time.Now().Add(5*time.Minute))))

	require.NoError(t, s.DeleteAuthorizationCode(ctx, "code-to-delete"))

	_, err := s.GetAuthorizationCode(ctx, "code-to-delete")
	require.ErrorIs(t, err, storage.ErrAuthorizationCodeNotFound)

	// Deleting an already-gone code is a harmless no-op.
	require.NoError(t, s.DeleteAuthorizationCode(ctx, "code-to-delete"))
}
