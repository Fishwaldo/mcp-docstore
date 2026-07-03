// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// OAuthKey is the authorization server's persistent cryptographic root, stored as a single
// row shared by every replica of the service. singleton is always set to 1 by the code that
// creates this row and is unique, so the database itself rejects a second row: whichever
// process loses the create race on first boot re-reads the winner's row instead of minting
// its own key material. ec_private_key_pem
// is the ES256 access-token signing key (PEM-encoded); kid is its stable JWK key ID.
// master_secret is 32 random bytes (hex-encoded) that every other secret the server needs —
// at-rest encryption, the first-party web client secret, consent-cookie signing — is
// derived from via HKDF, so those never need their own row or config value. All key
// material is immutable: rotation means writing a new row and switching the singleton, not
// editing this one in place.
type OAuthKey struct{ ent.Schema }

func (OAuthKey) Mixin() []ent.Mixin { return []ent.Mixin{BaseMixin{}} }

func (OAuthKey) Fields() []ent.Field {
	return []ent.Field{
		field.Int("singleton").Unique(),
		field.String("ec_private_key_pem").Sensitive().NotEmpty().Immutable(),
		field.String("kid").NotEmpty().Immutable(),
		field.String("master_secret").Sensitive().NotEmpty().Immutable(),
	}
}
