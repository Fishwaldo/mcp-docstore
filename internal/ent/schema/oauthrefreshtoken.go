// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthRefreshToken is one issued refresh token, identified only by the SHA-256 hash of the
// token value so a leaked database can't be used to mint a valid token directly — the raw
// value is never stored. family_id and generation implement refresh token rotation:
// every refresh in a chain shares one family_id with an incrementing generation, so an
// incoming refresh at an older generation than the family has already reached is recognizable
// as reuse — a sign the token was stolen and both the legitimate client and an attacker are
// racing to use it — rather than a normal retry.
type OAuthRefreshToken struct{ ent.Schema }

func (OAuthRefreshToken) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (OAuthRefreshToken) Fields() []ent.Field {
	return []ent.Field{
		field.String("token_hash").Unique().Immutable().NotEmpty(),
		field.String("user_id").NotEmpty(),
		// Optional (default ""): the plain TokenStore.SaveRefreshToken has no client_id to
		// record, and an empty client_id is the intended value — the mcp-oauth server routes
		// storedClientID=="" to its OAuth 2.1 Section 6 "missing client binding" rejection
		// path. The family-aware SaveRefreshTokenWithFamily sets a real client_id.
		field.String("client_id").Optional(),
		field.String("family_id").Optional(),
		field.Int("generation").Default(0),
		field.Time("expires_at"),
	}
}

func (OAuthRefreshToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("family_id"),
		index.Fields("expires_at"), // the sweep scans by expiry
	}
}
