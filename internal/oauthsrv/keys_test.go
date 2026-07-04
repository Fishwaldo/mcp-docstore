// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package oauthsrv

import (
	"context"
	"database/sql"
	"encoding/hex"
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
	require.Equal(t, first.ConsentKey, second.ConsentKey)
}

func TestLoadOrCreateKeyMaterial_DerivationsDiffer(t *testing.T) {
	client := newTestEntClient(t)
	ctx := context.Background()

	km, err := LoadOrCreateKeyMaterial(ctx, client)
	require.NoError(t, err)

	require.NotEqual(t, km.EncryptionKey, km.ConsentKey)
}

// Known-answer constants captured by running the pre-removal derivation code
// (hkdf.New(sha256.New, master, nil, info) with derivedKeyBytes=32) against the fixed 32-byte
// master below, BEFORE the BFFSecret derivation (hkdfInfoBFFSecret) was deleted from keys.go.
// This test proves that removing the BFFSecret derivation did not disturb the other two: since
// each derivation uses HKDF-Expand with an independent info string and the same secret/salt,
// they are mathematically independent outputs, but this test is the empirical proof rather than
// an assertion of that fact.
const (
	knownAnswerMasterHex        = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	knownAnswerEncryptionKeyHex = "6c2abd2a4fde16ee443e3f96d48dd853c209c5522730ebf5f016f0bfca4f8ef4"
	knownAnswerConsentKeyHex    = "5c775011d616aaba84988e532d6c6ae64dc8aa0d617aba2e51f4ead33c11f5f9"
)

func TestDeriveKey_KnownAnswer(t *testing.T) {
	master, err := hex.DecodeString(knownAnswerMasterHex)
	require.NoError(t, err)
	require.Len(t, master, masterKeyBytes)

	enc, err := deriveKey(master, hkdfInfoEncryptionKey)
	require.NoError(t, err)
	require.Equal(t, knownAnswerEncryptionKeyHex, hex.EncodeToString(enc))

	consent, err := deriveKey(master, hkdfInfoConsentKey)
	require.NoError(t, err)
	require.Equal(t, knownAnswerConsentKeyHex, hex.EncodeToString(consent))
}
