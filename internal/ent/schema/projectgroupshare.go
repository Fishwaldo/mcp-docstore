// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ProjectGroupShare struct{ ent.Schema }

func (ProjectGroupShare) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (ProjectGroupShare) Fields() []ent.Field {
	return []ent.Field{
		field.String("group_name").NotEmpty(),
		field.Enum("permission").Values("read", "write"),
	}
}

func (ProjectGroupShare) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("group_shares").Unique().Required(),
		edge.To("created_by", User.Type).Unique(),
	}
}

func (ProjectGroupShare) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("group_name").Edges("project").Unique(),
	}
}
