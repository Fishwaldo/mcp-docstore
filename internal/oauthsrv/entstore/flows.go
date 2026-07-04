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
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthauthcode"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthauthstate"
)

// providerTokenEnvelope is the on-disk shape of an AuthorizationCode's ProviderToken. A plain
// json.Marshal of an *oauth2.Token loses its OIDC id_token: that value lives in the token's
// private "raw" extra-fields map, which json.Marshal cannot see. This envelope carries the
// four exported oauth2.Token fields plus id_token explicitly, so the round trip through the
// encrypted provider_token column preserves the identity claims the OIDC flow depends on.
// Other extra fields oauth2.Token may carry (e.g. "scope", "expires_in") are not read back by
// anything downstream and are dropped, matching the only extra field this codebase consumes
// (see server.go in giantswarm/mcp-oauth, which reads exactly "id_token" off the exchanged
// token and nothing else).
type providerTokenEnvelope struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	IDToken      string    `json:"id_token,omitempty"`
}

// encodeProviderToken serializes and encrypts a provider token set for storage in the
// provider_token column. A nil token encodes to the empty string, so an authorization code
// that never captured an upstream token round-trips to a nil ProviderToken on read.
func (s *Store) encodeProviderToken(token *oauth2.Token) (string, error) {
	if token == nil {
		return "", nil
	}
	env := providerTokenEnvelope{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}
	if idToken, ok := token.Extra("id_token").(string); ok {
		env.IDToken = idToken
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return s.enc.Encrypt(string(raw))
}

// decodeProviderToken reverses encodeProviderToken. An empty column decodes to a nil token.
func (s *Store) decodeProviderToken(encoded string) (*oauth2.Token, error) {
	if encoded == "" {
		return nil, nil
	}
	raw, err := s.enc.Decrypt(encoded)
	if err != nil {
		return nil, err
	}
	var env providerTokenEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, err
	}
	token := &oauth2.Token{
		AccessToken:  env.AccessToken,
		TokenType:    env.TokenType,
		RefreshToken: env.RefreshToken,
		Expiry:       env.Expiry,
	}
	if env.IDToken != "" {
		token = token.WithExtra(map[string]any{"id_token": env.IDToken})
	}
	return token, nil
}

// toAuthCode maps a persisted OAuthAuthCode row to the storage.AuthorizationCode shape the
// mcp-oauth library operates on, decrypting the provider_token column along the way.
func (s *Store) toAuthCode(row *ent.OAuthAuthCode) (*storage.AuthorizationCode, error) {
	token, err := s.decodeProviderToken(row.ProviderToken)
	if err != nil {
		return nil, err
	}
	return &storage.AuthorizationCode{
		Code:                row.Code,
		ClientID:            row.ClientID,
		RedirectURI:         row.RedirectURI,
		Scope:               row.Scope,
		Resource:            row.Resource,
		Audience:            row.Audience,
		CodeChallenge:       row.CodeChallenge,
		CodeChallengeMethod: row.CodeChallengeMethod,
		UserID:              row.UserID,
		ProviderToken:       token,
		CreatedAt:           row.CreatedAt,
		ExpiresAt:           row.ExpiresAt,
		Used:                row.Used,
	}, nil
}

// rowToAuthState maps a persisted OAuthAuthState row to the storage.AuthorizationState shape,
// decrypting the provider_code_verifier column along the way.
func (s *Store) rowToAuthState(row *ent.OAuthAuthState) (*storage.AuthorizationState, error) {
	verifier, err := s.enc.Decrypt(row.ProviderCodeVerifier)
	if err != nil {
		return nil, err
	}
	return &storage.AuthorizationState{
		StateID:              row.StateID,
		OriginalClientState:  row.OriginalClientState,
		ClientID:             row.ClientID,
		RedirectURI:          row.RedirectURI,
		Scope:                row.Scope,
		Resource:             row.Resource,
		CodeChallenge:        row.CodeChallenge,
		CodeChallengeMethod:  row.CodeChallengeMethod,
		ProviderState:        row.ProviderState,
		ProviderCodeVerifier: verifier,
		Nonce:                row.Nonce,
		CreatedAt:            row.CreatedAt,
		ExpiresAt:            row.ExpiresAt,
	}, nil
}

// SaveAuthorizationState persists the state of an in-progress authorization_code flow. The
// row carries both lookup keys (state_id and provider_state) so GetAuthorizationState and
// GetAuthorizationStateByProviderState can each find it directly, without the dual-map
// duplication an in-memory backend needs.
func (s *Store) SaveAuthorizationState(ctx context.Context, state *storage.AuthorizationState) error {
	if state == nil || state.StateID == "" {
		return errors.New(storage.ErrMsgInvalidAuthorizationState)
	}
	if state.ProviderState == "" {
		return errors.New(storage.ErrMsgProviderStateRequired)
	}

	verifier, err := s.enc.Encrypt(state.ProviderCodeVerifier)
	if err != nil {
		return err
	}

	create := s.client.OAuthAuthState.Create().
		SetStateID(state.StateID).
		SetClientID(state.ClientID).
		SetRedirectURI(state.RedirectURI).
		SetProviderState(state.ProviderState).
		SetProviderCodeVerifier(verifier).
		SetExpiresAt(state.ExpiresAt)
	if state.OriginalClientState != "" {
		create.SetOriginalClientState(state.OriginalClientState)
	}
	if state.Scope != "" {
		create.SetScope(state.Scope)
	}
	if state.Resource != "" {
		create.SetResource(state.Resource)
	}
	if state.CodeChallenge != "" {
		create.SetCodeChallenge(state.CodeChallenge)
	}
	if state.CodeChallengeMethod != "" {
		create.SetCodeChallengeMethod(state.CodeChallengeMethod)
	}
	if state.Nonce != "" {
		create.SetNonce(state.Nonce)
	}
	if !state.CreatedAt.IsZero() {
		create.SetCreatedAt(state.CreatedAt)
	}
	return create.Exec(ctx)
}

// GetAuthorizationState retrieves an authorization state by the client-facing state_id,
// returning storage.ErrAuthorizationStateNotFound when no such state exists and
// storage.ErrTokenExpired when it has passed its expires_at.
func (s *Store) GetAuthorizationState(ctx context.Context, stateID string) (*storage.AuthorizationState, error) {
	row, err := s.client.OAuthAuthState.Query().Where(oauthauthstate.StateID(stateID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, storage.ErrAuthorizationStateNotFound
		}
		return nil, err
	}
	state, err := s.rowToAuthState(row)
	if err != nil {
		return nil, err
	}
	if state.HasExpired() {
		return nil, storage.ErrTokenExpired
	}
	return state, nil
}

// GetAuthorizationStateByProviderState retrieves an authorization state by the
// server-generated provider_state, used when validating the upstream provider's callback.
func (s *Store) GetAuthorizationStateByProviderState(ctx context.Context, providerState string) (*storage.AuthorizationState, error) {
	row, err := s.client.OAuthAuthState.Query().Where(oauthauthstate.ProviderState(providerState)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, storage.ErrAuthorizationStateNotFound
		}
		return nil, err
	}
	state, err := s.rowToAuthState(row)
	if err != nil {
		return nil, err
	}
	if state.HasExpired() {
		return nil, storage.ErrTokenExpired
	}
	return state, nil
}

// DeleteAuthorizationState removes an authorization state, matching either its state_id or
// its provider_state so callers can delete using whichever key they validated the flow with.
// Deleting an already-gone state is a no-op, not an error.
func (s *Store) DeleteAuthorizationState(ctx context.Context, stateID string) error {
	_, err := s.client.OAuthAuthState.Delete().
		Where(oauthauthstate.Or(oauthauthstate.StateID(stateID), oauthauthstate.ProviderState(stateID))).
		Exec(ctx)
	return err
}

// SaveAuthorizationCode persists a newly issued authorization code, encrypting its captured
// upstream provider token (if any) before it is written to the provider_token column.
func (s *Store) SaveAuthorizationCode(ctx context.Context, code *storage.AuthorizationCode) error {
	if code == nil || code.Code == "" {
		return errors.New("invalid authorization code")
	}

	encoded, err := s.encodeProviderToken(code.ProviderToken)
	if err != nil {
		return err
	}

	create := s.client.OAuthAuthCode.Create().
		SetCode(code.Code).
		SetClientID(code.ClientID).
		SetRedirectURI(code.RedirectURI).
		SetUserID(code.UserID).
		SetExpiresAt(code.ExpiresAt).
		SetUsed(code.Used)
	if code.Scope != "" {
		create.SetScope(code.Scope)
	}
	if code.Resource != "" {
		create.SetResource(code.Resource)
	}
	if code.Audience != "" {
		create.SetAudience(code.Audience)
	}
	if code.CodeChallenge != "" {
		create.SetCodeChallenge(code.CodeChallenge)
	}
	if code.CodeChallengeMethod != "" {
		create.SetCodeChallengeMethod(code.CodeChallengeMethod)
	}
	if encoded != "" {
		create.SetProviderToken(encoded)
	}
	if !code.CreatedAt.IsZero() {
		create.SetCreatedAt(code.CreatedAt)
	}
	return create.Exec(ctx)
}

// GetAuthorizationCode retrieves an authorization code without modifying it, returning
// storage.ErrAuthorizationCodeNotFound when it does not exist and storage.ErrTokenExpired
// when it has passed its expires_at. Use AtomicCheckAndMarkAuthCodeUsed instead of this
// method when redeeming a code at the token endpoint, so two concurrent redemptions of the
// same code cannot both succeed.
func (s *Store) GetAuthorizationCode(ctx context.Context, code string) (*storage.AuthorizationCode, error) {
	row, err := s.client.OAuthAuthCode.Query().Where(oauthauthcode.Code(code)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, storage.ErrAuthorizationCodeNotFound
		}
		return nil, err
	}
	ac, err := s.toAuthCode(row)
	if err != nil {
		return nil, err
	}
	if ac.HasExpired() {
		return nil, storage.ErrTokenExpired
	}
	return ac, nil
}

// AtomicCheckAndMarkAuthCodeUsed flips used=false→true in a single guarded
// UPDATE so two concurrent exchanges of the same code cannot both win.
// The read of the full row happens only after winning the flip.
func (s *Store) AtomicCheckAndMarkAuthCodeUsed(ctx context.Context, code string) (*storage.AuthorizationCode, error) {
	n, err := s.client.OAuthAuthCode.Update().
		Where(oauthauthcode.Code(code), oauthauthcode.Used(false)).
		SetUsed(true).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		// Either the code doesn't exist, or it was already used (reuse attack).
		row, err := s.client.OAuthAuthCode.Query().Where(oauthauthcode.Code(code)).Only(ctx)
		if ent.IsNotFound(err) {
			return nil, storage.ErrAuthorizationCodeNotFound
		}
		if err != nil {
			return nil, err
		}
		if row.Used {
			// Reuse attack: return the decoded code alongside the error. The
			// library's reuse handler dereferences the returned code's UserID
			// to revoke every token for that user+client (OAuth 2.1 §4.1.2);
			// a nil here would panic the token endpoint on every replay.
			ac, err := s.toAuthCode(row)
			if err != nil {
				return nil, err
			}
			return ac, storage.ErrAuthorizationCodeUsed
		}
		return nil, storage.ErrAuthorizationCodeNotFound
	}
	row, err := s.client.OAuthAuthCode.Query().Where(oauthauthcode.Code(code)).Only(ctx)
	if err != nil {
		return nil, err
	}
	ac, err := s.toAuthCode(row) // decrypts ProviderToken
	if err != nil {
		return nil, err
	}
	if ac.HasExpired() {
		return nil, storage.ErrTokenExpired
	}
	return ac, nil
}

// DeleteAuthorizationCode removes an authorization code. Deleting an already-gone code is a
// no-op, not an error.
func (s *Store) DeleteAuthorizationCode(ctx context.Context, code string) error {
	_, err := s.client.OAuthAuthCode.Delete().Where(oauthauthcode.Code(code)).Exec(ctx)
	return err
}
