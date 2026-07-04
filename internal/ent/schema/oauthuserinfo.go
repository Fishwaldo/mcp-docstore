// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthUserInfo is a cached copy of the upstream OIDC userinfo claims for a user, so minting
// an access token doesn't require a userinfo round trip to the provider every time. info_json
// is the serialized, encrypted claims; expires_at bounds how stale the cached claims may get
// before the AS re-fetches them.
type OAuthUserInfo struct{ ent.Schema }

func (OAuthUserInfo) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (OAuthUserInfo) Fields() []ent.Field {
	return []ent.Field{
		field.String("user_id").Unique().NotEmpty(),
		// Text, not String: an encrypted JSON blob of userinfo claims (e.g. group
		// memberships) can exceed a typical VARCHAR(255) column.
		field.Text("info_json").Sensitive().NotEmpty(),
		field.Time("expires_at"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (OAuthUserInfo) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at"),
	}
}
