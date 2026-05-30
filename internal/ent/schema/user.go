// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type User struct{ ent.Schema }

func (User) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (User) Fields() []ent.Field {
	return []ent.Field{
		// IdP subject ("sub"); globally unique → a user belongs to exactly one tenant.
		field.String("external_subject").Unique().NotEmpty(),
		field.String("email").NotEmpty(),
		field.Enum("role").Values("admin", "member").Default("member"),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("users").Unique().Required(),
	}
}
