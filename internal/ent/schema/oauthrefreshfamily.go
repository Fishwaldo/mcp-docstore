// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthRefreshFamily tracks the current state of one refresh token rotation chain: one row
// per family_id shared by every OAuthRefreshToken descended from the same initial grant.
// generation records the highest generation issued in the family, so an incoming refresh
// presenting an older generation is recognizable as reuse. revoked (with revoked_at) lets the
// AS kill an entire chain in a single write — e.g. on detected reuse, or on logout — instead
// of having to find and revoke every token row that belongs to the family.
type OAuthRefreshFamily struct{ ent.Schema }

func (OAuthRefreshFamily) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (OAuthRefreshFamily) Fields() []ent.Field {
	return []ent.Field{
		field.String("family_id").Unique().Immutable().NotEmpty(),
		field.String("user_id").NotEmpty(),
		field.String("client_id").NotEmpty(),
		field.Int("generation"),
		field.Time("issued_at").Immutable(),
		field.Bool("revoked").Default(false),
		field.Time("revoked_at").Optional(),
	}
}

func (OAuthRefreshFamily) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
	}
}
