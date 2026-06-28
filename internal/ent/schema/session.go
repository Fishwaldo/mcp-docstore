// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Session is a browser login session for the web UI (BFF transport). The cookie carries a
// random token; only the token's hash is stored here, so reading this table cannot mint a
// valid cookie. subject/email/groups are the claims snapshot the per-request middleware
// re-resolves identity from; the OIDC token set is held server-side for silent refresh.
// expires_at is the sliding idle deadline; absolute_expires_at is the immutable hard cap.
type Session struct{ ent.Schema }

func (Session) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("token_hash").Unique().NotEmpty(),
		field.String("subject").NotEmpty(),
		field.String("email").NotEmpty(),
		field.JSON("groups", []string{}).Optional(),
		field.Text("id_token").Optional(),
		field.Text("access_token").Optional(),
		field.Text("refresh_token").Optional(),
		field.Time("token_expiry").Optional(),
		field.Time("last_seen_at"),
		field.Time("expires_at"),
		field.Time("absolute_expires_at").Immutable(),
	}
}

func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at"), // the sweep scans by expiry
	}
}
