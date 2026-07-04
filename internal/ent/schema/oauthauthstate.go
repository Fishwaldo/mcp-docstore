// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthAuthState is a short-lived record of one authorization_code flow in progress. It
// bridges the value we hand back to our own client as `state` (state_id) to the parallel
// flow we run against the upstream identity provider (provider_state, provider_code_verifier
// — our own PKCE pair against the provider, independent of any PKCE our client used against
// us). original_client_state preserves the client's own `state` parameter, if it sent one,
// so it can be echoed back untouched once the flow completes. expires_at is short (minutes)
// and indexed so a sweep can purge flows the browser never came back to complete.
type OAuthAuthState struct{ ent.Schema }

func (OAuthAuthState) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (OAuthAuthState) Fields() []ent.Field {
	return []ent.Field{
		field.String("state_id").Unique().Immutable().NotEmpty(),
		field.String("original_client_state").Optional(),
		field.String("client_id").NotEmpty(),
		field.String("redirect_uri").NotEmpty(),
		field.String("scope").Optional(),
		field.String("resource").Optional(),
		field.String("code_challenge").Optional(),
		field.String("code_challenge_method").Optional(),
		field.String("provider_state").Unique().NotEmpty(),
		field.String("provider_code_verifier").Sensitive().NotEmpty(),
		field.String("nonce").Optional(),
		field.Time("expires_at"),
	}
}

func (OAuthAuthState) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at"), // the sweep scans by expiry
	}
}
