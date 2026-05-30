package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ProjectShare struct{ ent.Schema }

func (ProjectShare) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (ProjectShare) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("permission").Values("read", "write"),
	}
}

func (ProjectShare) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("shares").Unique().Required(),
		edge.To("user", User.Type).Unique().Required(),
		edge.To("created_by", User.Type).Unique(),
	}
}

func (ProjectShare) Indexes() []ent.Index {
	return []ent.Index{
		index.Edges("project", "user").Unique(),
	}
}
