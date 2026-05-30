package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

type Document struct{ ent.Schema }

func (Document) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (Document) Fields() []ent.Field {
	return []ent.Field{
		// Denormalized tenant id for direct filtering and (Phase 2) search indexing.
		field.UUID("tenant_id", uuid.UUID{}),
		field.String("title").NotEmpty(),
		field.Text("overview"),
		field.Text("body"),
		field.String("content_type").Default("text/markdown"),
		field.JSON("tags", []string{}).Optional(),
		field.Int("version").Default(1),
		field.Text("change_comment").Optional(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Document) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("documents").Unique().Required(),
		edge.To("created_by", User.Type).Unique(),
		edge.To("updated_by", User.Type).Unique(),
		// Deleting a document removes its snapshots (ON DELETE CASCADE).
		edge.To("snapshots", DocumentSnapshot.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Document) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id"),
	}
}
