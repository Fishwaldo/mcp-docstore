// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package entstore

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // registers "sqlite" driver

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
)

// TestSchemaMigrateAndRoundTrip is a smoke test for the OAuth AS ent schemas: it migrates a
// fresh database and does a trivial create + read-back on OAuthClient via the generated ent
// API, with no entstore adapter code involved yet.
func TestSchemaMigrateAndRoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	require.NoError(t, client.Schema.Create(ctx))

	created, err := client.OAuthClient.Create().
		SetClientID("client-123").
		SetClientSecretHash("hash-of-secret").
		SetClientType("confidential").
		SetRedirectUris([]string{"https://example.com/callback"}).
		SetTokenEndpointAuthMethod("client_secret_basic").
		SetGrantTypes([]string{"authorization_code", "refresh_token"}).
		SetResponseTypes([]string{"code"}).
		SetClientName("Test Client").
		SetScopes([]string{"openid", "profile"}).
		Save(ctx)
	require.NoError(t, err)

	fetched, err := client.OAuthClient.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "client-123", fetched.ClientID)
	require.Equal(t, []string{"https://example.com/callback"}, fetched.RedirectUris)
	require.Equal(t, "confidential", fetched.ClientType)
}
