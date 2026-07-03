// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthAuthCode is a one-time authorization code issued after a completed login, redeemed
// exactly once at the token endpoint for a token set. provider_token carries the upstream
// identity provider's token set (encrypted at rest) so the redemption can mint an access
// token backed by a real upstream session without a second round trip through the browser.
// used flags the row after redemption instead of deleting it, so a replayed code is
// recognizable as reuse rather than looking like a code that never existed. expires_at is
// short-lived and indexed so a sweep can purge codes nobody ever redeemed.
type OAuthAuthCode struct{ ent.Schema }

func (OAuthAuthCode) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (OAuthAuthCode) Fields() []ent.Field {
	return []ent.Field{
		field.String("code").Unique().Immutable().NotEmpty(),
		field.String("client_id").NotEmpty(),
		field.String("redirect_uri").NotEmpty(),
		field.String("scope").Optional(),
		field.String("resource").Optional(),
		field.String("audience").Optional(),
		field.String("code_challenge").Optional(),
		field.String("code_challenge_method").Optional(),
		field.String("user_id").NotEmpty(),
		// Text, not String: this is an encrypted JSON blob of the upstream token set, which
		// can exceed a typical VARCHAR(255) column once an id_token JWT is included.
		field.Text("provider_token").Sensitive().Optional(),
		field.Time("expires_at"),
		field.Bool("used").Default(false),
	}
}

func (OAuthAuthCode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at"), // the sweep scans by expiry
	}
}
