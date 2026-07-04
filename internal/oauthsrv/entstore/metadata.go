// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package entstore

import (
	"context"
	"errors"
	"time"

	"github.com/giantswarm/mcp-oauth/storage"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrefreshfamily"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrefreshtoken"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrevokedjti"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthtokenmetadata"
)

// SaveTokenMetadata upserts the introspection record for tokenID — an access or refresh
// token's raw bearer value — keyed by its SHA-256 hash so the row itself never holds a live
// credential, mirroring hashToken's use for refresh tokens elsewhere in this package. IssuedAt
// and ExpiresAt are persisted exactly as given; callers own their semantics (a zero ExpiresAt
// means "unknown", per storage.TokenMetadata's own doc, and is stored as-is).
func (s *Store) SaveTokenMetadata(ctx context.Context, tokenID string, md storage.TokenMetadata) error {
	if tokenID == "" || md.UserID == "" || md.ClientID == "" {
		return errors.New("tokenID, userID, and clientID cannot be empty")
	}

	hash := hashToken(tokenID)
	existing, err := s.client.OAuthTokenMetadata.Query().Where(oauthtokenmetadata.TokenID(hash)).Only(ctx)
	switch {
	case err == nil:
		return applyTokenMetadata(existing.Update(), md).Exec(ctx)
	case ent.IsNotFound(err):
		return s.client.OAuthTokenMetadata.Create().
			SetTokenID(hash).
			SetUserID(md.UserID).
			SetClientID(md.ClientID).
			SetIssuedAt(md.IssuedAt).
			SetExpiresAt(md.ExpiresAt).
			SetTokenType(md.TokenType).
			SetAudience(md.Audience).
			SetScopes(md.Scopes).
			SetFamilyID(md.FamilyID).
			SetJkt(md.JKT).
			SetExtraClaims(md.ExtraClaims).
			Exec(ctx)
	default:
		return err
	}
}

func applyTokenMetadata(upd *ent.OAuthTokenMetadataUpdateOne, md storage.TokenMetadata) *ent.OAuthTokenMetadataUpdateOne {
	return upd.
		SetUserID(md.UserID).
		SetClientID(md.ClientID).
		SetIssuedAt(md.IssuedAt).
		SetExpiresAt(md.ExpiresAt).
		SetTokenType(md.TokenType).
		SetAudience(md.Audience).
		SetScopes(md.Scopes).
		SetFamilyID(md.FamilyID).
		SetJkt(md.JKT).
		SetExtraClaims(md.ExtraClaims)
}

// GetTokenMetadata retrieves the introspection record for tokenID, looked up by the same hash
// SaveTokenMetadata stored it under. Per the storage.TokenMetadataGetter contract this method
// takes no context — that is the library's own signature, not an inconsistency on our part —
// so it uses context.Background() internally rather than plumbing a request context it was
// never given.
func (s *Store) GetTokenMetadata(tokenID string) (*storage.TokenMetadata, error) {
	row, err := s.client.OAuthTokenMetadata.Query().
		Where(oauthtokenmetadata.TokenID(hashToken(tokenID))).
		Only(context.Background())
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New("token metadata not found")
		}
		return nil, err
	}
	return &storage.TokenMetadata{
		UserID:      row.UserID,
		ClientID:    row.ClientID,
		IssuedAt:    row.IssuedAt,
		ExpiresAt:   row.ExpiresAt,
		TokenType:   row.TokenType,
		Audience:    row.Audience,
		Scopes:      row.Scopes,
		FamilyID:    row.FamilyID,
		JKT:         row.Jkt,
		ExtraClaims: row.ExtraClaims,
	}, nil
}

// RevokeJTI upserts jti into the self-issued-JWT denylist until expiresAt (RFC 7009). jti is
// stored as given: unlike a refresh token or opaque access token it is not itself a usable
// bearer credential, just a random identifier, so there is nothing gained by hashing it — and
// hashing it would only get in the way of the exact-match lookup IsJTIRevoked (and the
// validation hot path calling it) needs to perform.
func (s *Store) RevokeJTI(ctx context.Context, jti string, expiresAt time.Time) error {
	if jti == "" {
		return errors.New("jti cannot be empty")
	}
	existing, err := s.client.OAuthRevokedJTI.Query().Where(oauthrevokedjti.Jti(jti)).Only(ctx)
	switch {
	case err == nil:
		return existing.Update().SetExpiresAt(expiresAt).Exec(ctx)
	case ent.IsNotFound(err):
		return s.client.OAuthRevokedJTI.Create().SetJti(jti).SetExpiresAt(expiresAt).Exec(ctx)
	default:
		return err
	}
}

// IsJTIRevoked reports whether jti is on the denylist with an entry that has not yet expired.
// An expired entry is treated as not-revoked here rather than deleted: sweeping expired rows
// is the job of a background cleanup pass (see the expires_at index on OAuthRevokedJTI), not a
// read path that must stay cheap on the validation hot path.
func (s *Store) IsJTIRevoked(ctx context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}
	row, err := s.client.OAuthRevokedJTI.Query().Where(oauthrevokedjti.Jti(jti)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return time.Now().Before(row.ExpiresAt), nil
}

// RevokeAllTokensForUserClient revokes every refresh-token family issued to (userID, clientID)
// — marking each revoked and deleting its member refresh-token rows — and adds every
// non-expired access-token metadata row for the pair to the JTI denylist (their stored,
// already-hashed token_id is what RevokeJTI/IsJTIRevoked key on for these rows; see
// SaveTokenMetadata). It returns the total number of tokens revoked: refresh-token rows
// deleted plus JTIs newly denylisted.
func (s *Store) RevokeAllTokensForUserClient(ctx context.Context, userID, clientID string) (int, error) {
	if userID == "" || clientID == "" {
		return 0, errors.New("userID and clientID cannot be empty")
	}

	total := 0
	now := time.Now()

	families, err := s.client.OAuthRefreshFamily.Query().
		Where(oauthrefreshfamily.UserID(userID), oauthrefreshfamily.ClientID(clientID)).
		All(ctx)
	if err != nil {
		return 0, err
	}
	if len(families) > 0 {
		familyIDs := make([]string, len(families))
		for i, f := range families {
			familyIDs[i] = f.FamilyID
		}

		n, err := s.client.OAuthRefreshToken.Delete().
			Where(oauthrefreshtoken.FamilyIDIn(familyIDs...)).
			Exec(ctx)
		if err != nil {
			return total, err
		}
		total += n

		if err := s.client.OAuthRefreshFamily.Update().
			Where(oauthrefreshfamily.FamilyIDIn(familyIDs...)).
			SetRevoked(true).
			SetRevokedAt(now).
			Exec(ctx); err != nil {
			return total, err
		}
	}

	metas, err := s.client.OAuthTokenMetadata.Query().
		Where(
			oauthtokenmetadata.UserID(userID),
			oauthtokenmetadata.ClientID(clientID),
			oauthtokenmetadata.TokenType("access"),
		).
		All(ctx)
	if err != nil {
		return total, err
	}
	for _, m := range metas {
		if !m.ExpiresAt.IsZero() && !m.ExpiresAt.After(now) {
			continue // already expired: no value in denylisting it
		}
		if err := s.RevokeJTI(ctx, m.TokenID, m.ExpiresAt); err != nil {
			return total, err
		}
		total++
	}

	return total, nil
}

// GetTokensByUserClient retrieves the token IDs recorded for a user+client combination, for
// testing/debugging use per the storage.TokenRevocationStore contract. The returned strings
// are the SHA-256 hashes SaveTokenMetadata stored them under, not the original bearer values —
// entstore never persists a raw bearer credential, so there is nothing else to return.
func (s *Store) GetTokensByUserClient(ctx context.Context, userID, clientID string) ([]string, error) {
	if userID == "" || clientID == "" {
		return nil, errors.New("userID and clientID cannot be empty")
	}
	metas, err := s.client.OAuthTokenMetadata.Query().
		Where(oauthtokenmetadata.UserID(userID), oauthtokenmetadata.ClientID(clientID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(metas))
	for i, m := range metas {
		ids[i] = m.TokenID
	}
	return ids, nil
}
