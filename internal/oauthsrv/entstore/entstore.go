// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

// Package entstore adapts the mcp-docstore ent client to the storage interfaces of
// github.com/giantswarm/mcp-oauth, so a single SQL database backs both the document store
// and the OAuth authorization server. It persists clients, tokens, and authorization flows
// through the same connection pool the rest of the service uses.
package entstore

import (
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

// Interface compliance for the parts implemented so far. The remaining storage interfaces
// (TokenStore, FlowStore, and the optional extensions) are added by later methods on Store.
var (
	_ storage.ClientStore     = (*Store)(nil)
	_ storage.ClientIPTracker = (*Store)(nil)
)

// New constructs a Store over the given ent client. enc encrypts sensitive token fields at
// rest (pass an encryptor with an empty key to disable encryption); providerTokenTTL is the
// lifetime applied to cached upstream provider tokens.
func New(c *ent.Client, enc *security.Encryptor, providerTokenTTL time.Duration) *Store {
	return &Store{client: c, enc: enc, providerTokenTTL: providerTokenTTL}
}
