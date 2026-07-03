// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package oauthsrv

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

// newTestEntClient mirrors internal/oauthsrv/entstore's newTestEntStore DSN pattern: a named
// shared in-memory SQLite database, since a private ":memory:" DSN would give each pooled
// connection its own empty database.
func newTestEntClient(t *testing.T) *ent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	drv := entsql.OpenDB(dialect.SQLite, db)
	client := ent.NewClient(ent.Driver(drv))
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Schema.Create(context.Background()))
	return client
}

func TestLoadOrCreateKeyMaterial_FirstCallCreates(t *testing.T) {
	client := newTestEntClient(t)
	ctx := context.Background()

	km, err := LoadOrCreateKeyMaterial(ctx, client)
	require.NoError(t, err)
	require.NotNil(t, km)

	require.NotNil(t, km.Signer)
	require.Equal(t, "P-256", km.Signer.Curve.Params().Name)
	require.NotEmpty(t, km.KID)
	require.Len(t, km.EncryptionKey, 32)
	require.NotEmpty(t, km.BFFSecret)
	require.Len(t, km.ConsentKey, 32)
}

func TestLoadOrCreateKeyMaterial_SecondCallPersists(t *testing.T) {
	client := newTestEntClient(t)
	ctx := context.Background()

	first, err := LoadOrCreateKeyMaterial(ctx, client)
	require.NoError(t, err)

	second, err := LoadOrCreateKeyMaterial(ctx, client)
	require.NoError(t, err)

	require.Equal(t, first.Signer.D, second.Signer.D)
	require.Equal(t, first.KID, second.KID)
	require.Equal(t, first.EncryptionKey, second.EncryptionKey)
	require.Equal(t, first.BFFSecret, second.BFFSecret)
	require.Equal(t, first.ConsentKey, second.ConsentKey)
}

func TestLoadOrCreateKeyMaterial_FreshCallsOnSameDBAgree(t *testing.T) {
	client := newTestEntClient(t)
	ctx := context.Background()

	first, err := LoadOrCreateKeyMaterial(ctx, client)
	require.NoError(t, err)

	second, err := LoadOrCreateKeyMaterial(ctx, client)
	require.NoError(t, err)

	require.Equal(t, first.KID, second.KID)
	require.Equal(t, first.EncryptionKey, second.EncryptionKey)
	require.Equal(t, first.BFFSecret, second.BFFSecret)
	require.Equal(t, first.ConsentKey, second.ConsentKey)
}

func TestLoadOrCreateKeyMaterial_DerivationsDiffer(t *testing.T) {
	client := newTestEntClient(t)
	ctx := context.Background()

	km, err := LoadOrCreateKeyMaterial(ctx, client)
	require.NoError(t, err)

	require.NotEqual(t, km.EncryptionKey, km.ConsentKey)
	require.NotEqual(t, string(km.EncryptionKey), km.BFFSecret)
	require.NotEqual(t, string(km.ConsentKey), km.BFFSecret)
}
