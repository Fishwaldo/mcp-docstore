// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package entstore

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giantswarm/mcp-oauth/storage"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
)

// TestContract_AtomicGates adapts github.com/giantswarm/mcp-oauth's own backend-parity suite
// (storage/atomic_gates_parity_test.go, which checks the memory and valkey backends agree on
// these outcomes) to run against entstore.Store. The mcp-oauth server relies on these two
// single-use gates — AtomicCheckAndMarkAuthCodeUsed and AtomicGetAndDeleteRefreshToken — to
// enforce OAuth 2.1 §4.1.2 (authorization code single-use) and refresh token rotation/reuse
// detection; a SQL backend that let either gate award more than one winner would silently
// defeat both protections.
func TestContract_AtomicGates(t *testing.T) {
	t.Run("auth_code_first_use_then_reuse", func(t *testing.T) {
		s, _ := newTestEntStore(t)
		ctx := context.Background()

		code := &storage.AuthorizationCode{
			Code:        "contract-ac-1",
			ClientID:    "client-contract",
			RedirectURI: "https://app.example/cb",
			UserID:      "user-contract",
			CreatedAt:   time.Now(),
			ExpiresAt:   time.Now().Add(5 * time.Minute),
		}
		require.NoError(t, s.SaveAuthorizationCode(ctx, code))

		first, err := s.AtomicCheckAndMarkAuthCodeUsed(ctx, code.Code)
		require.NoError(t, err)
		require.NotNil(t, first)

		reused, err := s.AtomicCheckAndMarkAuthCodeUsed(ctx, code.Code)
		require.ErrorIs(t, err, storage.ErrAuthorizationCodeUsed)
		require.NotNil(t, reused, "reuse case must surface the stored code")
		require.True(t, reused.Used)
	})

	t.Run("auth_code_unknown_returns_not_found", func(t *testing.T) {
		s, _ := newTestEntStore(t)
		ctx := context.Background()

		got, err := s.AtomicCheckAndMarkAuthCodeUsed(ctx, "contract-ac-unknown")
		require.ErrorIs(t, err, storage.ErrAuthorizationCodeNotFound)
		require.Nil(t, got)
	})

	t.Run("refresh_token_first_use_then_reuse", func(t *testing.T) {
		s, _ := newTestEntStore(t)
		ctx := context.Background()

		refreshToken := "contract-rt-1"
		providerToken := &oauth2.Token{
			AccessToken:  "provider-access",
			RefreshToken: "provider-refresh",
			TokenType:    "Bearer",
			Expiry:       time.Now().Add(time.Hour),
		}
		// The provider token is cached under the refresh-token key: that is where the atomic
		// gate reads it back, matching how the mcp-oauth library re-saves it on each rotation.
		require.NoError(t, s.SaveToken(ctx, refreshToken, providerToken))
		require.NoError(t, s.SaveRefreshToken(ctx, refreshToken, "user-contract", time.Now().Add(time.Hour)))

		userID, _, gotToken, err := s.AtomicGetAndDeleteRefreshToken(ctx, refreshToken)
		require.NoError(t, err)
		require.Equal(t, "user-contract", userID)
		require.NotNil(t, gotToken)

		_, _, _, err = s.AtomicGetAndDeleteRefreshToken(ctx, refreshToken)
		require.ErrorIs(t, err, storage.ErrTokenNotFound)
	})

	t.Run("refresh_token_unknown_returns_not_found", func(t *testing.T) {
		s, _ := newTestEntStore(t)
		ctx := context.Background()

		_, _, _, err := s.AtomicGetAndDeleteRefreshToken(ctx, "contract-rt-unknown")
		require.ErrorIs(t, err, storage.ErrTokenNotFound, "backends MUST agree on the sentinel error type")
	})

	t.Run("auth_code_single_winner_race", func(t *testing.T) {
		const n = 100

		s, _ := newTestEntStore(t)
		ctx := context.Background()

		code := &storage.AuthorizationCode{
			Code:        "contract-ac-race",
			ClientID:    "client-contract",
			RedirectURI: "https://app.example/cb",
			UserID:      "user-contract",
			CreatedAt:   time.Now(),
			ExpiresAt:   time.Now().Add(5 * time.Minute),
		}
		require.NoError(t, s.SaveAuthorizationCode(ctx, code))

		var success, reuse atomic.Int32
		ready := make(chan struct{})
		g, gctx := errgroup.WithContext(ctx)
		for range n {
			g.Go(func() error {
				<-ready
				_, err := s.AtomicCheckAndMarkAuthCodeUsed(gctx, code.Code)
				switch {
				case err == nil:
					success.Add(1)
					return nil
				case errors.Is(err, storage.ErrAuthorizationCodeUsed):
					reuse.Add(1)
					return nil
				default:
					return err
				}
			})
		}
		close(ready)
		require.NoError(t, g.Wait())

		require.Equal(t, int32(1), success.Load(), "more than one success would break OAuth 2.1 §4.1.2")
		require.Equal(t, int32(n-1), reuse.Load())
	})

	t.Run("refresh_token_single_winner_race", func(t *testing.T) {
		const n = 100

		s, _ := newTestEntStore(t)
		ctx := context.Background()

		const refreshToken = "contract-rt-race"
		providerToken := &oauth2.Token{
			AccessToken:  "provider-access",
			RefreshToken: "provider-refresh",
			TokenType:    "Bearer",
			Expiry:       time.Now().Add(time.Hour),
		}
		// The provider token is cached under the refresh-token key: that is where the atomic
		// gate reads it back, matching how the mcp-oauth library re-saves it on each rotation.
		require.NoError(t, s.SaveToken(ctx, refreshToken, providerToken))
		require.NoError(t, s.SaveRefreshToken(ctx, refreshToken, "user-contract", time.Now().Add(time.Hour)))

		var success, notFound atomic.Int32
		ready := make(chan struct{})
		g, gctx := errgroup.WithContext(ctx)
		for i := range n {
			g.Go(func() error {
				<-ready
				_, _, _, err := s.AtomicGetAndDeleteRefreshToken(gctx, refreshToken)
				switch {
				case err == nil:
					success.Add(1)
					return nil
				case errors.Is(err, storage.ErrTokenNotFound):
					notFound.Add(1)
					return nil
				default:
					return fmt.Errorf("goroutine %d: %w", i, err)
				}
			})
		}
		close(ready)
		require.NoError(t, g.Wait())

		require.Equal(t, int32(1), success.Load(), "more than one success would allow refresh token replay")
		require.Equal(t, int32(n-1), notFound.Load())
	})
}
