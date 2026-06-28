// SPDX-FileCopyrightText: 2026 Justin Hammond
// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Fishwaldo/mcp-docstore/internal/ent"
	"github.com/Fishwaldo/mcp-docstore/internal/ent/session"
)

// NewSession is the data needed to persist a browser session. The caller (the web layer)
// computes the TTL timestamps and the token hash, so the store stays free of cookie and
// TTL policy.
type NewSession struct {
	TokenHash         string
	Subject           string
	Email             string
	Groups            []string
	IDToken           string
	AccessToken       string
	RefreshToken      string
	TokenExpiry       time.Time
	LastSeenAt        time.Time
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
}

// CreateSession persists a new browser session.
func (s *Store) CreateSession(ctx context.Context, in NewSession) (*ent.Session, error) {
	return s.client.Session.Create().
		SetTokenHash(in.TokenHash).
		SetSubject(in.Subject).
		SetEmail(in.Email).
		SetGroups(in.Groups).
		SetIDToken(in.IDToken).
		SetAccessToken(in.AccessToken).
		SetRefreshToken(in.RefreshToken).
		SetTokenExpiry(in.TokenExpiry).
		SetLastSeenAt(in.LastSeenAt).
		SetExpiresAt(in.ExpiresAt).
		SetAbsoluteExpiresAt(in.AbsoluteExpiresAt).
		Save(ctx)
}

// SessionByTokenHash returns the session with the given token hash, or ErrNotFound.
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (*ent.Session, error) {
	sess, err := s.client.Session.Query().Where(session.TokenHashEQ(tokenHash)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return sess, err
}

// TouchSession slides the idle window by updating last_seen_at and expires_at.
func (s *Store) TouchSession(ctx context.Context, id uuid.UUID, lastSeenAt, expiresAt time.Time) error {
	return s.client.Session.UpdateOneID(id).
		SetLastSeenAt(lastSeenAt).
		SetExpiresAt(expiresAt).
		Exec(ctx)
}

// DeleteSessionByTokenHash removes a session (logout / lazy delete). Deleting an absent
// session is not an error.
func (s *Store) DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error {
	_, err := s.client.Session.Delete().Where(session.TokenHashEQ(tokenHash)).Exec(ctx)
	return err
}

// DeleteExpiredSessions removes every session whose expires_at is at or before now,
// returning the number deleted. The sweeper passes the current time; tests pass a fixed
// time for determinism.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int, error) {
	return s.client.Session.Delete().Where(session.ExpiresAtLTE(now)).Exec(ctx)
}
