// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type DocumentSnapshot struct{ ent.Schema }

func (DocumentSnapshot) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (DocumentSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.Int("version"),
		field.Text("overview"),
		field.Text("body"),
		field.JSON("tags", []string{}).Optional(),
		field.Text("comment").Optional(),
	}
}

func (DocumentSnapshot) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("document", Document.Type).Ref("snapshots").Unique().Required(),
		edge.To("created_by", User.Type).Unique(),
	}
}
