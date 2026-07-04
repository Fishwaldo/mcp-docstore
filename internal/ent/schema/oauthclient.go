// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// OAuthClient is a dynamically registered OAuth 2.1 client. client_secret_hash holds only a
// hash for confidential clients — public clients (SPAs, CLIs, mobile apps) leave it empty
// since they authenticate via PKCE rather than a shared secret, so it is not required to be
// non-empty. redirect_uris, grant_types, response_types and scopes are JSON string lists
// because a client can register more than one of each. registration_access_token_hash lets
// the client that registered itself later read/update/delete its own registration without a
// hash lookup ever revealing the raw token from the database. registration_ip is indexed so
// registration-abuse checks can rate-limit by source IP.
type OAuthClient struct{ ent.Schema }

func (OAuthClient) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (OAuthClient) Fields() []ent.Field {
	return []ent.Field{
		field.String("client_id").Unique().Immutable().NotEmpty(),
		field.String("client_secret_hash").Sensitive(),
		field.String("client_type").NotEmpty(),
		field.JSON("redirect_uris", []string{}),
		field.String("token_endpoint_auth_method").NotEmpty(),
		field.JSON("grant_types", []string{}),
		field.JSON("response_types", []string{}),
		field.String("client_name").NotEmpty(),
		field.JSON("scopes", []string{}),
		field.String("registration_access_token_hash").Sensitive().Optional(),
		field.String("registration_ip").Optional(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (OAuthClient) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("registration_ip"),
	}
}
