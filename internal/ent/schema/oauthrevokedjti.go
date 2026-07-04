// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthRevokedJTI is a denylist of revoked JWT IDs (the jti claim) for access tokens the AS
// issued as self-contained JWTs rather than opaque references. A JWT can't be deleted once
// issued, so revoking one ahead of its natural expiry means recording its jti here and
// checking it on every introspection. expires_at mirrors the token's own expiry so the row
// can be purged once the JWT would have expired anyway, revoked or not.
type OAuthRevokedJTI struct{ ent.Schema }

func (OAuthRevokedJTI) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (OAuthRevokedJTI) Fields() []ent.Field {
	return []ent.Field{
		field.String("jti").Unique().Immutable().NotEmpty(),
		field.Time("expires_at"),
	}
}

func (OAuthRevokedJTI) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at"), // the sweep scans by expiry
	}
}
