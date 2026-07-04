// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthTokenMetadata is the AS's introspection record for every access token it issues,
// independent of how the token itself is represented on the wire (opaque or JWT) — it is what
// backs token introspection and bulk revocation. The composite (user_id, client_id) index
// lets the AS revoke every token a client holds for a user in one query — e.g. on
// logout-everywhere or client de-authorization — without a full table scan. extra_claims
// holds any additional claims attached to the token beyond the fixed columns, so
// introspection can echo them back without a schema change per new claim.
type OAuthTokenMetadata struct{ ent.Schema }

func (OAuthTokenMetadata) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (OAuthTokenMetadata) Fields() []ent.Field {
	return []ent.Field{
		field.String("token_id").Unique().Immutable().NotEmpty(),
		field.String("user_id").NotEmpty(),
		field.String("client_id").NotEmpty(),
		field.Time("issued_at"),
		field.Time("expires_at").Optional(),
		field.String("token_type").NotEmpty(),
		field.String("audience").Optional(),
		field.JSON("scopes", []string{}),
		field.String("family_id").Optional(),
		field.String("jkt").Optional(),
		field.JSON("extra_claims", map[string]any{}).Optional(),
	}
}

func (OAuthTokenMetadata) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("client_id"),
		index.Fields("expires_at"),
		index.Fields("user_id", "client_id"), // powers TokenRevocationStore lookups
	}
}
