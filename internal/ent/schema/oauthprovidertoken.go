// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthProviderToken is the AS's cached copy of the upstream identity provider's token set
// for a user, refreshed silently so the AS can call back into the provider (e.g. to revalidate
// a session or fetch fresh claims) without re-running the browser login every time. token_json
// is the serialized, encrypted token set; expires_at is the cache lifetime the AS applies to
// this stored copy (now + a configured TTL, renewed on every re-save), after which the entry
// is treated as absent and must be re-fetched or re-issued.
type OAuthProviderToken struct{ ent.Schema }

func (OAuthProviderToken) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (OAuthProviderToken) Fields() []ent.Field {
	return []ent.Field{
		// The lookup key is never a raw identifier: the AS library keys this row by raw
		// access/refresh tokens (and, at login, by the user id), and the adapter stores the
		// SHA-256 hash of whichever value it is handed. Sensitive so it never appears in logs
		// or error strings, matching the hashed refresh-token column.
		field.String("user_id").Unique().NotEmpty().Sensitive(),
		// Text, not String: an encrypted JSON blob of the full upstream token set can exceed
		// a typical VARCHAR(255) column once an id_token JWT is included.
		field.Text("token_json").Sensitive().NotEmpty(),
		field.Time("expires_at"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (OAuthProviderToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at"),
	}
}
