// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package entstore

import (
	"context"
	"fmt"

	"github.com/giantswarm/mcp-oauth/storage"
	"golang.org/x/crypto/bcrypt"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/oauthclient"
)

// errInvalidClientCredentials is the single opaque error returned by ValidateClientSecret
// for every failure mode. A missing client and a wrong secret are deliberately
// indistinguishable so the error identity never reveals whether a client_id exists.
func errInvalidClientCredentials() error { return fmt.Errorf("invalid client credentials") }

// rowToClient maps a persisted OAuthClient row to the storage.Client shape the mcp-oauth
// library operates on. registration_ip is intentionally omitted: it is server-managed via
// TrackClientIP and is not part of the client's public representation.
func rowToClient(row *ent.OAuthClient) *storage.Client {
	return &storage.Client{
		ClientID:                    row.ClientID,
		ClientSecretHash:            row.ClientSecretHash,
		ClientType:                  row.ClientType,
		RedirectURIs:                row.RedirectUris,
		TokenEndpointAuthMethod:     row.TokenEndpointAuthMethod,
		GrantTypes:                  row.GrantTypes,
		ResponseTypes:               row.ResponseTypes,
		ClientName:                  row.ClientName,
		Scopes:                      row.Scopes,
		CreatedAt:                   row.CreatedAt,
		UpdatedAt:                   row.UpdatedAt,
		RegistrationAccessTokenHash: row.RegistrationAccessTokenHash,
	}
}

// SaveClient upserts a registered client keyed by client_id. An existing row is updated in
// place — preserving its immutable created_at and its server-managed registration_ip — while
// a new client_id inserts a fresh row. registration_ip is never written here; it is only set
// through TrackClientIP.
func (s *Store) SaveClient(ctx context.Context, client *storage.Client) error {
	if client == nil || client.ClientID == "" {
		return fmt.Errorf("invalid client")
	}

	existing, err := s.client.OAuthClient.Query().
		Where(oauthclient.ClientID(client.ClientID)).
		Only(ctx)
	switch {
	case err == nil:
		return existing.Update().
			SetClientSecretHash(client.ClientSecretHash).
			SetClientType(client.ClientType).
			SetRedirectUris(client.RedirectURIs).
			SetTokenEndpointAuthMethod(client.TokenEndpointAuthMethod).
			SetGrantTypes(client.GrantTypes).
			SetResponseTypes(client.ResponseTypes).
			SetClientName(client.ClientName).
			SetScopes(client.Scopes).
			SetRegistrationAccessTokenHash(client.RegistrationAccessTokenHash).
			Exec(ctx)
	case ent.IsNotFound(err):
		create := s.client.OAuthClient.Create().
			SetClientID(client.ClientID).
			SetClientSecretHash(client.ClientSecretHash).
			SetClientType(client.ClientType).
			SetRedirectUris(client.RedirectURIs).
			SetTokenEndpointAuthMethod(client.TokenEndpointAuthMethod).
			SetGrantTypes(client.GrantTypes).
			SetResponseTypes(client.ResponseTypes).
			SetClientName(client.ClientName).
			SetScopes(client.Scopes).
			SetRegistrationAccessTokenHash(client.RegistrationAccessTokenHash)
		if !client.CreatedAt.IsZero() {
			create.SetCreatedAt(client.CreatedAt)
		}
		return create.Exec(ctx)
	default:
		return err
	}
}

// GetClient retrieves a client by its client_id, returning storage.ErrClientNotFound when no
// such client exists.
func (s *Store) GetClient(ctx context.Context, clientID string) (*storage.Client, error) {
	row, err := s.client.OAuthClient.Query().
		Where(oauthclient.ClientID(clientID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, storage.ErrClientNotFound
		}
		return nil, err
	}
	return rowToClient(row), nil
}

// ValidateClientSecret checks a client's secret with a constant-time bcrypt comparison.
//
// The comparison always runs — against the stored hash for a confidential client, or against
// storage.DummyBcryptHash when the client is missing — so an attacker cannot use response
// timing to enumerate valid client_ids. Every failure (unknown client or wrong secret)
// returns the same opaque error, so error identity does not reveal existence either. Public
// clients authenticate via PKCE rather than a secret, so any secret validates for them.
func (s *Store) ValidateClientSecret(ctx context.Context, clientID, clientSecret string) error {
	client, err := s.GetClient(ctx, clientID)

	hashToCompare := storage.DummyBcryptHash
	isPublicClient := false
	if err == nil {
		if client.IsPublic() {
			isPublicClient = true
		} else if client.ClientSecretHash != "" {
			hashToCompare = client.ClientSecretHash
		}
	}

	// Always perform the comparison to keep timing independent of client existence.
	bcryptErr := bcrypt.CompareHashAndPassword([]byte(hashToCompare), []byte(clientSecret))

	if isPublicClient && err == nil {
		return nil
	}
	if err != nil {
		return errInvalidClientCredentials()
	}
	if bcryptErr != nil {
		return errInvalidClientCredentials()
	}
	return nil
}

// ListClients returns every registered client (for administrative use).
func (s *Store) ListClients(ctx context.Context) ([]*storage.Client, error) {
	rows, err := s.client.OAuthClient.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	clients := make([]*storage.Client, 0, len(rows))
	for _, row := range rows {
		clients = append(clients, rowToClient(row))
	}
	return clients, nil
}

// DeleteClient removes a client by its client_id, returning storage.ErrClientNotFound when no
// such client exists.
func (s *Store) DeleteClient(ctx context.Context, clientID string) error {
	n, err := s.client.OAuthClient.Delete().
		Where(oauthclient.ClientID(clientID)).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return storage.ErrClientNotFound
	}
	return nil
}

// CheckIPLimit enforces the per-source-IP client registration cap by counting clients whose
// registration_ip matches ip. A maxClientsPerIP of zero or less disables the check.
func (s *Store) CheckIPLimit(ctx context.Context, ip string, maxClientsPerIP int) error {
	if maxClientsPerIP <= 0 {
		return nil
	}
	count, err := s.client.OAuthClient.Query().
		Where(oauthclient.RegistrationIP(ip)).
		Count(ctx)
	if err != nil {
		return err
	}
	if count >= maxClientsPerIP {
		return fmt.Errorf("%w: %s (%d/%d clients)", storage.ErrClientIPLimitExceeded, ip, count, maxClientsPerIP)
	}
	return nil
}

// TrackClientIP records the source IP a client registered from by stamping registration_ip on
// the client row, which is what CheckIPLimit counts against. A missing client returns
// storage.ErrClientNotFound.
func (s *Store) TrackClientIP(ctx context.Context, clientID, ip string) error {
	n, err := s.client.OAuthClient.Update().
		Where(oauthclient.ClientID(clientID)).
		SetRegistrationIP(ip).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return storage.ErrClientNotFound
	}
	return nil
}
