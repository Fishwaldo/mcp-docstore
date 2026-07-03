// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package entstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/giantswarm/mcp-oauth/storage"
	"golang.org/x/oauth2"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthprovidertoken"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthrefreshtoken"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthuserinfo"
)

// getProviderToken loads and decrypts the cached upstream provider token stored under key. The
// key is whatever opaque value the mcp-oauth library uses to reach this token (a raw access or
// refresh token, or the user id at login); it is looked up by its SHA-256 hash, matching how
// SaveToken/DeleteToken write and delete it. Both a missing row and an expired one are reported
// as storage.ErrTokenNotFound: the caller (GetToken and the refresh-token atomic gate) has no
// use for a stale cached token, so a stale one and an absent one are equally "nothing to return."
func (s *Store) getProviderToken(ctx context.Context, key string) (*oauth2.Token, error) {
	row, err := s.client.OAuthProviderToken.Query().Where(oauthprovidertoken.UserID(hashToken(key))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, storage.ErrTokenNotFound
		}
		return nil, err
	}
	if time.Now().After(row.ExpiresAt) {
		return nil, storage.ErrTokenNotFound
	}
	return s.decodeProviderToken(row.TokenJSON)
}

// SaveToken upserts the cached upstream provider token under key, refreshing its expiry to
// now+providerTokenTTL on every save (including re-saves under an existing key). The mcp-oauth
// library calls this under several keys for the same login — the user id, the raw access token,
// and the raw refresh token — so on each refresh rotation the row keyed by the new refresh token
// is written afresh with a full TTL; the atomic gate reads back by that same refresh-token key,
// which is what lets a session outlive the provider-token TTL as long as it keeps rotating. The
// key is stored as its SHA-256 hash so a leaked database yields no replayable raw token.
func (s *Store) SaveToken(ctx context.Context, key string, token *oauth2.Token) error {
	if key == "" {
		return errors.New("key cannot be empty")
	}
	if token == nil {
		return errors.New("token cannot be nil")
	}

	encoded, err := s.encodeProviderToken(token)
	if err != nil {
		return err
	}
	expiresAt := time.Now().Add(s.providerTokenTTL)

	existing, err := s.client.OAuthProviderToken.Query().Where(oauthprovidertoken.UserID(hashToken(key))).Only(ctx)
	switch {
	case err == nil:
		return existing.Update().
			SetTokenJSON(encoded).
			SetExpiresAt(expiresAt).
			Exec(ctx)
	case ent.IsNotFound(err):
		return s.client.OAuthProviderToken.Create().
			SetUserID(hashToken(key)).
			SetTokenJSON(encoded).
			SetExpiresAt(expiresAt).
			Exec(ctx)
	default:
		return err
	}
}

// GetToken retrieves the cached upstream provider token stored under key (see getProviderToken
// for the key semantics), returning storage.ErrTokenNotFound when there is none or it has expired.
func (s *Store) GetToken(ctx context.Context, key string) (*oauth2.Token, error) {
	return s.getProviderToken(ctx, key)
}

// DeleteToken removes the cached upstream provider token stored under key, looking it up by the
// same SHA-256 hash SaveToken wrote. Deleting an already-gone token is a no-op, not an error.
func (s *Store) DeleteToken(ctx context.Context, key string) error {
	_, err := s.client.OAuthProviderToken.Delete().Where(oauthprovidertoken.UserID(hashToken(key))).Exec(ctx)
	return err
}

// SaveUserInfo upserts the cached OIDC userinfo claims for a user, encrypting the serialized
// JSON before it is written to the info_json column and refreshing its expiry to
// now+providerTokenTTL on every save.
func (s *Store) SaveUserInfo(ctx context.Context, userID string, info *storage.UserInfo) error {
	if userID == "" {
		return errors.New("userID cannot be empty")
	}
	if info == nil {
		return errors.New("userInfo cannot be nil")
	}

	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}
	encoded, err := s.enc.Encrypt(string(raw))
	if err != nil {
		return err
	}
	expiresAt := time.Now().Add(s.providerTokenTTL)

	existing, err := s.client.OAuthUserInfo.Query().Where(oauthuserinfo.UserID(userID)).Only(ctx)
	switch {
	case err == nil:
		return existing.Update().
			SetInfoJSON(encoded).
			SetExpiresAt(expiresAt).
			Exec(ctx)
	case ent.IsNotFound(err):
		return s.client.OAuthUserInfo.Create().
			SetUserID(userID).
			SetInfoJSON(encoded).
			SetExpiresAt(expiresAt).
			Exec(ctx)
	default:
		return err
	}
}

// GetUserInfo retrieves the cached OIDC userinfo claims for a user, returning
// storage.ErrUserInfoNotFound when there is none. Unlike GetToken, a past expires_at does not
// hide the row: userinfo has no analog to a provider token going stale mid-request, so the
// stored claims remain usable until an explicit re-fetch overwrites them (matching the memory
// backend, which never checks expiry on read here).
func (s *Store) GetUserInfo(ctx context.Context, userID string) (*storage.UserInfo, error) {
	row, err := s.client.OAuthUserInfo.Query().Where(oauthuserinfo.UserID(userID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, storage.ErrUserInfoNotFound
		}
		return nil, err
	}
	raw, err := s.enc.Decrypt(row.InfoJSON)
	if err != nil {
		return nil, err
	}
	var info storage.UserInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// SaveRefreshToken inserts a new refresh token row keyed by the SHA-256 hash of its value.
// client_id, family_id, and generation are left at their zero values (""/""/0): this plain
// (non-family-aware) save has no client_id available from its caller and no family to track
// reuse against. An empty client_id is the intended value — the mcp-oauth server routes it to
// its OAuth 2.1 Section 6 "missing client binding" rejection path. The family-aware
// SaveRefreshTokenWithFamily variant sets all three properly.
func (s *Store) SaveRefreshToken(ctx context.Context, refreshToken, userID string, expiresAt time.Time) error {
	if refreshToken == "" {
		return errors.New("refresh token cannot be empty")
	}
	if userID == "" {
		return errors.New("userID cannot be empty")
	}
	return s.client.OAuthRefreshToken.Create().
		SetTokenHash(hashToken(refreshToken)).
		SetUserID(userID).
		SetClientID("").
		SetFamilyID("").
		SetGeneration(0).
		SetExpiresAt(expiresAt).
		Exec(ctx)
}

// GetRefreshTokenInfo returns the user ID a refresh token was issued to, looking it up by its
// SHA-256 hash. Returns storage.ErrTokenNotFound when no such token exists and
// storage.ErrTokenExpired when it has passed its expires_at.
func (s *Store) GetRefreshTokenInfo(ctx context.Context, refreshToken string) (string, error) {
	row, err := s.client.OAuthRefreshToken.Query().Where(oauthrefreshtoken.TokenHash(hashToken(refreshToken))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", storage.ErrTokenNotFound
		}
		return "", err
	}
	if time.Now().After(row.ExpiresAt) {
		return "", storage.ErrTokenExpired
	}
	return row.UserID, nil
}

// DeleteRefreshToken removes a refresh token by its SHA-256 hash. Deleting an already-gone (or
// never-existent) token is a no-op, not an error.
func (s *Store) DeleteRefreshToken(ctx context.Context, refreshToken string) error {
	_, err := s.client.OAuthRefreshToken.Delete().Where(oauthrefreshtoken.TokenHash(hashToken(refreshToken))).Exec(ctx)
	return err
}

// AtomicGetAndDeleteRefreshToken atomically retrieves and deletes a refresh token by its
// SHA-256 hash: the DELETE is unconditional but only ever removes at most one row (token_hash
// is unique), and its reported row count is the single-winner gate — of any two concurrent
// calls racing on the same refresh token, exactly one observes n==1 and proceeds, the other
// observes n==0 and reports storage.ErrTokenNotFound, exactly as if it had lost a read/delete
// race against a legitimate rotation.
func (s *Store) AtomicGetAndDeleteRefreshToken(ctx context.Context, refreshToken string) (string, string, *oauth2.Token, error) {
	h := hashToken(refreshToken)
	row, err := s.client.OAuthRefreshToken.Query().Where(oauthrefreshtoken.TokenHash(h)).Only(ctx)
	if ent.IsNotFound(err) {
		return "", "", nil, storage.ErrTokenNotFound
	}
	if err != nil {
		return "", "", nil, err
	}
	n, err := s.client.OAuthRefreshToken.Delete().Where(oauthrefreshtoken.TokenHash(h)).Exec(ctx)
	if err != nil {
		return "", "", nil, err
	}
	if n == 0 { // another exchange won the delete between our read and delete
		return "", "", nil, storage.ErrTokenNotFound
	}
	if time.Now().After(row.ExpiresAt) {
		return "", "", nil, storage.ErrTokenExpired
	}
	// Read the provider token by the REFRESH-TOKEN key, not by the user id. The mcp-oauth
	// library re-saves the provider token under each newly issued refresh token on every
	// rotation, so this key carries a full, freshly renewed TTL; reading by the user id would
	// instead find only the row written once at login, which expires after providerTokenTTL and
	// is never renewed — capping every session at that TTL and, once it lapsed, making the
	// library misread a legitimate refresh as refresh-token reuse and revoke the whole family.
	// Matches the memory backend, which reads s.tokens[refreshToken] here.
	//
	// An absent or expired cached provider token must propagate as ErrTokenNotFound, NOT be
	// swallowed into a (…, nil, nil) success: the mcp-oauth refresh handler dereferences the
	// returned provider token unconditionally (server/refresh.go passes
	// providerToken.RefreshToken to the upstream provider), so returning a nil token here would
	// be a nil-pointer DoS. getProviderToken already maps both cases to ErrTokenNotFound, which
	// is exactly what the memory backend returns when its provider token is missing.
	tok, err := s.getProviderToken(ctx, refreshToken)
	if err != nil {
		return "", "", nil, err
	}
	return row.UserID, row.ClientID, tok, nil
}
