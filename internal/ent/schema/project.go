// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type Project struct{ ent.Schema }

func (Project) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (Project) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("description").Optional(),
		field.Enum("visibility").Values("org", "private").Default("private"),
		// archived projects are hidden from list_projects and search (reversible; no delete).
		field.Bool("archived").Default(false),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Project) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("projects").Unique().Required(),
		edge.To("owner", User.Type).Unique().Required(),
		edge.To("shares", ProjectShare.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("group_shares", ProjectGroupShare.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("documents", Document.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
