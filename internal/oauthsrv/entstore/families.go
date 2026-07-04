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
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthtokenmetadata"
)

// SaveRefreshTokenWithFamily inserts a new refresh-token row (hashed, per hashToken) recording
// its client and family binding, and upserts two rows that track the family's state
// independent of this one token:
//
//   - OAuthRefreshFamily, keyed by familyID, records the family's current (highest-issued)
//     generation and revocation state.
//   - OAuthTokenMetadata, keyed by hashToken(refreshToken), records which family this specific
//     token was issued under. This row is deliberately untouched by DeleteRefreshToken and
//     AtomicGetAndDeleteRefreshToken (they only ever act on OAuthRefreshToken), so
//     GetRefreshTokenFamily keeps working for a refresh token whose OAuthRefreshToken row is
//     already gone — the exact situation OAuth 2.1 reuse detection depends on: the
//     just-rotated token (looked up immediately after its row is deleted, to compute the next
//     generation) and an already-rotated token being replayed by an attacker.
func (s *Store) SaveRefreshTokenWithFamily(ctx context.Context, refreshToken, userID, clientID, familyID string, generation int, expiresAt time.Time) error {
	if refreshToken == "" {
		return errors.New("refresh token cannot be empty")
	}
	if userID == "" {
		return errors.New("userID cannot be empty")
	}
	if familyID == "" {
		return errors.New("family ID cannot be empty")
	}

	hash := hashToken(refreshToken)

	if err := s.client.OAuthRefreshToken.Create().
		SetTokenHash(hash).
		SetUserID(userID).
		SetClientID(clientID).
		SetFamilyID(familyID).
		SetGeneration(generation).
		SetExpiresAt(expiresAt).
		Exec(ctx); err != nil {
		return err
	}

	if err := s.upsertRefreshFamily(ctx, familyID, userID, clientID, generation); err != nil {
		return err
	}

	return s.upsertRefreshTokenFamilyMetadata(ctx, hash, userID, clientID, familyID, expiresAt)
}

// upsertRefreshFamily creates or updates the OAuthRefreshFamily row for familyID, advancing its
// generation to the one just issued. issued_at is immutable and set only at creation: it marks
// when the family itself began, not when its latest generation was minted.
func (s *Store) upsertRefreshFamily(ctx context.Context, familyID, userID, clientID string, generation int) error {
	existing, err := s.client.OAuthRefreshFamily.Query().Where(oauthrefreshfamily.FamilyID(familyID)).Only(ctx)
	switch {
	case err == nil:
		return existing.Update().
			SetUserID(userID).
			SetClientID(clientID).
			SetGeneration(generation).
			Exec(ctx)
	case ent.IsNotFound(err):
		return s.client.OAuthRefreshFamily.Create().
			SetFamilyID(familyID).
			SetUserID(userID).
			SetClientID(clientID).
			SetGeneration(generation).
			SetIssuedAt(time.Now()).
			Exec(ctx)
	default:
		return err
	}
}

// upsertRefreshTokenFamilyMetadata records that hash belongs to familyID, preserving any
// Scopes/Audience/JKT/ExtraClaims a prior SaveTokenMetadata call already wrote for this same
// hashed token rather than clobbering them. This mirrors the merge-preserving upsert the
// mcp-oauth in-memory backend performs in its own SaveRefreshTokenWithFamily, so metadata set
// earlier in a flow (e.g. scope/audience at issuance) survives the family bookkeeping write.
func (s *Store) upsertRefreshTokenFamilyMetadata(ctx context.Context, hash, userID, clientID, familyID string, expiresAt time.Time) error {
	existing, err := s.client.OAuthTokenMetadata.Query().Where(oauthtokenmetadata.TokenID(hash)).Only(ctx)
	switch {
	case err == nil:
		upd := existing.Update().
			SetUserID(userID).
			SetClientID(clientID).
			SetIssuedAt(time.Now()).
			SetExpiresAt(expiresAt).
			SetTokenType("refresh")
		if existing.FamilyID == "" {
			upd = upd.SetFamilyID(familyID)
		}
		return upd.Exec(ctx)
	case ent.IsNotFound(err):
		return s.client.OAuthTokenMetadata.Create().
			SetTokenID(hash).
			SetUserID(userID).
			SetClientID(clientID).
			SetIssuedAt(time.Now()).
			SetExpiresAt(expiresAt).
			SetTokenType("refresh").
			SetFamilyID(familyID).
			SetScopes([]string{}).
			Exec(ctx)
	default:
		return err
	}
}

// GetRefreshTokenFamily retrieves family metadata for a refresh token, looked up by the
// OAuthTokenMetadata row SaveRefreshTokenWithFamily wrote for its hash — not the live
// OAuthRefreshToken row, which AtomicGetAndDeleteRefreshToken deletes the moment the token is
// consumed. See SaveRefreshTokenWithFamily's doc comment for why that distinction matters.
func (s *Store) GetRefreshTokenFamily(ctx context.Context, refreshToken string) (*storage.RefreshTokenFamilyMetadata, error) {
	meta, err := s.client.OAuthTokenMetadata.Query().Where(oauthtokenmetadata.TokenID(hashToken(refreshToken))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, storage.ErrRefreshTokenFamilyNotFound
		}
		return nil, err
	}
	if meta.FamilyID == "" {
		return nil, storage.ErrRefreshTokenFamilyNotFound
	}
	return s.GetRefreshTokenFamilyByID(ctx, meta.FamilyID)
}

// GetRefreshTokenFamilyByID returns the family metadata for familyID (storage.
// RefreshTokenFamilyByIDStore), reading the OAuthRefreshFamily row directly rather than
// scanning refresh tokens. Returns storage.ErrRefreshTokenFamilyNotFound when the family is
// unknown.
func (s *Store) GetRefreshTokenFamilyByID(ctx context.Context, familyID string) (*storage.RefreshTokenFamilyMetadata, error) {
	if familyID == "" {
		return nil, storage.ErrRefreshTokenFamilyNotFound
	}
	row, err := s.client.OAuthRefreshFamily.Query().Where(oauthrefreshfamily.FamilyID(familyID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, storage.ErrRefreshTokenFamilyNotFound
		}
		return nil, err
	}
	return familyMetadataFromRow(row), nil
}

func familyMetadataFromRow(row *ent.OAuthRefreshFamily) *storage.RefreshTokenFamilyMetadata {
	return &storage.RefreshTokenFamilyMetadata{
		FamilyID:   row.FamilyID,
		UserID:     row.UserID,
		ClientID:   row.ClientID,
		Generation: row.Generation,
		IssuedAt:   row.IssuedAt,
		Revoked:    row.Revoked,
		RevokedAt:  row.RevokedAt,
	}
}

// RevokeRefreshTokenFamily marks familyID revoked (with revoked_at) and deletes every member
// refresh-token row, so a stolen-and-rotated chain can be killed in one call: the family row
// itself is kept (marked revoked) for forensics and for GetRefreshTokenFamily/
// GetRefreshTokenFamilyByID to report "revoked" rather than "unknown" on a subsequent lookup.
// A familyID that does not exist is a no-op, matching the mcp-oauth in-memory backend, which
// only acts on families it actually finds.
func (s *Store) RevokeRefreshTokenFamily(ctx context.Context, familyID string) error {
	if err := s.client.OAuthRefreshFamily.Update().
		Where(oauthrefreshfamily.FamilyID(familyID)).
		SetRevoked(true).
		SetRevokedAt(time.Now()).
		Exec(ctx); err != nil {
		return err
	}
	_, err := s.client.OAuthRefreshToken.Delete().Where(oauthrefreshtoken.FamilyID(familyID)).Exec(ctx)
	return err
}
