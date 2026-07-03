// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package entstore adapts the mcp-docstore ent client to the storage interfaces of
// github.com/giantswarm/mcp-oauth, so a single SQL database backs both the document store
// and the OAuth authorization server. It persists clients, tokens, and authorization flows
// through the same connection pool the rest of the service uses.
package entstore

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/giantswarm/mcp-oauth/security"
	"github.com/giantswarm/mcp-oauth/storage"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
)

// Store is an ent-backed implementation of the mcp-oauth storage interfaces. It holds the
// generated ent client, an optional encryptor used to protect sensitive token material at
// rest, and the lifetime applied to cached upstream provider tokens.
type Store struct {
	client           *ent.Client
	enc              *security.Encryptor
	providerTokenTTL time.Duration
}

// Store implements the full mcp-oauth storage.Combined interface (TokenStore, ClientStore,
// and FlowStore), plus the optional ClientIPTracker, RefreshTokenFamilyStore,
// RefreshTokenFamilyByIDStore, RevokedTokenStore, TokenRevocationStore, TokenMetadataStore,
// and TokenMetadataGetter extensions.
var (
	_ storage.Combined                    = (*Store)(nil)
	_ storage.ClientIPTracker             = (*Store)(nil)
	_ storage.RefreshTokenFamilyStore     = (*Store)(nil)
	_ storage.RefreshTokenFamilyByIDStore = (*Store)(nil)
	_ storage.RevokedTokenStore           = (*Store)(nil)
	_ storage.TokenRevocationStore        = (*Store)(nil)
	_ storage.TokenMetadataStore          = (*Store)(nil)
	_ storage.TokenMetadataGetter         = (*Store)(nil)
)

// New constructs a Store over the given ent client. enc encrypts sensitive token fields at
// rest (pass an encryptor with an empty key to disable encryption); providerTokenTTL is the
// lifetime applied to cached upstream provider tokens.
func New(c *ent.Client, enc *security.Encryptor, providerTokenTTL time.Duration) *Store {
	return &Store{client: c, enc: enc, providerTokenTTL: providerTokenTTL}
}

// hashToken returns the lowercase hex SHA-256 of a raw token value. Refresh tokens are
// looked up and stored by this hash, never by their raw value, mirroring
// internal/store/session.go's rationale: a leaked database must not yield a usable
// credential directly.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
