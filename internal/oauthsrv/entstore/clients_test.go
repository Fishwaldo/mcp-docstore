// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package entstore

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/giantswarm/mcp-oauth/security"
	"github.com/giantswarm/mcp-oauth/storage"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite" // registers "sqlite" driver

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
)

// newTestEntStore builds a Store backed by a fresh named-shared in-memory SQLite database
// with a real AES-256 encryptor and a 24h provider-token TTL. It returns the Store and the
// underlying ent client so tests can inspect rows directly.
func newTestEntStore(t *testing.T) (*Store, *ent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Schema.Create(context.Background()))

	enc, err := security.NewEncryptor([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)

	return New(client, enc, 24*time.Hour), client
}

func bcryptHash(t *testing.T, secret string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	require.NoError(t, err)
	return string(h)
}

func TestSaveClientRoundTrip(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	want := &storage.Client{
		ClientID:                    "client-abc",
		ClientSecretHash:            bcryptHash(t, "s3cret"),
		ClientType:                  storage.ClientTypeConfidential,
		RedirectURIs:                []string{"https://app.example/cb", "https://app.example/cb2"},
		TokenEndpointAuthMethod:     "client_secret_basic",
		GrantTypes:                  []string{"authorization_code", "refresh_token"},
		ResponseTypes:               []string{"code"},
		ClientName:                  "Example App",
		Scopes:                      []string{"openid", "profile"},
		RegistrationAccessTokenHash: bcryptHash(t, "rat"),
	}
	require.NoError(t, s.SaveClient(ctx, want))

	got, err := s.GetClient(ctx, "client-abc")
	require.NoError(t, err)
	require.Equal(t, want.ClientID, got.ClientID)
	require.Equal(t, want.ClientSecretHash, got.ClientSecretHash)
	require.Equal(t, want.ClientType, got.ClientType)
	require.Equal(t, want.RedirectURIs, got.RedirectURIs)
	require.Equal(t, want.TokenEndpointAuthMethod, got.TokenEndpointAuthMethod)
	require.Equal(t, want.GrantTypes, got.GrantTypes)
	require.Equal(t, want.ResponseTypes, got.ResponseTypes)
	require.Equal(t, want.ClientName, got.ClientName)
	require.Equal(t, want.Scopes, got.Scopes)
	require.Equal(t, want.RegistrationAccessTokenHash, got.RegistrationAccessTokenHash)
	require.False(t, got.CreatedAt.IsZero())
	require.False(t, got.UpdatedAt.IsZero())
}

func TestSaveClientUpsert(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveClient(ctx, &storage.Client{
		ClientID:                "client-upsert",
		ClientSecretHash:        bcryptHash(t, "old"),
		ClientType:              storage.ClientTypeConfidential,
		RedirectURIs:            []string{"https://old.example/cb"},
		TokenEndpointAuthMethod: "client_secret_basic",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Old Name",
		Scopes:                  []string{"openid"},
	}))

	require.NoError(t, s.SaveClient(ctx, &storage.Client{
		ClientID:                "client-upsert",
		ClientSecretHash:        bcryptHash(t, "new"),
		ClientType:              storage.ClientTypeConfidential,
		RedirectURIs:            []string{"https://new.example/cb"},
		TokenEndpointAuthMethod: "client_secret_post",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              "New Name",
		Scopes:                  []string{"openid", "email"},
	}))

	got, err := s.GetClient(ctx, "client-upsert")
	require.NoError(t, err)
	require.Equal(t, "New Name", got.ClientName)
	require.Equal(t, []string{"https://new.example/cb"}, got.RedirectURIs)
	require.Equal(t, "client_secret_post", got.TokenEndpointAuthMethod)
	require.Equal(t, []string{"openid", "email"}, got.Scopes)

	all, err := s.ListClients(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
}

func TestSaveClientPreservesRegistrationIP(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveClient(ctx, &storage.Client{
		ClientID:                "keep-ip",
		ClientType:              storage.ClientTypePublic,
		RedirectURIs:            []string{"https://spa.example/cb"},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Before",
		Scopes:                  []string{"openid"},
	}))

	const ip = "203.0.113.7"
	require.NoError(t, s.TrackClientIP(ctx, "keep-ip", ip))

	// Re-save the same client_id with a changed field. registration_ip is
	// schema-mutable but must NOT be touched by SaveClient's update path.
	require.NoError(t, s.SaveClient(ctx, &storage.Client{
		ClientID:                "keep-ip",
		ClientType:              storage.ClientTypePublic,
		RedirectURIs:            []string{"https://spa.example/cb"},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		ClientName:              "After",
		Scopes:                  []string{"openid"},
	}))

	// The changed field took effect.
	got, err := s.GetClient(ctx, "keep-ip")
	require.NoError(t, err)
	require.Equal(t, "After", got.ClientName)

	// The registration IP survived the update: the row is still counted for it.
	require.ErrorIs(t, s.CheckIPLimit(ctx, ip, 1), storage.ErrClientIPLimitExceeded)
}

func TestGetClientNotFound(t *testing.T) {
	s, _ := newTestEntStore(t)

	_, err := s.GetClient(context.Background(), "missing")
	require.ErrorIs(t, err, storage.ErrClientNotFound)
}

func TestValidateClientSecret(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveClient(ctx, &storage.Client{
		ClientID:                "conf",
		ClientSecretHash:        bcryptHash(t, "correct-secret"),
		ClientType:              storage.ClientTypeConfidential,
		RedirectURIs:            []string{"https://app.example/cb"},
		TokenEndpointAuthMethod: "client_secret_basic",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Confidential",
		Scopes:                  []string{"openid"},
	}))

	// Correct secret succeeds.
	require.NoError(t, s.ValidateClientSecret(ctx, "conf", "correct-secret"))

	// Wrong secret fails.
	wrongErr := s.ValidateClientSecret(ctx, "conf", "wrong-secret")
	require.Error(t, wrongErr)

	// Missing client fails, and the error must be INDISTINGUISHABLE from the
	// wrong-secret case (anti-enumeration): it is not storage.ErrClientNotFound.
	missingErr := s.ValidateClientSecret(ctx, "does-not-exist", "any-secret")
	require.Error(t, missingErr)
	require.NotErrorIs(t, missingErr, storage.ErrClientNotFound)
	require.Equal(t, wrongErr.Error(), missingErr.Error())
}

func TestValidateClientSecretPublicClient(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveClient(ctx, &storage.Client{
		ClientID:                "pub",
		ClientType:              storage.ClientTypePublic,
		RedirectURIs:            []string{"https://spa.example/cb"},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Public SPA",
		Scopes:                  []string{"openid"},
	}))

	// Public clients authenticate via PKCE, so any secret validates.
	require.NoError(t, s.ValidateClientSecret(ctx, "pub", ""))
	require.NoError(t, s.ValidateClientSecret(ctx, "pub", "anything"))
}

func TestDeleteClient(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveClient(ctx, &storage.Client{
		ClientID:                "to-delete",
		ClientType:              storage.ClientTypePublic,
		RedirectURIs:            []string{"https://spa.example/cb"},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Doomed",
		Scopes:                  []string{"openid"},
	}))

	require.NoError(t, s.DeleteClient(ctx, "to-delete"))

	_, err := s.GetClient(ctx, "to-delete")
	require.ErrorIs(t, err, storage.ErrClientNotFound)

	// Deleting a missing client returns ErrClientNotFound.
	require.ErrorIs(t, s.DeleteClient(ctx, "to-delete"), storage.ErrClientNotFound)
}

func TestCheckIPLimit(t *testing.T) {
	s, client := newTestEntStore(t)
	ctx := context.Background()

	const ip = "203.0.113.7"
	// Seed two clients registered from the same IP.
	for _, id := range []string{"ip-1", "ip-2"} {
		require.NoError(t, s.SaveClient(ctx, &storage.Client{
			ClientID:                id,
			ClientType:              storage.ClientTypePublic,
			RedirectURIs:            []string{"https://spa.example/cb"},
			TokenEndpointAuthMethod: "none",
			GrantTypes:              []string{"authorization_code"},
			ResponseTypes:           []string{"code"},
			ClientName:              id,
			Scopes:                  []string{"openid"},
		}))
		require.NoError(t, s.TrackClientIP(ctx, id, ip))
	}
	_ = client

	// Below the limit: 2 clients, max 3.
	require.NoError(t, s.CheckIPLimit(ctx, ip, 3))

	// At the limit: 2 clients, max 2 -> exceeded.
	require.ErrorIs(t, s.CheckIPLimit(ctx, ip, 2), storage.ErrClientIPLimitExceeded)

	// Above the limit: 2 clients, max 1 -> exceeded.
	require.ErrorIs(t, s.CheckIPLimit(ctx, ip, 1), storage.ErrClientIPLimitExceeded)

	// Disabled: max <= 0 always passes.
	require.NoError(t, s.CheckIPLimit(ctx, ip, 0))
	require.NoError(t, s.CheckIPLimit(ctx, ip, -1))

	// A different IP has no registrations.
	require.NoError(t, s.CheckIPLimit(ctx, "198.51.100.9", 1))
}

func TestTrackClientIP(t *testing.T) {
	s, _ := newTestEntStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveClient(ctx, &storage.Client{
		ClientID:                "track-me",
		ClientType:              storage.ClientTypePublic,
		RedirectURIs:            []string{"https://spa.example/cb"},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Track",
		Scopes:                  []string{"openid"},
	}))

	const ip = "192.0.2.44"
	require.NoError(t, s.TrackClientIP(ctx, "track-me", ip))

	// The IP is now counted for that client.
	require.ErrorIs(t, s.CheckIPLimit(ctx, ip, 1), storage.ErrClientIPLimitExceeded)

	// Tracking a missing client returns ErrClientNotFound.
	require.True(t, errors.Is(s.TrackClientIP(ctx, "no-such-client", ip), storage.ErrClientNotFound))
}
