// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthConsent is the audit trail of human approvals for third-party OAuth clients riding the
// embedded authorization server's single upstream IdP registration. Every dynamically
// registered client shares that one upstream registration, so without an explicit approval
// step any such client could silently trigger a login redirect and harvest the resulting
// identity (a confused-deputy attack) — this row is the record that a browser explicitly
// approved a given client_id before that redirect was allowed to happen.
//
// user_id is written empty: approval happens on the consent page, before the browser has
// completed the upstream login that would resolve an identity, so the grantor is unknown at
// write time. The (user_id, client_id) unique index turns repeat approvals of the same client
// into an update of one row (refreshed client_name and granted_at) rather than an
// ever-growing log.
type OAuthConsent struct{ ent.Schema }

func (OAuthConsent) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (OAuthConsent) Fields() []ent.Field {
	return []ent.Field{
		field.String("user_id"),
		field.String("client_id").NotEmpty(),
		field.String("client_name").NotEmpty(),
		field.Time("granted_at").Default(time.Now),
	}
}

func (OAuthConsent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "client_id").Unique(),
	}
}
